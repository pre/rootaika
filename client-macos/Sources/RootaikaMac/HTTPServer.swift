import Foundation
import Network

/// Minimal loopback HTTP/1.1 server for the daemon <-> agent IPC. Serves one
/// small JSON request per connection (Connection: close), reads bodies via
/// Content-Length, and hands parsed requests to a synchronous handler that
/// runs on the server's dispatch queue.
///
/// ponytail: intentionally not a general HTTP server — no keep-alive, no
/// chunked encoding, no TLS. The only client is our own agent on 127.0.0.1.
final class AgentHTTPServer {
    struct Request {
        let method: String
        let path: String
        /// Header names lowercased.
        let headers: [String: String]
        let body: Data
    }

    struct Response {
        let status: Int
        let body: Data

        static func json<T: Encodable>(_ status: Int, _ value: T) -> Response {
            Response(status: status, body: (try? RootaikaJSON.makeEncoder().encode(value)) ?? Data())
        }
    }

    typealias Handler = (Request) -> Response

    private let listener: NWListener
    private let queue = DispatchQueue(label: "rootaika.agent-http")
    private let handler: Handler
    /// Requests are tiny JSON; anything bigger than this is a broken client.
    private static let maxRequestBytes = 1_048_576

    init(port: UInt16, handler: @escaping Handler, onFailure: @escaping (Error) -> Void) throws {
        let parameters = NWParameters.tcp
        parameters.allowLocalEndpointReuse = true
        parameters.requiredLocalEndpoint = NWEndpoint.hostPort(
            host: .ipv4(.loopback),
            port: NWEndpoint.Port(rawValue: port)!
        )
        listener = try NWListener(using: parameters)
        self.handler = handler
        listener.newConnectionHandler = { [weak self] connection in
            self?.serve(connection)
        }
        listener.stateUpdateHandler = { state in
            if case .failed(let error) = state {
                onFailure(error)
            }
        }
    }

    func start() {
        listener.start(queue: queue)
    }

    func stop() {
        listener.cancel()
    }

    private func serve(_ connection: NWConnection) {
        connection.start(queue: queue)
        receive(connection, accumulated: Data())
    }

    private func receive(_ connection: NWConnection, accumulated: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [weak self] data, _, isComplete, error in
            guard let self = self else {
                connection.cancel()
                return
            }
            var buffer = accumulated
            if let data = data {
                buffer.append(data)
            }
            if let request = AgentHTTPServer.parseRequest(buffer) {
                self.send(connection, self.handler(request))
                return
            }
            if error != nil || isComplete || buffer.count > AgentHTTPServer.maxRequestBytes {
                connection.cancel()
                return
            }
            self.receive(connection, accumulated: buffer)
        }
    }

    /// Parse a complete HTTP request from `data`. Returns nil while the request
    /// is still incomplete (caller keeps receiving) or on garbage (caller ends
    /// up cancelling the connection when the peer closes).
    static func parseRequest(_ data: Data) -> Request? {
        guard let headerEnd = data.range(of: Data("\r\n\r\n".utf8)) else { return nil }
        guard let head = String(data: data[..<headerEnd.lowerBound], encoding: .utf8) else { return nil }

        let lines = head.components(separatedBy: "\r\n")
        let requestLine = lines[0].split(separator: " ")
        guard requestLine.count >= 2 else { return nil }

        var headers: [String: String] = [:]
        for line in lines.dropFirst() {
            guard let colon = line.firstIndex(of: ":") else { continue }
            let name = line[..<colon].lowercased()
            let value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
            headers[name] = value
        }

        let contentLength = Int(headers["content-length"] ?? "0") ?? 0
        let bodyStart = headerEnd.upperBound
        guard data.count - bodyStart >= contentLength else { return nil }
        let body = data.subdata(in: bodyStart..<(bodyStart + contentLength))

        // Strip any query string; the IPC endpoints take none.
        let target = String(requestLine[1])
        let path = target.split(separator: "?", maxSplits: 1)[0]
        return Request(method: String(requestLine[0]), path: String(path), headers: headers, body: body)
    }

    private func send(_ connection: NWConnection, _ response: Response) {
        let head = "HTTP/1.1 \(response.status) \(AgentHTTPServer.statusText(response.status))\r\n"
            + "Content-Type: application/json\r\n"
            + "Content-Length: \(response.body.count)\r\n"
            + "Connection: close\r\n\r\n"
        var payload = Data(head.utf8)
        payload.append(response.body)
        connection.send(content: payload, completion: .contentProcessed { _ in
            connection.cancel()
        })
    }

    private static func statusText(_ status: Int) -> String {
        switch status {
        case 200: return "OK"
        case 202: return "Accepted"
        case 400: return "Bad Request"
        case 401: return "Unauthorized"
        case 404: return "Not Found"
        case 405: return "Method Not Allowed"
        default: return "Status"
        }
    }
}
