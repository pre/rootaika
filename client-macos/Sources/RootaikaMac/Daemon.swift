import Foundation
import SystemConfiguration

/// The privileged half of the macOS client (a root LaunchDaemon), mirroring
/// the Windows rootaika-service: it owns the config file and SQLite buffer,
/// uploads event batches, long-polls the server for config, caches the warning
/// MP3, serves the loopback endpoint the user-session agent talks to, and — as
/// the watchdog — re-bootstraps the agent LaunchAgent if a user boots it out.
final class Daemon {
    private let baseDir: URL
    private let store: DaemonStateStore
    private let buffer: EventBuffer
    private let board: NetworkBoardClient
    private let agentToken: String
    private let soundPath: URL
    private var server: AgentHTTPServer?

    init(options: CLIOptions) throws {
        baseDir = Config.daemonBaseDir()
        try FileManager.default.createDirectory(
            at: baseDir,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o755]
        )
        // The dir must stay world-traversable: the agent reads the token file
        // and warning MP3 inside it (config.json itself stays 0600 root).
        try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: baseDir.path)

        let configPath = baseDir.appendingPathComponent("config.json", isDirectory: false)
        var config = try Config.load(path: configPath)
        if let serverURL = options.serverOverride, !serverURL.isEmpty {
            config.serverURL = serverURL
        }
        if options.forceDebug {
            config.debugMode = true
        }

        store = DaemonStateStore(path: configPath, config: config)
        buffer = try EventBuffer(path: baseDir.appendingPathComponent("rootaika-client.db", isDirectory: false))
        board = NetworkBoardClient(config: config)
        agentToken = try AgentToken.loadOrCreate(baseDir: baseDir)
        soundPath = baseDir.appendingPathComponent("warning-sound.mp3", isDirectory: false)
    }

    /// Start the HTTP endpoint and the three loops. Returns immediately; the
    /// caller parks the main thread (dispatchMain).
    func start() throws {
        setbuf(stdout, nil) // launchd log file: line-timely output
        let cfg = store.snapshot()
        log("daemon started: server=\(cfg.serverURL) client_id=\(cfg.clientID) base=\(baseDir.path)")

        let server = try AgentHTTPServer(
            port: AgentIPC.port,
            handler: { [weak self] request in
                self?.handle(request) ?? .json(500, ["error": "daemon shutting down"])
            },
            onFailure: { error in
                // Without the agent endpoint the client is blind; die and let
                // launchd's KeepAlive restart us (mirrors the Windows service
                // failing startup when its listener cannot bind).
                FileHandle.standardError.write(Data("agent endpoint failed: \(error)\n".utf8))
                exit(1)
            }
        )
        server.start()
        self.server = server
        log("agent endpoint listening on 127.0.0.1:\(AgentIPC.port)")

        Task.detached { await self.uploadLoop() }
        Task.detached { await self.pollLoop() }
        Task.detached { await self.watchdogLoop() }
    }

    // MARK: Agent endpoint

    private func handle(_ request: AgentHTTPServer.Request) -> AgentHTTPServer.Response {
        guard request.headers[AgentIPC.tokenHeader.lowercased()] == agentToken else {
            return .json(401, ["error": "unauthorized"])
        }
        switch (request.method, request.path) {
        case ("GET", "/agent/state"):
            let cfg = store.snapshot()
            return .json(200, AgentState(
                locked: cfg.locked,
                lockMessage: cfg.lockMessage,
                lockWarningSeconds: cfg.lockWarningSeconds,
                idleThresholdSeconds: cfg.idleThresholdSeconds,
                observeIntervalSeconds: cfg.observeIntervalSeconds,
                debugMode: cfg.debugMode,
                warningSoundPath: cachedSoundPath(cfg)
            ))
        case ("POST", "/agent/events"):
            guard let body = try? RootaikaJSON.makeDecoder().decode(AgentEventsRequest.self, from: request.body),
                  !body.events.isEmpty else {
                return .json(400, ["error": "events is empty or undecodable"])
            }
            var queued = 0
            for event in body.events {
                do {
                    try buffer.enqueue(event)
                    queued += 1
                } catch {
                    return .json(400, ["error": "\(error)"])
                }
            }
            // Remember the most recent state the agent observed so the poll
            // loop reports it back to the server as the device status.
            if let lastState = body.events.last?.state {
                store.setReported(lastState.rawValue)
            }
            return .json(202, ["queued": queued])
        case (_, "/agent/state"), (_, "/agent/events"):
            return .json(405, ["error": "method not allowed"])
        default:
            return .json(404, ["error": "not found"])
        }
    }

    /// Path handed to the agent for the pre-lock warning sound; "" until a
    /// sound is cached on disk.
    private func cachedSoundPath(_ cfg: Config) -> String {
        guard !cfg.warningSoundVersion.isEmpty,
              FileManager.default.fileExists(atPath: soundPath.path) else {
            return ""
        }
        return soundPath.path
    }

    // MARK: Upload loop

    private func uploadLoop() async {
        while true {
            await uploadOnce()
            let interval = store.snapshot().uploadIntervalSeconds
            await Daemon.sleep(seconds: interval > 0 ? interval : 60)
        }
    }

    private func uploadOnce() async {
        let cfg = store.snapshot()
        let batchSize = cfg.batchSize > 0 ? cfg.batchSize : 100
        // Drain in batchSize chunks; stop on the first failure so the remaining
        // events stay buffered for the next cycle.
        while true {
            let slice: [Event]
            do {
                slice = try buffer.pending(limit: batchSize)
            } catch {
                log("read pending events failed: \(error)")
                return
            }
            if slice.isEmpty { return }
            do {
                let resp = try await board.postEvents(EventBatch(clientID: cfg.clientID, events: slice))
                try buffer.markSent(slice.map { $0.eventID }) // mark-sent only after ack
                log("uploaded \(slice.count) event(s): accepted=\(resp.accepted) duplicate_or_ignored=\(resp.duplicateOrIgnored)")
            } catch {
                log("event upload failed: \(error) (events stay buffered)")
                return
            }
        }
    }

    // MARK: Config long-poll

    /// Floor between consecutive long polls; guards against a hot loop when a
    /// misconfigured server answers instantly.
    private static let minPollGapSeconds = 1

    private func pollLoop() async {
        while true {
            var gap = Daemon.minPollGapSeconds
            do {
                try await pollOnce()
            } catch {
                log("poll failed: \(error)")
                let backoff = store.snapshot().pollIntervalSeconds
                gap = backoff > 0 ? backoff : 30
            }
            await Daemon.sleep(seconds: gap)
        }
    }

    private func pollOnce() async throws {
        let cfg = store.snapshot()
        let serverConfig = try await board.fetchConfig(
            clientID: cfg.clientID,
            status: store.reported(),
            knownVersion: store.version(),
            waitSeconds: cfg.pollWaitSeconds > 0 ? cfg.pollWaitSeconds : 25
        )
        store.setVersion(serverConfig.configVersion)
        store.update { $0.applyServerConfig(serverConfig) }
        await syncWarningSound(serverVersion: serverConfig.warningSoundVersion)
        let applied = store.snapshot()
        log("config applied: version=\(serverConfig.configVersion) locked=\(applied.locked) debug=\(applied.debugMode)")
    }

    private func syncWarningSound(serverVersion: String) async {
        let cachedVersion = store.snapshot().warningSoundVersion
        do {
            let result = try await WarningSound.sync(
                downloader: board,
                soundPath: soundPath,
                cachedVersion: cachedVersion,
                serverVersion: serverVersion
            )
            switch result {
            case .unchanged:
                return
            case .cleared:
                store.update { cfg in
                    guard !cfg.warningSoundVersion.isEmpty else { return false }
                    cfg.warningSoundVersion = ""
                    return true
                }
                log("warning sound: cleared (server has none)")
            case .updated(let version):
                // World-readable so the user-session agent can play it.
                try? FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: soundPath.path)
                store.update { cfg in
                    guard cfg.warningSoundVersion != version else { return false }
                    cfg.warningSoundVersion = version
                    return true
                }
                log("warning sound: downloaded version \(version)")
            }
        } catch {
            log("warning sound sync failed: \(error)")
        }
    }

    // MARK: Watchdog

    /// Windows parity: the service's watchdog respawns the agent every 15s.
    /// Here launchd's KeepAlive restarts a crashed agent, so the only job left
    /// is re-bootstrapping the LaunchAgent when a user `launchctl bootout`s it.
    private func watchdogLoop() async {
        while true {
            ensureAgentLoaded()
            await Daemon.sleep(seconds: 15)
        }
    }

    private func ensureAgentLoaded() {
        // Dev runs (no installed plist) and the login window have no agent to keep alive.
        guard FileManager.default.fileExists(atPath: AgentIPC.agentPlistPath) else { return }
        var uid: uid_t = 0
        var gid: gid_t = 0
        guard let user = SCDynamicStoreCopyConsoleUser(nil, &uid, &gid) as String?,
              user != "loginwindow", uid != 0 else {
            return
        }
        // ponytail: "already bootstrapped" is the common outcome; output discarded.
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/launchctl")
        process.arguments = ["bootstrap", "gui/\(uid)", AgentIPC.agentPlistPath]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try? process.run()
        process.waitUntilExit()
    }

    // MARK: Helpers

    private static let logTimestamp: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    private func log(_ message: String) {
        print("\(Daemon.logTimestamp.string(from: Date())) \(message)")
    }

    private static func sleep(seconds: Int) async {
        try? await Task.sleep(nanoseconds: UInt64(max(1, seconds)) * 1_000_000_000)
    }
}

