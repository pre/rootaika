import Foundation

/// The agent loop: observe -> state machine -> buffer/report -> long-poll config
/// -> apply lock state (warning countdown + overlay). Depends ONLY on the
/// protocols; concrete implementations are injected via init.
actor Core {
    private let board: BoardClienting
    private let probe: ActivityProbing
    private let lock: LockControlling
    private let debug: DebugLogging
    private var config: Config

    // Tracks the last debug-mode value applied to the console so it is shown the
    // moment the server enables debug and hidden when it disables it.
    private var lastDebugMode: Bool = false

    // Buffered, unsent events (in-memory placeholder for the SQLite buffer).
    private var pending: [Event] = []
    private var nextSequence: Int64 = 1

    // Reporting state (shouldEmit / heartbeat logic).
    private var lastEvent: Event?
    private var lastSentAt: Date?
    private let heartbeat: TimeInterval = 60

    // Long-poll state.
    private var lastConfigVersion: String?
    private var lastReportedStatus: ActivityState?

    // Immediate-upload signalling: when a state/process change is observed we
    // wake the upload loop instead of waiting for the next interval tick.
    private var uploadWakeup: CheckedContinuation<Void, Never>?

    // Lock / warning state.
    private var warned: Bool = false
    private var warningTask: Task<Void, Never>?
    // Last lock intent already applied. Every long-poll return (config change or
    // wait budget elapsed) re-runs applyConfig with a usually-unchanged lock
    // state. Without this guard each poll would relaunch the warning countdown
    // (restarting the sound) and rebuild the lock windows (green flicker). We
    // act only when the intent actually changes.
    private var lastLockSignature: String?

    init(config: Config, board: BoardClienting, probe: ActivityProbing, lock: LockControlling, debug: DebugLogging) {
        self.config = config
        self.board = board
        self.probe = probe
        self.lock = lock
        self.debug = debug
    }

    // MARK: Run loops

    /// Start the three concurrent loops (observe, upload, poll) and block until cancelled.
    func run() async {
        lastDebugMode = config.debugMode
        debug.setVisible(config.debugMode)
        debug.log("agent started: server=\(config.serverURL) client_id=\(config.clientID) debug=\(config.debugMode)")
        debug.log("config: observe=\(config.observeIntervalSeconds)s idle_threshold=\(config.idleThresholdSeconds)s upload=\(config.uploadIntervalSeconds)s poll_wait=\(config.pollWaitSeconds)s")
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await self.observeLoop() }
            group.addTask { await self.uploadLoop() }
            group.addTask { await self.pollLoop() }
            await group.waitForAll()
        }
    }

    // MARK: Observe + state machine

    private func observeLoop() async {
        while !Task.isCancelled {
            let interval = config.observeIntervalSeconds > 0 ? config.observeIntervalSeconds : 5
            tick()
            try? await Task.sleep(nanoseconds: UInt64(interval) * 1_000_000_000)
        }
    }

    /// One observe tick: sample, derive state, build event, emit if needed.
    func tick() {
        let idle = probe.idleSeconds()
        let foreground = probe.frontmostProcessName()
        let screenLocked = lock.isShowing

        let threshold = config.idleThresholdSeconds > 0 ? config.idleThresholdSeconds : 60

        let state: ActivityState
        var processName: String?
        if screenLocked {
            state = .locked
            processName = nil
        } else if idle >= Double(threshold) {
            state = .idle
            processName = nil
        } else {
            state = .active
            processName = Core.normalizeProcessName(foreground)
        }

        let now = Date()
        let event = Event(
            eventID: UUID().uuidString,
            occurredAt: now,
            state: state,
            processName: state == .active ? processName : nil,
            sequence: 0
        )

        debug.log("observe: idle=\(String(format: "%.1f", idle))s frontmost=\(foreground ?? "nil") -> state=\(state.rawValue) process=\(event.processName ?? "nil")")

        if shouldEmit(last: lastEvent, current: event, lastSentAt: lastSentAt, now: now) {
            let stateChanged = (lastEvent?.state != event.state)
            let processChanged = (lastEvent?.processName != event.processName)
            enqueue(event)
            lastEvent = event
            lastSentAt = now
            lastReportedStatus = state
            let reason = stateChanged ? "state-change" : (processChanged ? "process-change" : "heartbeat")
            debug.log("queued event: state=\(state.rawValue) process=\(event.processName ?? "nil") reason=\(reason) pending=\(pending.count)")
            // Report state/process transitions immediately rather than waiting
            // for the next upload interval; heartbeats ride the interval.
            if stateChanged || processChanged {
                signalUpload()
            }
        }
    }

    /// Wake the upload loop so a freshly emitted transition is sent right away.
    private func signalUpload() {
        if let cont = uploadWakeup {
            uploadWakeup = nil
            cont.resume()
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

    private func enqueue(_ event: Event) {
        var e = event
        e.sequence = nextSequence
        nextSequence += 1
        pending.append(e)
    }

    // MARK: Upload

    private func uploadLoop() async {
        while !Task.isCancelled {
            let interval = config.uploadIntervalSeconds > 0 ? config.uploadIntervalSeconds : 60
            await uploadOnce()
            await waitForUploadOrInterval(seconds: interval)
        }
    }

    /// Sleep up to `seconds`, but return early if `signalUpload()` is called.
    private func waitForUploadOrInterval(seconds: Int) async {
        let timer = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(max(1, seconds)) * 1_000_000_000)
            await self?.signalUpload()
        }
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            // If a signal is already pending (continuation slot occupied), resume
            // the prior waiter first; only one waiter exists at a time here.
            if uploadWakeup != nil {
                let prev = uploadWakeup
                uploadWakeup = nil
                prev?.resume()
            }
            uploadWakeup = cont
        }
        timer.cancel()
    }

    func uploadOnce() async {
        let batchSize = config.batchSize > 0 ? config.batchSize : 100
        // Drain in batchSize chunks; stop on the first failure so the remaining
        // events stay queued for the next cycle.
        while !pending.isEmpty {
            let slice = Array(pending.prefix(batchSize))
            let batch = EventBatch(clientID: config.clientID, events: slice)
            do {
                debug.log("upload: sending \(slice.count) event(s) to \(config.serverURL)/api/v1/events/batch")
                let resp = try await board.postEvents(batch)
                pending.removeFirst(slice.count) // mark-sent only after ack
                debug.log("upload ok: accepted=\(resp.accepted) duplicate_or_ignored=\(resp.duplicateOrIgnored) device_id=\(resp.deviceID) remaining=\(pending.count)")
            } catch {
                // Keep events queued; retry next cycle.
                debug.log("upload failed: \(error) (keeping \(pending.count) event(s) queued)")
                break
            }
        }
    }

    // MARK: Long-poll config

    private func pollLoop() async {
        while !Task.isCancelled {
            do {
                let status = lastReportedStatus?.rawValue
                debug.log("poll: GET /api/v1/client/config status=\(status ?? "nil") known_version=\(lastConfigVersion ?? "nil") wait=\(config.pollWaitSeconds)s")
                let cfg = try await board.fetchConfig(
                    clientID: config.clientID,
                    status: status,
                    knownVersion: lastConfigVersion,
                    waitSeconds: config.pollWaitSeconds
                )
                await applyConfig(cfg)
                debug.log("poll ok: version=\(cfg.configVersion) debug=\(cfg.debugMode) locked=\(cfg.locked) warning=\(cfg.warningSeconds)s idle_threshold=\(cfg.idleThresholdSeconds)s upload=\(cfg.uploadIntervalSeconds)s")
                let gap = max(1, 0) // minPollGapSeconds floor
                try? await Task.sleep(nanoseconds: UInt64(gap) * 1_000_000_000)
            } catch {
                debug.log("poll error: \(error)")
                let backoff = config.pollIntervalSeconds > 0 ? config.pollIntervalSeconds : 30
                try? await Task.sleep(nanoseconds: UInt64(backoff) * 1_000_000_000)
            }
        }
    }

    func applyConfig(_ cfg: ClientConfig) async {
        lastConfigVersion = cfg.configVersion
        _ = config.applyServerConfig(cfg)
        // Show/hide the on-screen console the moment the server toggles debug mode.
        if config.debugMode != lastDebugMode {
            lastDebugMode = config.debugMode
            debug.log("debug mode \(config.debugMode ? "enabled" : "disabled") by server")
            debug.setVisible(config.debugMode)
        }
        await syncWarningSound(serverVersion: cfg.warningSoundVersion)
        try? config.save()
        applyLockState(locked: cfg.locked, message: cfg.lockMessage, warningSeconds: cfg.warningSeconds)
    }

    /// Reconcile the cached warning MP3 with the server's version so it is on
    /// disk before a lock arrives. Best-effort: a download failure is ignored
    /// (the prior cache stays) and only updates `config.warningSoundVersion`.
    private func syncWarningSound(serverVersion: String) async {
        do {
            let result = try await WarningSound.sync(
                downloader: board,
                soundPath: config.warningSoundPath(),
                cachedVersion: config.warningSoundVersion,
                serverVersion: serverVersion
            )
            switch result {
            case .unchanged:
                break
            case .cleared:
                config.warningSoundVersion = ""
                debug.log("warning sound: cleared (server has none)")
            case .updated(let version):
                config.warningSoundVersion = version
                debug.log("warning sound: downloaded new version \(version)")
            }
        } catch {
            debug.log("warning sound sync error: \(error)")
        }
    }

    // MARK: Lock state

    func applyLockState(locked: Bool, message: String, warningSeconds: Int) {
        // Edge-trigger on the lock intent. The server short-polls, so this is
        // called repeatedly with an identical state; only a real change should
        // (re)drive the overlay, otherwise the sound restarts and the overlay
        // flickers every poll. The warned flag is part of the signature so that
        // a warning -> lock transition we drove ourselves is not seen as "new".
        let signature = "\(locked)|\(warningSeconds)|\(warned)|\(config.debugMode)|\(message)"
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
                debugShutdownAllowed: config.debugMode
            )
            return
        }

        // Pre-lock warning countdown: show the click-through banner with a live
        // remaining-seconds count and loop the cached warning sound, while the
        // screen stays usable. Engage the lock overlay only once it elapses. The
        // sound plays ONLY here, never at the lock moment (matches Windows).
        if warningTask == nil {
            let soundPath = WarningSound.cachedPath(config)
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
            debugShutdownAllowed: config.debugMode
        )
    }

    // MARK: Test helpers

    /// Build a stubbed event (used by --selftest).
    func buildStubEvent() -> Event {
        Event(
            eventID: UUID().uuidString,
            occurredAt: Date(),
            state: .active,
            processName: "selftest",
            sequence: 1
        )
    }
}
