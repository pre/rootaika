import Foundation

/// Reconciles the locally cached warning MP3 with the server's reported version.
/// Mirrors the Windows client's syncWarningSound (warning_sound.go):
///   - Same version cached  -> .unchanged (no-op).
///   - Empty server version -> admin removed/never set a sound; clear the cache
///                             so the agent falls back to silence -> .cleared.
///   - Differing version    -> download and atomically replace the file, then
///                             report the new version -> .updated.
///
/// On a download error the cache is left untouched (a transient failure never
/// loses a working sound) and the error is rethrown. The function returns a
/// result rather than mutating Config so the caller, an actor, never holds an
/// `inout` to an isolated property across the `await`.
enum WarningSound {
    enum SyncResult: Equatable {
        case unchanged
        case cleared
        case updated(version: String)
    }

    static func sync(
        downloader: BoardClienting,
        soundPath: URL,
        cachedVersion: String,
        serverVersion: String
    ) async throws -> SyncResult {
        if cachedVersion == serverVersion {
            return .unchanged
        }

        if serverVersion.isEmpty {
            try? FileManager.default.removeItem(at: soundPath)
            return .cleared
        }

        let data = try await downloader.downloadWarningSound()
        try writeFileAtomic(soundPath, data: data)
        return .updated(version: serverVersion)
    }

    /// Writes `data` to a temp file in the destination directory and renames it
    /// over the target, so a crash mid-write never leaves a truncated MP3.
    static func writeFileAtomic(_ path: URL, data: Data) throws {
        let dir = path.deletingLastPathComponent()
        try FileManager.default.createDirectory(
            at: dir,
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
        let tmp = dir.appendingPathComponent(path.lastPathComponent + ".tmp-\(UUID().uuidString)")
        try data.write(to: tmp, options: .atomic)
        if FileManager.default.fileExists(atPath: path.path) {
            try? FileManager.default.removeItem(at: path)
        }
        do {
            try FileManager.default.moveItem(at: tmp, to: path)
        } catch {
            try? FileManager.default.removeItem(at: tmp)
            throw error
        }
    }
}