/// Mutex-guarded runtime state shared between the HTTP handler thread and the
/// async loops (the Windows client's stateStore, in Swift). Persists the config
/// to disk whenever an update reports a change.
final class DaemonStateStore {
    private let lock = NSLock()
    private let path: URL
    private var current: Config
    private var lastReported: String?
    private var configVersion: String?

    init(path: URL, config: Config) {
        self.path = path
        self.current = config
    }

    func snapshot() -> Config {
        lock.lock()
        defer { lock.unlock() }
        return current
    }

    func setReported(_ state: String) {
        lock.lock()
        defer { lock.unlock() }
        lastReported = state
    }

    func reported() -> String? {
        lock.lock()
        defer { lock.unlock() }
        return lastReported
    }

    func setVersion(_ version: String) {
        lock.lock()
        defer { lock.unlock() }
        configVersion = version
    }

    func version() -> String? {
        lock.lock()
        defer { lock.unlock() }
        return configVersion
    }

    /// Apply a mutation; persist when it reports a change.
    func update(_ mutate: (inout Config) -> Bool) {
        lock.lock()
        defer { lock.unlock() }
        guard mutate(&current) else { return }
        do {
            try current.save(path: path)
        } catch {
            FileHandle.standardError.write(Data("config save failed: \(error)\n".utf8))
        }
    }
}
