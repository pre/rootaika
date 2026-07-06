import Foundation

/// Activity state reported per observation. Mirrors model.ActivityState on the server.
/// Wire values MUST be exactly "active" | "idle" | "locked".
enum ActivityState: String, Codable, Equatable {
    case active
    case idle
    case locked
}

/// A single activity observation. Maps 1:1 to the server's batch event object.
/// Field names use snake_case via CodingKeys to match the server JSON exactly.
struct Event: Codable, Equatable {
    /// Unique UUID per event; dedup key on the server (event_id).
    var eventID: String
    /// MUST equal "activity_observed".
    var type: String
    /// RFC3339 timestamp (UTC, fractional seconds + Z).
    var occurredAt: Date
    /// "active" | "idle" | "locked".
    var state: ActivityState
    /// Only meaningful when state == active; defaults to "unknown" if active+blank server-side.
    var processName: String?
    /// Ordering tiebreaker; assigned server-side (here: by the local buffer). Defaults to 0.
    var sequence: Int64

    enum CodingKeys: String, CodingKey {
        case eventID = "event_id"
        case type
        case occurredAt = "occurred_at"
        case state
        case processName = "process_name"
        case sequence
    }

    static let typeActivityObserved = "activity_observed"

    init(
        eventID: String,
        type: String = Event.typeActivityObserved,
        occurredAt: Date,
        state: ActivityState,
        processName: String? = nil,
        sequence: Int64 = 0
    ) {
        self.eventID = eventID
        self.type = type
        self.occurredAt = occurredAt
        self.state = state
        self.processName = processName
        self.sequence = sequence
    }
}

/// Body of POST /api/v1/events/batch.
struct EventBatch: Codable, Equatable {
    /// The persistent device UUID.
    var clientID: String
    /// 1..10000 events, non-empty.
    var events: [Event]

    enum CodingKeys: String, CodingKey {
        case clientID = "client_id"
        case events
    }
}

/// Response of POST /api/v1/events/batch.
struct EventBatchResponse: Codable, Equatable {
    var accepted: Int
    var duplicateOrIgnored: Int
    var deviceID: Int64

    enum CodingKeys: String, CodingKey {
        case accepted
        case duplicateOrIgnored = "duplicate_or_ignored"
        case deviceID = "device_id"
    }
}

/// A program-name -> category mapping rule from the config payload.
struct CategoryRule: Codable, Equatable {
    /// "exact" | "prefix" | "contains".
    var matchType: String
    var pattern: String
    var category: String

    enum CodingKeys: String, CodingKey {
        case matchType = "match_type"
        case pattern
        case category
    }
}

/// Response of GET /api/v1/client/config (the long-poll payload).
struct ClientConfig: Codable, Equatable {
    var clientID: String
    /// 16-hex fingerprint; pass back as `version` on the next poll.
    var configVersion: String
    var idleThresholdSeconds: Int
    var uploadIntervalSeconds: Int
    var pollIntervalSeconds: Int
    var maxCountableGapSeconds: Int
    var debugMode: Bool
    /// Continuous lock STATE (not a one-shot command).
    var locked: Bool
    /// Overlay text; empty when unlocked.
    var lockMessage: String
    /// Countdown before enforcing the lock; 0 = lock immediately.
    var warningSeconds: Int
    /// Opaque version of the admin-uploaded warning MP3; empty when none is set.
    /// Changes whenever the admin uploads/removes a sound, so the client knows to
    /// re-download. Decoded leniently so an older server omitting it is tolerated.
    var warningSoundVersion: String
    /// OTA update directives (transient, never persisted to config.json): the
    /// release tag, asset name and SHA256 the daemon should be running. All
    /// empty means no update is desired.
    var desiredVersion: String
    var artifactName: String
    var sha256: String
    var categories: [CategoryRule]

