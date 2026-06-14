import Foundation

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// Server-facing HTTP client. Implements the rootaika server contract using
/// URLSession async APIs with HTTP Basic Auth, JSON bodies, and exponential
/// backoff retry for transient failures.
///
/// Endpoints:
///   - POST /api/v1/events/batch  (postEvents)
///   - GET  /api/v1/client/config (fetchConfig, long-poll)
final class NetworkBoardClient: BoardClienting {
    private let config: Config
    private let session: URLSession

    /// Retry policy (mirrors the Windows reference client):
    /// 4 attempts, base 500ms doubling, capped at 5s -> delays 0.5s, 1s, 2s.
    private let maxAttempts = 4
    private let baseBackoff: TimeInterval = 0.5
    private let maxBackoff: TimeInterval = 5.0

    /// Fixed HTTP transport timeout for the non-long-poll (events) requests.
    private let standardTimeout: TimeInterval = 10.0
    /// Extra headroom added above the long-poll wait budget so the transport
    /// never times out before the server returns. (server caps wait at 60s.)
    private let pollTimeoutHeadroom: TimeInterval = 10.0

    init(config: Config) {
        self.config = config
        let sessionConfig = URLSessionConfiguration.ephemeral
        // Per-request timeouts are set on each URLRequest; keep the resource
        // timeout generous so long polls are not cut short.
        sessionConfig.timeoutIntervalForRequest = 90
        sessionConfig.timeoutIntervalForResource = 120
        sessionConfig.httpAdditionalHeaders = ["Accept": "application/json"]
        // The board server runs on an ESP-AT WiFi co-processor with only 5
        // simultaneous TCP links total. URLSession defaults to up to 6 parallel
        // connections per host, which exhausts and wedges that pool. Force a
        // single connection so all requests are serialized over one link.
        sessionConfig.httpMaximumConnectionsPerHost = 1
        self.session = URLSession(configuration: sessionConfig)
    }

    // MARK: - Public API

    @discardableResult
    func postEvents(_ batch: EventBatch) async throws -> EventBatchResponse {
        let url = try makeURL(path: "/api/v1/events/batch", queryItems: nil)
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = standardTimeout
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        applyBasicAuth(&request)

        let encoder = RootaikaJSON.makeEncoder()
        request.httpBody = try encoder.encode(batch)

        let (data, response) = try await performWithRetry(request, timeout: standardTimeout)
        try ensureSuccess(data: data, response: response)
        // Response body decode is best-effort; a successful HTTP status already
        // means the batch was stored. Fall back to a zeroed response so the
        // caller (debug trace) still has a value to print.
        return (try? RootaikaJSON.makeDecoder().decode(EventBatchResponse.self, from: data))
            ?? EventBatchResponse(accepted: batch.events.count, duplicateOrIgnored: 0, deviceID: 0)
    }

    func fetchConfig(
        clientID: String,
        status: String?,
        knownVersion: String?,
        waitSeconds: Int
    ) async throws -> ClientConfig {
        let clampedWait = max(0, min(60, waitSeconds))

        var query: [URLQueryItem] = [URLQueryItem(name: "client_id", value: clientID)]
        if let status = status, !status.isEmpty {
            query.append(URLQueryItem(name: "status", value: status))
        }
        query.append(URLQueryItem(name: "wait", value: String(clampedWait)))
        if let version = knownVersion, !version.isEmpty {
            query.append(URLQueryItem(name: "version", value: version))
        }

        let url = try makeURL(path: "/api/v1/client/config", queryItems: query)
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        // Transport timeout MUST exceed the long-poll wait budget.
        let timeout = TimeInterval(clampedWait) + pollTimeoutHeadroom
        request.timeoutInterval = timeout
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        applyBasicAuth(&request)

        let (data, response) = try await performWithRetry(request, timeout: timeout)
        try ensureSuccess(data: data, response: response)
        return try RootaikaJSON.makeDecoder().decode(ClientConfig.self, from: data)
    }

    /// Download the admin-uploaded warning MP3. The body is audio/mpeg, returned
    /// verbatim. A 404 (no sound set) surfaces as a thrown NetworkError so the
    /// caller leaves any existing cache untouched on transient failures and only
    /// clears it on an explicit empty server version (handled in syncWarningSound).
    func downloadWarningSound() async throws -> Data {
        let url = try makeURL(path: "/api/v1/warning-sound", queryItems: nil)
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.timeoutInterval = standardTimeout
        request.setValue("*/*", forHTTPHeaderField: "Accept")
        applyBasicAuth(&request)

        let (data, response) = try await performWithRetry(request, timeout: standardTimeout)
        try ensureSuccess(data: data, response: response)
        return data
    }

