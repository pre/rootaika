import Foundation

/// The agent loop: observe -> state machine -> buffer/report -> long-poll config
/// -> apply lock state (warning countdown + overlay). Depends ONLY on the
/// protocols; concrete implementations are injected via init.
actor Core {
    private let board: BoardClienting
    private let probe: ActivityProbing
    private let lock: LockControlling
    private var config: Config

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

    init(config: Config, board: BoardClienting, probe: ActivityProbing, lock: LockControlling) {
        self.config = config
        self.board = board
        self.probe = probe
        self.lock = lock
    }

    // MARK: Run loops

    /// Start the three concurrent loops (observe, upload, poll) and block until cancelled.
    func run() async {
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

        if shouldEmit(last: lastEvent, current: event, lastSentAt: lastSentAt, now: now) {
            let stateChanged = (lastEvent?.state != event.state)
            let processChanged = (lastEvent?.processName != event.processName)
            enqueue(event)
            lastEvent = event
            lastSentAt = now
            lastReportedStatus = state
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
                try await board.postEvents(batch)
                pending.removeFirst(slice.count) // mark-sent only after ack
            } catch {
                // Keep events queued; retry next cycle.
                break
            }
        }
    }

    // MARK: Long-poll config

    private func pollLoop() async {
        while !Task.isCancelled {
            do {
                let status = lastReportedStatus?.rawValue
                let cfg = try await board.fetchConfig(
                    clientID: config.clientID,
                    status: status,
                    knownVersion: lastConfigVersion,
                    waitSeconds: config.pollWaitSeconds
                )
                applyConfig(cfg)
                if config.debugMode {
                    FileHandle.standardError.write(Data("poll ok: version=\(cfg.configVersion) locked=\(cfg.locked)\n".utf8))
                }
                let gap = max(1, 0) // minPollGapSeconds floor
                try? await Task.sleep(nanoseconds: UInt64(gap) * 1_000_000_000)
            } catch {
                if config.debugMode {
                    FileHandle.standardError.write(Data("poll error: \(error)\n".utf8))
                }
                let backoff = config.pollIntervalSeconds > 0 ? config.pollIntervalSeconds : 30
                try? await Task.sleep(nanoseconds: UInt64(backoff) * 1_000_000_000)
            }
        }
    }

    func applyConfig(_ cfg: ClientConfig) {
        lastConfigVersion = cfg.configVersion
        _ = config.applyServerConfig(cfg)
        try? config.save()
        applyLockState(locked: cfg.locked, message: cfg.lockMessage, warningSeconds: cfg.warningSeconds)
    }

    // MARK: Lock state

    func applyLockState(locked: Bool, message: String, warningSeconds: Int) {
        if !locked {
            warningTask?.cancel()
            warningTask = nil
            warned = false
            if lock.isShowing { lock.hideLock() }
            return
        }

        // Locked.
        if warningSeconds <= 0 || warned {
            lock.showLock(message: message, warningSeconds: warningSeconds)
            return
        }

        // Pre-lock warning countdown (screen stays usable; engage overlay after).
        if warningTask == nil {
            warningTask = Task { [weak self] in
                guard let self = self else { return }
                try? await Task.sleep(nanoseconds: UInt64(warningSeconds) * 1_000_000_000)
                if Task.isCancelled { return }
                await self.completeWarning(message: message, warningSeconds: warningSeconds)
            }
        }
    }

    private func completeWarning(message: String, warningSeconds: Int) {
        warned = true
        warningTask = nil
        lock.showLock(message: message, warningSeconds: warningSeconds)
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
