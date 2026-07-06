import Foundation

/// Daemon <-> agent IPC contract: loopback HTTP with a shared-token header,
/// mirroring the Windows service <-> agent bridge (same port, same header, and
/// the same /agent/state + /agent/events endpoints and JSON field names).
enum AgentIPC {
    static let port: UInt16 = 48611
    static let baseURL = URL(string: "http://127.0.0.1:48611")!
    static let tokenHeader = "X-Rootaika-Agent-Token"
    static let agentLabel = "com.rootaika.agent"
    static let agentPlistPath = "/Library/LaunchAgents/com.rootaika.agent.plist"
}

/// Response of GET /agent/state: everything the user-session agent needs to
/// drive the overlay and observation loop. Field names mirror the Windows
/// agentStateResponse exactly.
struct AgentState: Codable, Equatable {
    var locked: Bool
    var lockMessage: String
    var lockWarningSeconds: Int
    var idleThresholdSeconds: Int
    var observeIntervalSeconds: Int
    var debugMode: Bool
    /// Absolute path of the daemon-cached warning MP3, "" when none is cached.
    var warningSoundPath: String

    enum CodingKeys: String, CodingKey {
        case locked
        case lockMessage = "lock_message"
        case lockWarningSeconds = "lock_warning_seconds"
        case idleThresholdSeconds = "idle_threshold_seconds"
        case observeIntervalSeconds = "observe_interval_seconds"
        case debugMode = "debug_mode"
        case warningSoundPath = "warning_sound_path"
    }

    /// Defaults used until the first successful daemon contact.
    static let fallback = AgentState(
        locked: false,
        lockMessage: "",
        lockWarningSeconds: 0,
        idleThresholdSeconds: 60,
        observeIntervalSeconds: 5,
        debugMode: false,
        warningSoundPath: ""
    )
}

/// Body of POST /agent/events.
struct AgentEventsRequest: Codable, Equatable {
    var events: [Event]
}

/// The loopback auth token, written by the daemon into a world-readable file
/// so the (unprivileged) user-session agent can present it. It only authorizes
/// the local state/events endpoints; the server credentials stay in the
/// root-only config.json.
enum AgentToken {
    static func fileURL(baseDir: URL) -> URL {
        baseDir.appendingPathComponent("agent-token", isDirectory: false)
    }

    static func load(baseDir: URL) -> String? {
        guard let raw = try? String(contentsOf: fileURL(baseDir: baseDir), encoding: .utf8) else {
            return nil
        }
        let token = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return token.isEmpty ? nil : token
    }

    /// Daemon side: reuse the existing token or mint and persist a fresh one.
    static func loadOrCreate(baseDir: URL) throws -> String {
        if let token = load(baseDir: baseDir), UUID(uuidString: token) != nil {
            return token
        }
        let token = UUID().uuidString
        let url = fileURL(baseDir: baseDir)
        try (token + "\n").write(to: url, atomically: true, encoding: .utf8)
        try? FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: url.path)
        return token
    }
}

/// Agent-side client for the daemon's loopback endpoint. The token is re-read
/// from disk per request (cheap at one call per observe tick) so an agent that
/// started before the daemon picks the token up as soon as it appears.
final class DaemonClient {
    enum DaemonError: Error, CustomStringConvertible {
        case tokenUnavailable
        case status(Int)

        var description: String {
            switch self {
            case .tokenUnavailable:
                return "agent token not readable (daemon not installed/started yet?)"
            case .status(let code):
                return "daemon responded with status \(code)"
            }
        }
    }

    private let session: URLSession

    init() {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 5
        config.timeoutIntervalForResource = 10
        session = URLSession(configuration: config)
    }

    func fetchState() async throws -> AgentState {
        let (data, response) = try await session.data(for: makeRequest("GET", "agent/state", body: nil))
        try ensureSuccess(response)
        return try RootaikaJSON.makeDecoder().decode(AgentState.self, from: data)
    }

    func postEvent(_ event: Event) async throws {
        let body = try RootaikaJSON.makeEncoder().encode(AgentEventsRequest(events: [event]))
        let (_, response) = try await session.data(for: makeRequest("POST", "agent/events", body: body))
        try ensureSuccess(response)
    }

    private func makeRequest(_ method: String, _ path: String, body: Data?) throws -> URLRequest {
        guard let token = AgentToken.load(baseDir: Config.daemonBaseDir()) else {
            throw DaemonError.tokenUnavailable
        }
        var request = URLRequest(url: AgentIPC.baseURL.appendingPathComponent(path))
        request.httpMethod = method
        request.setValue(token, forHTTPHeaderField: AgentIPC.tokenHeader)
        if let body = body {
            request.httpBody = body
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return request
    }

    private func ensureSuccess(_ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else {
            throw DaemonError.status(0)
        }
        guard (200...299).contains(http.statusCode) else {
            throw DaemonError.status(http.statusCode)
        }
    }
}