    // MARK: - Request building

    private func makeURL(path: String, queryItems: [URLQueryItem]?) throws -> URL {
        let base = config.serverURL.hasSuffix("/")
            ? String(config.serverURL.dropLast())
            : config.serverURL
        guard var components = URLComponents(string: base + path) else {
            throw NetworkError.invalidURL(base + path)
        }
        if let queryItems = queryItems, !queryItems.isEmpty {
            components.queryItems = queryItems
        }
        guard let url = components.url else {
            throw NetworkError.invalidURL(base + path)
        }
        return url
    }

    private func applyBasicAuth(_ request: inout URLRequest) {
        let raw = "\(config.clientUser):\(config.clientPassword)"
        let encoded = Data(raw.utf8).base64EncodedString()
        request.setValue("Basic \(encoded)", forHTTPHeaderField: "Authorization")
    }

    // MARK: - Retry / backoff

    /// Perform the request, retrying transient failures (network/transport
    /// errors, HTTP 5xx, HTTP 429) with exponential backoff. 4xx and decode
    /// errors are terminal. Honors task cancellation.
    private func performWithRetry(
        _ request: URLRequest,
        timeout: TimeInterval
    ) async throws -> (Data, URLResponse) {
        var attempt = 0
        var lastError: Error = NetworkError.unknown

        while attempt < maxAttempts {
            attempt += 1
            try Task.checkCancellation()

            do {
                let (data, response) = try await session.data(for: request)
                guard let http = response as? HTTPURLResponse else {
                    // Non-HTTP response: treat as transport error, retryable.
                    lastError = NetworkError.nonHTTPResponse
                    if attempt < maxAttempts {
                        try await backoffDelay(forAttempt: attempt)
                        continue
                    }
                    throw lastError
                }

                if isRetryableStatus(http.statusCode), attempt < maxAttempts {
                    lastError = NetworkError.serverStatus(http.statusCode)
                    try await backoffDelay(forAttempt: attempt)
                    continue
                }

                // Either success, a terminal 4xx, or last attempt on a 5xx/429.
                return (data, response)
            } catch let error as CancellationError {
                // Cancellation is terminal.
                throw error
            } catch let urlError as URLError {
                // ctx cancellation surfaces as URLError.cancelled -> terminal.
                if urlError.code == .cancelled {
                    throw urlError
                }
                lastError = urlError
                if attempt < maxAttempts {
                    try await backoffDelay(forAttempt: attempt)
                    continue
                }
                throw urlError
            }
        }

        throw lastError
    }

    private func isRetryableStatus(_ status: Int) -> Bool {
        return status == 429 || (status >= 500 && status <= 599)
    }

    /// Backoff before the NEXT attempt. attempt is the just-completed attempt
    /// number (1-based), so the delay for attempts 2/3/4 is 0.5s, 1s, 2s.
    private func backoffDelay(forAttempt attempt: Int) async throws {
        let exponent = max(0, attempt - 1)
        let raw = baseBackoff * pow(2.0, Double(exponent))
        let delay = min(maxBackoff, raw)
        try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
    }

    // MARK: - Response validation

    private func ensureSuccess(data: Data, response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse else {
            throw NetworkError.nonHTTPResponse
        }
        let status = http.statusCode
        guard (200...299).contains(status) else {
            // Try the standard {"error": string} envelope first.
            if let apiError = try? RootaikaJSON.makeDecoder().decode(APIError.self, from: data) {
                throw NetworkError.api(status: status, message: apiError.error)
            }
            let body = String(data: data, encoding: .utf8) ?? ""
            throw NetworkError.api(status: status, message: body)
        }
    }
}

/// Errors raised by NetworkBoardClient beyond the server's {"error": ...} envelope.
enum NetworkError: Error, CustomStringConvertible {
    case invalidURL(String)
    case nonHTTPResponse
    case serverStatus(Int)
    case api(status: Int, message: String)
    case unknown

    var description: String {
        switch self {
        case .invalidURL(let url):
            return "invalid URL: \(url)"
        case .nonHTTPResponse:
            return "non-HTTP response"
        case .serverStatus(let code):
            return "server status \(code)"
        case .api(let status, let message):
            return "API error (\(status)): \(message)"
        case .unknown:
            return "unknown network error"
        }
    }
}
