import Foundation

/// Client configuration, persisted as pretty-printed JSON under
/// ~/Library/Application Support/rootaika/config.json.
/// Mirrors the Windows config.Config schema/defaults; generates and persists
/// client_id once on first run. Env overrides are applied after defaults.
struct Config: Codable, Equatable {
    static let defaultServerURL = "http://192.168.68.199:8080"
    static let legacyLocalServerURL = "http://127.0.0.1:8080"

    var serverURL: String
    var clientUser: String
    var clientPassword: String
    /// Persistent device identity UUID, generated once.
    var clientID: String

    var idleThresholdSeconds: Int
    var uploadIntervalSeconds: Int
    var pollIntervalSeconds: Int
    var pollWaitSeconds: Int
    var observeIntervalSeconds: Int
    var maxCountableGapSeconds: Int
    var batchSize: Int

    var locked: Bool
    var lockMessage: String
    var lockWarningSeconds: Int
    var debugMode: Bool
    /// Version of the locally cached warning MP3, last reconciled with the
    /// server. Empty means no sound is cached. Managed by syncWarningSound, not
    /// applyServerConfig (mirrors the Windows client).
    var warningSoundVersion: String

    enum CodingKeys: String, CodingKey {
        case serverURL = "server_url"
        case clientUser = "client_username"
        case clientPassword = "client_password"
        case clientID = "client_id"
        case idleThresholdSeconds = "idle_threshold_seconds"
        case uploadIntervalSeconds = "upload_interval_seconds"
        case pollIntervalSeconds = "poll_interval_seconds"
        case pollWaitSeconds = "poll_wait_seconds"
        case observeIntervalSeconds = "observe_interval_seconds"
        case maxCountableGapSeconds = "max_countable_gap_seconds"
        case batchSize = "batch_size"
        case locked
        case lockMessage = "lock_message"
        case lockWarningSeconds = "lock_warning_seconds"
        case debugMode = "debug_mode"
        case warningSoundVersion = "warning_sound_version"
    }

    // MARK: Defaults

    static func makeDefault() -> Config {
        Config(
            serverURL: defaultServerURL,
            clientUser: "client",
            clientPassword: "client",
            clientID: UUID().uuidString,
            idleThresholdSeconds: 60,
            uploadIntervalSeconds: 60,
            pollIntervalSeconds: 30,
            pollWaitSeconds: 25,
            observeIntervalSeconds: 5,
            maxCountableGapSeconds: 300,
            batchSize: 100,
            locked: false,
            lockMessage: "",
            lockWarningSeconds: 0,
            debugMode: false,
            warningSoundVersion: ""
        )
    }

    // MARK: Paths

