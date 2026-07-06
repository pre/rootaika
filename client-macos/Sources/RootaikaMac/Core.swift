import Foundation

/// The user-session agent loop, mirroring the Windows rootaika-agent: each
/// observe tick it pulls lock/config state from the local daemon, drives the
/// lock overlay and pre-lock warning, samples activity, and reports events to
/// the daemon (which buffers and uploads them). The agent holds no server
/// credentials and no persistent state of its own.
actor Core {
    private let daemon: DaemonClient
    private let probe: ActivityProbing
    private let lock: LockControlling
    private let debug: DebugLogging
    /// --debug CLI flag: keeps the console visible regardless of daemon state.
    private let forceDebug: Bool

    /// Latest state from the daemon; fallback defaults until first contact.
    private var state = AgentState.fallback

    // Tracks the last debug-mode value applied to the console so it is shown
    // the moment the server enables debug and hidden when it disables it.
    private var lastDebugMode: Bool?

    // Reporting state (shouldEmit / heartbeat logic).
    private var lastEvent: Event?
    private var lastSentAt: Date?
    private let heartbeat: TimeInterval = 60

    // Lock / warning state.
    private var warned: Bool = false
    private var warningTask: Task<Void, Never>?
    // Last lock intent already applied. The daemon state is re-fetched every
    // observe tick with a usually-unchanged lock state; without this guard each
    // tick would relaunch the warning countdown (restarting the sound) and
    // rebuild the lock windows (green flicker). We act only when the intent
    // actually changes. The warned flag is part of the signature so that a
    // warning -> lock transition we drove ourselves is not seen as "new".
    private var lastLockSignature: String?

    init(daemon: DaemonClient, probe: ActivityProbing, lock: LockControlling, debug: DebugLogging, forceDebug: Bool) {
        self.daemon = daemon
        self.probe = probe
        self.lock = lock
        self.debug = debug
        self.forceDebug = forceDebug
    }

    private var effectiveDebug: Bool {
        state.debugMode || forceDebug
    }

    // MARK: Run loop

    /// One loop, one tick cadence — matches the Windows agent's single loop.
    func run() async {
        debug.setVisible(forceDebug)
        debug.log("agent started: daemon=\(AgentIPC.baseURL) forced_debug=\(forceDebug)")
        while !Task.isCancelled {
            await tickOnce()
            let interval = state.observeIntervalSeconds > 0 ? state.observeIntervalSeconds : 5
            try? await Task.sleep(nanoseconds: UInt64(interval) * 1_000_000_000)
        }
    }

    func tickOnce() async {
        // 1. Pull lock/config state from the daemon. On failure keep driving
        //    with the last known state (matches the Windows agent).
        do {
            state = try await daemon.fetchState()
            debug.log("state from daemon: locked=\(state.locked) warning=\(state.lockWarningSeconds)s idle_threshold=\(state.idleThresholdSeconds)s observe=\(state.observeIntervalSeconds)s debug=\(state.debugMode)")
        } catch {
            debug.log("fetch daemon state failed: \(error)")
        }

        // 2. Console visibility follows debug mode.
        if lastDebugMode != effectiveDebug {
            lastDebugMode = effectiveDebug
            debug.setVisible(effectiveDebug)
        }

        // 3. Drive the lock overlay / warning countdown.
        applyLockState(locked: state.locked, message: state.lockMessage, warningSeconds: state.lockWarningSeconds)

        // 4. Observe and report.
        let idle = probe.idleSeconds()
        let foreground = probe.frontmostProcessName()
        // The actual overlay state, which lags state.locked while a warning
        // countdown runs: the playable grace period still counts as usage.
        let screenLocked = lock.isShowing
        let threshold = state.idleThresholdSeconds > 0 ? state.idleThresholdSeconds : 60

        let activityState: ActivityState
        if screenLocked {
            activityState = .locked
        } else if idle >= Double(threshold) {
            activityState = .idle
        } else {
            activityState = .active
        }

        let now = Date()
        let event = Event(
            eventID: UUID().uuidString,
            occurredAt: now,
            state: activityState,
            processName: activityState == .active ? Core.normalizeProcessName(foreground) : nil
        )
        debug.log("observe: idle=\(String(format: "%.1f", idle))s frontmost=\(foreground ?? "nil") -> state=\(activityState.rawValue) process=\(event.processName ?? "nil")")

        if shouldEmit(last: lastEvent, current: event, lastSentAt: lastSentAt, now: now) {
            do {
                try await daemon.postEvent(event)
                lastEvent = event
                lastSentAt = now
                debug.log("sent event to daemon: state=\(activityState.rawValue) process=\(event.processName ?? "nil")")
            } catch {
                // Keep lastEvent unchanged so the next tick retries.
                debug.log("post event to daemon failed: \(error)")
            }
        }
    }

    /// Replicates the Windows shouldEmit: emit on first event, state change,
    /// process change, or heartbeat elapsed.
    func shouldEmit(last: Event?, current: Event, lastSentAt: Date?, now: Date) -> Bool {
        guard let last = last, let sentAt = lastSentAt else { return true }
        if last.state != current.state { return true }
        if last.processName != current.processName { return true }
        if now >= sentAt.addingTimeInterval(heartbeat) { return true }
        return false
    }

    static func normalizeProcessName(_ raw: String?) -> String? {
        guard let raw = raw else { return nil }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return nil }
        let slashed = trimmed.replacingOccurrences(of: "\\", with: "/")
        let base = (slashed as NSString).lastPathComponent
        return base.lowercased()
    }

    // MARK: Lock state

    func applyLockState(locked: Bool, message: String, warningSeconds: Int) {
        let signature = "\(locked)|\(warningSeconds)|\(warned)|\(effectiveDebug)|\(message)"
        if signature == lastLockSignature {
            return
        }
        lastLockSignature = signature
        debug.log("lock state change: locked=\(locked) warning=\(warningSeconds)s warned=\(warned) message=\(message.isEmpty ? "(none)" : message)")

        if !locked {
            warningTask?.cancel()
            warningTask = nil
            warned = false
            lock.hideWarning()
            if lock.isShowing { lock.hideLock() }
            return
        }

        // Locked.
        if warningSeconds <= 0 || warned {
            lock.hideWarning()
            lock.showLock(
                message: message,
                warningSeconds: warningSeconds,
                debugShutdownAllowed: effectiveDebug
            )
            return
        }

        // Pre-lock warning countdown: show the click-through banner with a live
        // remaining-seconds count and loop the cached warning sound, while the
        // screen stays usable. Engage the lock overlay only once it elapses. The
        // sound plays ONLY here, never at the lock moment (matches Windows).
        if warningTask == nil {
            let soundPath = state.warningSoundPath
            warningTask = Task { [weak self] in
                await self?.runWarningCountdown(
                    message: message,
                    warningSeconds: warningSeconds,
                    soundPath: soundPath
                )
            }
        }
    }

    private func runWarningCountdown(message: String, warningSeconds: Int, soundPath: String) async {
        lock.startWarningAudio(path: soundPath)
        var remaining = warningSeconds
        while remaining > 0 {
            if Task.isCancelled {
                lock.hideWarning()
                return
            }
            lock.showWarning(message: message, remainingSeconds: remaining)
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            remaining -= 1
        }
        if Task.isCancelled {
            lock.hideWarning()
            return
        }
        completeWarning(message: message, warningSeconds: warningSeconds)
    }

    private func completeWarning(message: String, warningSeconds: Int) {
        warned = true
        warningTask = nil
        lock.hideWarning() // stops the warning sound and banner
        lock.showLock(
            message: message,
            warningSeconds: warningSeconds,
            debugShutdownAllowed: effectiveDebug
        )
    }
}