    enum CodingKeys: String, CodingKey {
        case clientID = "client_id"
        case configVersion = "config_version"
        case idleThresholdSeconds = "idle_threshold_seconds"
        case uploadIntervalSeconds = "upload_interval_seconds"
        case pollIntervalSeconds = "poll_interval_seconds"
        case maxCountableGapSeconds = "max_countable_gap_seconds"
        case debugMode = "debug_mode"
        case locked
        case lockMessage = "lock_message"
        case warningSeconds = "warning_seconds"
        case warningSoundVersion = "warning_sound_version"
        case desiredVersion = "desired_version"
        case artifactName = "artifact_name"
        case sha256
        case categories
    }

    init(
        clientID: String,
        configVersion: String,
        idleThresholdSeconds: Int,
        uploadIntervalSeconds: Int,
        pollIntervalSeconds: Int,
        maxCountableGapSeconds: Int,
        debugMode: Bool,
        locked: Bool,
        lockMessage: String,
        warningSeconds: Int,
        warningSoundVersion: String = "",
        desiredVersion: String = "",
        artifactName: String = "",
        sha256: String = "",
        categories: [CategoryRule]
    ) {
        self.clientID = clientID
        self.configVersion = configVersion
        self.idleThresholdSeconds = idleThresholdSeconds
        self.uploadIntervalSeconds = uploadIntervalSeconds
        self.pollIntervalSeconds = pollIntervalSeconds
        self.maxCountableGapSeconds = maxCountableGapSeconds
        self.debugMode = debugMode
        self.locked = locked
        self.lockMessage = lockMessage
        self.warningSeconds = warningSeconds
        self.warningSoundVersion = warningSoundVersion
        self.desiredVersion = desiredVersion
        self.artifactName = artifactName
        self.sha256 = sha256
        self.categories = categories
    }

    // Lenient decode so a server that omits warning_sound_version (older build)
    // still decodes; the field then defaults to "" (no sound).
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        clientID = try c.decode(String.self, forKey: .clientID)
        configVersion = try c.decode(String.self, forKey: .configVersion)
        idleThresholdSeconds = try c.decode(Int.self, forKey: .idleThresholdSeconds)
        uploadIntervalSeconds = try c.decode(Int.self, forKey: .uploadIntervalSeconds)
        pollIntervalSeconds = try c.decode(Int.self, forKey: .pollIntervalSeconds)
        maxCountableGapSeconds = try c.decode(Int.self, forKey: .maxCountableGapSeconds)
        debugMode = try c.decode(Bool.self, forKey: .debugMode)
        locked = try c.decode(Bool.self, forKey: .locked)
        lockMessage = try c.decode(String.self, forKey: .lockMessage)
        warningSeconds = try c.decode(Int.self, forKey: .warningSeconds)
        warningSoundVersion = try c.decodeIfPresent(String.self, forKey: .warningSoundVersion) ?? ""
        desiredVersion = try c.decodeIfPresent(String.self, forKey: .desiredVersion) ?? ""
        artifactName = try c.decodeIfPresent(String.self, forKey: .artifactName) ?? ""
        sha256 = try c.decodeIfPresent(String.self, forKey: .sha256) ?? ""
        categories = try c.decodeIfPresent([CategoryRule].self, forKey: .categories) ?? []
    }
}

/// Standard API error envelope: {"error": string}.
struct APIError: Codable, Equatable, Error {
    var error: String
}

enum RootaikaJSON {
    /// Encoder/decoder configured to match the server's RFC3339/ISO8601 (UTC, fractional seconds) wire format.
    static func makeEncoder() -> JSONEncoder {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(rfc3339String(from: date))
        }
        return enc
    }

    static func makeDecoder() -> JSONDecoder {
        let dec = JSONDecoder()
        dec.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let str = try container.decode(String.self)
            guard let date = rfc3339Date(from: str) else {
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription: "Invalid RFC3339 timestamp: \(str)"
                )
            }
            return date
        }
        return dec
    }

    private static let fractionalFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        f.timeZone = TimeZone(identifier: "UTC")
        return f
    }()

    private static let plainFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        f.timeZone = TimeZone(identifier: "UTC")
        return f
    }()

    static func rfc3339String(from date: Date) -> String {
        fractionalFormatter.string(from: date)
    }

    static func rfc3339Date(from string: String) -> Date? {
        fractionalFormatter.date(from: string) ?? plainFormatter.date(from: string)
    }
}