    static func defaultBaseDir() -> URL {
        if let home = ProcessInfo.processInfo.environment["ROOTAIKA_HOME"], !home.isEmpty {
            return URL(fileURLWithPath: home, isDirectory: true)
        }
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSHomeDirectory() + "/Library/Application Support", isDirectory: true)
        return appSupport.appendingPathComponent("rootaika", isDirectory: true)
    }

    static func defaultPath() -> URL {
        defaultBaseDir().appendingPathComponent("config.json", isDirectory: false)
    }

    /// Base dir for the root daemon's config/db/sound/token. ROOTAIKA_HOME
    /// overrides it (shared with defaultBaseDir) so dev runs work without root.
    static func daemonBaseDir() -> URL {
        if let home = ProcessInfo.processInfo.environment["ROOTAIKA_HOME"], !home.isEmpty {
            return URL(fileURLWithPath: home, isDirectory: true)
        }
        return URL(fileURLWithPath: "/Library/Application Support/rootaika", isDirectory: true)
    }

    // MARK: Load / Save

    /// Load config from `path` (or defaultPath()). Generates a new config with a
    /// fresh client_id if absent, fills missing defaults, applies env overrides,
    /// and persists if anything changed.
    static func load(path: URL? = nil) throws -> Config {
        let url = path ?? defaultPath()
        var config: Config
        var changed = false

        if let data = try? Data(contentsOf: url) {
            let dec = JSONDecoder()
            config = (try? dec.decode(Config.self, from: data)) ?? Config.makeDefault()
            if (try? dec.decode(Config.self, from: data)) == nil { changed = true }
            if UUID(uuidString: config.clientID) == nil {
                config.clientID = UUID().uuidString
                changed = true
            }
            if config.serverURL == Config.legacyLocalServerURL {
                config.serverURL = Config.defaultServerURL
                changed = true
            }
        } else {
            config = Config.makeDefault()
            changed = true
        }

        if config.applyEnvOverrides() { changed = true }

        if changed {
            try config.save(path: url)
        }
        return config
    }

    func save(path: URL? = nil) throws {
        let url = path ?? Config.defaultPath()
        let dir = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(
            at: dir,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
        let enc = JSONEncoder()
        enc.outputFormatting = [.prettyPrinted, .sortedKeys]
        var data = try enc.encode(self)
        data.append(0x0A) // trailing newline
        try data.write(to: url, options: .atomic)
        try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    }

    // MARK: Env / flag overrides

    /// Apply ROOTAIKA_* env overrides. Returns true if any field changed.
    @discardableResult
    mutating func applyEnvOverrides() -> Bool {
        var changed = false
        let env = ProcessInfo.processInfo.environment
        if let v = env["ROOTAIKA_SERVER_URL"], !v.isEmpty, v != serverURL {
            serverURL = v; changed = true
        }
        if let v = env["ROOTAIKA_CLIENT_USERNAME"], !v.isEmpty, v != clientUser {
            clientUser = v; changed = true
        }
        if let v = env["ROOTAIKA_CLIENT_PASSWORD"], !v.isEmpty, v != clientPassword {
            clientPassword = v; changed = true
        }
        return changed
    }

    /// Apply a server config payload from the long-poll. Only overwrites int knobs
    /// when the server value is > 0 and differs; lock state follows `locked`.
    /// Returns true if any field changed.
    @discardableResult
    mutating func applyServerConfig(_ server: ClientConfig) -> Bool {
        var changed = false
        func setIfPositive(_ value: Int, _ keyPath: WritableKeyPath<Config, Int>) {
            if value > 0 && self[keyPath: keyPath] != value {
                self[keyPath: keyPath] = value
                changed = true
            }
        }
        setIfPositive(server.idleThresholdSeconds, \.idleThresholdSeconds)
        setIfPositive(server.uploadIntervalSeconds, \.uploadIntervalSeconds)
        setIfPositive(server.pollIntervalSeconds, \.pollIntervalSeconds)
        setIfPositive(server.maxCountableGapSeconds, \.maxCountableGapSeconds)

        if debugMode != server.debugMode {
            debugMode = server.debugMode; changed = true
        }

        if locked != server.locked {
            locked = server.locked; changed = true
        }
        if locked {
            if lockMessage != server.lockMessage { lockMessage = server.lockMessage; changed = true }
            if lockWarningSeconds != server.warningSeconds { lockWarningSeconds = server.warningSeconds; changed = true }
        } else {
            if !lockMessage.isEmpty { lockMessage = ""; changed = true }
            if lockWarningSeconds != 0 { lockWarningSeconds = 0; changed = true }
        }
        return changed
    }
}

// MARK: Schema-tolerant decoding
//
// Decode field-by-field with defaults so a config written by an older/newer
// build that lacks a key never fails the whole decode. This preserves the
// persistent client_id (device identity) and custom settings across upgrades
// instead of silently resetting to defaults. Defined in an extension so the
// memberwise initializer used by makeDefault() is kept.
extension Config {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = Config.makeDefault()
        serverURL = try c.decodeIfPresent(String.self, forKey: .serverURL) ?? d.serverURL
        clientUser = try c.decodeIfPresent(String.self, forKey: .clientUser) ?? d.clientUser
        clientPassword = try c.decodeIfPresent(String.self, forKey: .clientPassword) ?? d.clientPassword
        clientID = try c.decodeIfPresent(String.self, forKey: .clientID) ?? d.clientID
        idleThresholdSeconds = try c.decodeIfPresent(Int.self, forKey: .idleThresholdSeconds) ?? d.idleThresholdSeconds
        uploadIntervalSeconds = try c.decodeIfPresent(Int.self, forKey: .uploadIntervalSeconds) ?? d.uploadIntervalSeconds
        pollIntervalSeconds = try c.decodeIfPresent(Int.self, forKey: .pollIntervalSeconds) ?? d.pollIntervalSeconds
        pollWaitSeconds = try c.decodeIfPresent(Int.self, forKey: .pollWaitSeconds) ?? d.pollWaitSeconds
        observeIntervalSeconds = try c.decodeIfPresent(Int.self, forKey: .observeIntervalSeconds) ?? d.observeIntervalSeconds
        maxCountableGapSeconds = try c.decodeIfPresent(Int.self, forKey: .maxCountableGapSeconds) ?? d.maxCountableGapSeconds
        batchSize = try c.decodeIfPresent(Int.self, forKey: .batchSize) ?? d.batchSize
        locked = try c.decodeIfPresent(Bool.self, forKey: .locked) ?? d.locked
        lockMessage = try c.decodeIfPresent(String.self, forKey: .lockMessage) ?? d.lockMessage
        lockWarningSeconds = try c.decodeIfPresent(Int.self, forKey: .lockWarningSeconds) ?? d.lockWarningSeconds
        debugMode = try c.decodeIfPresent(Bool.self, forKey: .debugMode) ?? d.debugMode
        warningSoundVersion = try c.decodeIfPresent(String.self, forKey: .warningSoundVersion) ?? d.warningSoundVersion
    }
}
