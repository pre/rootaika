import Foundation
import CryptoKit

/// OTA self-update, mirroring the Windows updater: the server's config names a
/// desired release (version tag / asset name / SHA256); the daemon downloads
/// the GitHub release asset, verifies the hash, renames it over the installed
/// binary (rename(2) is atomic; running processes keep the old inode), and
/// exits so launchd's KeepAlive restarts daemon and agent on the new version.
enum Updater {
    struct Plan: Equatable {
        let version: String
        let artifact: String
        let sha256: String

        var isComplete: Bool {
            !version.isEmpty && !artifact.isEmpty && !sha256.isEmpty
        }
    }

    enum UpdateError: Error, CustomStringConvertible {
        case incompletePlan
        case badURL
        case status(Int)
        case sha256Mismatch(got: String, want: String)
        case renameFailed(errno: Int32)

        var description: String {
            switch self {
            case .incompletePlan: return "incomplete update plan"
            case .badURL: return "cannot build download URL"
            case .status(let code): return "download failed with status \(code)"
            case .sha256Mismatch(let got, let want): return "sha256 mismatch: got \(got) want \(want)"
            case .renameFailed(let errno): return "rename failed: errno \(errno)"
            }
        }
    }

    /// Same policy as the Windows updater and the API client: retry network
    /// errors, 5xx and 429; 4xx and a hash mismatch are terminal.
    private static let maxAttempts = 4
    private static let baseBackoff: TimeInterval = 0.5
    private static let maxBackoff: TimeInterval = 5.0

    /// Plain string inequality (no semver ordering): any difference, including
    /// a downgrade, triggers an update. An incomplete plan never triggers.
    static func needsUpdate(current: String, plan: Plan) -> Bool {
        guard plan.isComplete else { return false }
        return plan.version != current
    }

    /// {base}/{owner}/{repo}/releases/download/{tag}/{asset}, owner/repo fixed
    /// at compile time.
    static func downloadURL(plan: Plan, base: String = "https://github.com") -> URL? {
        URL(string: "\(base)/\(Version.gitHubOwner)/\(Version.gitHubRepo)/releases/download/\(plan.version)/\(plan.artifact)")
    }

    static func sha256Hex(of data: Data) -> String {
        SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
    }

    /// Download the plan's release asset to `destination` and verify its
    /// SHA256. On any failure no usable file is left behind.
    static func download(plan: Plan, to destination: URL, base: String = "https://github.com") async throws {
        guard plan.isComplete else { throw UpdateError.incompletePlan }
        guard let url = downloadURL(plan: plan, base: base) else { throw UpdateError.badURL }

        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 300
        config.timeoutIntervalForResource = 300
        let session = URLSession(configuration: config)

        var lastError: Error = UpdateError.status(0)
        for attempt in 1...maxAttempts {
            if attempt > 1 {
                let delay = min(maxBackoff, baseBackoff * pow(2.0, Double(attempt - 2)))
                try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            }
            do {
                let (data, response) = try await session.data(from: url)
                guard let http = response as? HTTPURLResponse else {
                    lastError = UpdateError.status(0)
                    continue
                }
                if http.statusCode == 429 || (500...599).contains(http.statusCode) {
                    lastError = UpdateError.status(http.statusCode)
                    continue
                }
                guard (200...299).contains(http.statusCode) else {
                    throw UpdateError.status(http.statusCode) // terminal 4xx
                }
                let got = sha256Hex(of: data)
                let want = plan.sha256.trimmingCharacters(in: .whitespaces).lowercased()
                guard got == want else {
                    throw UpdateError.sha256Mismatch(got: got, want: want) // terminal
                }
                try data.write(to: destination, options: .atomic)
                try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: destination.path)
                return
            } catch let error as UpdateError {
                try? FileManager.default.removeItem(at: destination)
                throw error
            } catch is CancellationError {
                throw lastError
            } catch {
                lastError = error // transport error: retry
            }
        }
        try? FileManager.default.removeItem(at: destination)
        throw lastError
    }

    /// Atomically replace the live binary with the staged download.
    static func apply(staged: URL, target: URL) throws {
        let result = rename(staged.path, target.path)
        guard result == 0 else {
            let code = errno
            try? FileManager.default.removeItem(at: staged)
            throw UpdateError.renameFailed(errno: code)
        }
    }
}
