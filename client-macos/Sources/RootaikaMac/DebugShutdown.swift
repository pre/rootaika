import Foundation

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

/// After a debug shutdown, launchd's KeepAlive relaunches the agent every
/// ThrottleInterval. This gate makes each relaunch exit immediately while the
/// daemon still reports locked debug mode (the admin is debugging), and clears
/// the marker once the state changed so the agent resumes. The daemon is asked
/// instead of the server: it owns the config now. Unreachable daemon + marker
/// means stay stopped, matching the old server-based behavior.
enum DebugShutdownGate {
    static func shouldStayStopped(daemon: DaemonClient) -> Bool {
        guard DebugShutdownMarker.exists() else { return false }
        guard let state = fetchState(daemon: daemon) else { return true }
        if !state.locked || !state.debugMode {
            DebugShutdownMarker.clear()
            return false
        }
        return true
    }

    /// Sync bridge for the async daemon call; main.swift dispatch is synchronous.
    private static func fetchState(daemon: DaemonClient) -> AgentState? {
        let semaphore = DispatchSemaphore(value: 0)
        let result = LockedBox<AgentState?>(nil)
        Task.detached {
            result.set(try? await daemon.fetchState())
            semaphore.signal()
        }
        if semaphore.wait(timeout: .now() + 6) == .timedOut {
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
