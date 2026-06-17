import Foundation

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

enum DebugShutdownMarker {
    static func path() -> URL {
        Config.defaultBaseDir().appendingPathComponent("debug-shutdown-requested", isDirectory: false)
    }

    static func exists() -> Bool {
        FileManager.default.fileExists(atPath: path().path)
    }

    static func request() {
        let url = path()
        do {
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true,
                attributes: [.posixPermissions: 0o700]
            )
            let body = ISO8601DateFormatter().string(from: Date()) + "\n"
            try body.write(to: url, atomically: true, encoding: .utf8)
            try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        } catch {
            // A failed marker write must not leave the debug button unresponsive.
        }
    }

    static func clear() {
        try? FileManager.default.removeItem(at: path())
    }
}

enum DebugShutdownGate {
    static func shouldStayStopped(config: Config) -> Bool {
        guard DebugShutdownMarker.exists() else { return false }
        guard let serverConfig = fetchServerConfig(config: config) else { return true }
        if !serverConfig.locked || !serverConfig.debugMode {
            DebugShutdownMarker.clear()
            return false
        }
        return true
    }

    private static func fetchServerConfig(config: Config) -> ClientConfig? {
        let base = config.serverURL.hasSuffix("/")
            ? String(config.serverURL.dropLast())
            : config.serverURL
        guard var components = URLComponents(string: base + "/api/v1/client/config") else {
            return nil
        }
        components.queryItems = [
            URLQueryItem(name: "client_id", value: config.clientID),
            URLQueryItem(name: "wait", value: "0")
        ]
        guard let url = components.url else { return nil }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.timeoutInterval = 5
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let auth = "\(config.clientUser):\(config.clientPassword)"
        request.setValue("Basic \(Data(auth.utf8).base64EncodedString())", forHTTPHeaderField: "Authorization")

        let semaphore = DispatchSemaphore(value: 0)
        let result = LockedBox<ClientConfig?>(nil)
        let task = URLSession.shared.dataTask(with: request) { data, response, _ in
            defer { semaphore.signal() }
            guard let http = response as? HTTPURLResponse,
                  (200...299).contains(http.statusCode),
                  let data = data else {
                result.set(nil)
                return
            }
            result.set(try? RootaikaJSON.makeDecoder().decode(ClientConfig.self, from: data))
        }
        task.resume()
        if semaphore.wait(timeout: .now() + 6) == .timedOut {
            task.cancel()
            return nil
        }
        return result.get()
    }
}

private final class LockedBox<Value> {
    private let lock = NSLock()
    private var value: Value

    init(_ value: Value) {
        self.value = value
    }

    func set(_ value: Value) {
        lock.lock()
        self.value = value
        lock.unlock()
    }

    func get() -> Value {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}
