import Foundation

/// Server-facing network client. Implemented by NetworkBoardClient.
/// The Core agent loop depends ONLY on this abstraction.
protocol BoardClienting {
    /// Upload a batch of activity events (POST /api/v1/events/batch). Returns the
    /// server's accounting (accepted / duplicate-or-ignored / device_id) so the
    /// agent can surface it in the debug trace.
    @discardableResult
    func postEvents(_ batch: EventBatch) async throws -> EventBatchResponse

    /// Long-poll for effective config + lock state (GET /api/v1/client/config).
    /// - Parameters:
    ///   - clientID: the persistent device UUID.
    ///   - status: optional self-reported status ("active"|"idle"|"locked").
    ///   - knownVersion: the config_version from the previous response (for long-poll).
    ///   - waitSeconds: long-poll budget in seconds (clamped server-side to 0..60).
    func fetchConfig(
        clientID: String,
        status: String?,
        knownVersion: String?,
        waitSeconds: Int
    ) async throws -> ClientConfig

    /// Download the admin-uploaded lock-warning MP3 (GET /api/v1/warning-sound).
    /// Returns the raw audio bytes; throws on a 404 (no sound configured) or any
    /// transport error so the caller can leave the cache untouched.
    func downloadWarningSound() async throws -> Data
}

/// Samples local user activity. Implemented by MacActivityProbe.
protocol ActivityProbing {
    /// System-wide seconds since the last user input event.
    func idleSeconds() -> Double

    /// Lowercased basename / bundle id of the frontmost application, or nil if none.
    func frontmostProcessName() -> String?
}

/// Sink for human-readable debug trace lines. Implemented by MacDebugConsole
/// (an on-screen terminal window) and by stderr. The agent logs through this so
/// that, in debug mode, the user sees exactly what the client observes, reports,
/// and receives back, mirroring the Windows client's debug console.
protocol DebugLogging {
    /// Append one line to the trace. Safe to call from any thread/actor.
    func log(_ line: String)

    /// Show or hide the on-screen console. A no-op for sinks without UI (stderr).
    func setVisible(_ visible: Bool)
}

/// Drives the full-screen lock overlay. Implemented by MacLockController.
protocol LockControlling {
    /// Show (or refresh) the lock overlay with the given admin message.
    /// warningSeconds is informational; the countdown is owned by the caller.
    func showLock(message: String, warningSeconds: Int)

    /// Tear down the lock overlay.
    func hideLock()

    /// Whether the overlay is currently engaged.
    var isShowing: Bool { get }

    /// Show / refresh the pre-lock warning banner with the remaining seconds.
    func showWarning(message: String, remainingSeconds: Int)

    /// Tear down the warning banner and stop its sound.
    func hideWarning()

    /// Start looping the warning MP3 at `path` for the countdown (best-effort).
    func startWarningAudio(path: String)

    /// Stop the looping warning sound.
    func stopWarningAudio()
}
