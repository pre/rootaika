import XCTest
@testable import RootaikaMac

/// A BoardClienting stub that only implements downloadWarningSound; the other
/// methods are unused by these tests and trap if called.
private final class StubDownloader: BoardClienting, @unchecked Sendable {
    var soundData: Data
    var error: Error?
    private(set) var downloadCount = 0

    init(soundData: Data = Data("ID3stub".utf8), error: Error? = nil) {
        self.soundData = soundData
        self.error = error
    }

    func postEvents(_ batch: EventBatch) async throws -> EventBatchResponse {
        fatalError("unused")
    }

    func fetchConfig(clientID: String, status: String?, knownVersion: String?, waitSeconds: Int) async throws -> ClientConfig {
        fatalError("unused")
    }

    func downloadWarningSound() async throws -> Data {
        downloadCount += 1
        if let error = error { throw error }
        return soundData
    }
}

private struct StubError: Error {}

final class WarningSoundTests: XCTestCase {
    private var dir: URL!

    override func setUpWithError() throws {
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("rootaika-test-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
    }

    private func soundPath() -> URL {
        dir.appendingPathComponent("warning-sound.mp3", isDirectory: false)
    }

    func testUnchangedWhenVersionsMatch() async throws {
        let dl = StubDownloader()
        let result = try await WarningSound.sync(
            downloader: dl, soundPath: soundPath(), cachedVersion: "v1", serverVersion: "v1")
        XCTAssertEqual(result, .unchanged)
        XCTAssertEqual(dl.downloadCount, 0)
    }

    func testDownloadsAndWritesWhenVersionDiffers() async throws {
        let dl = StubDownloader(soundData: Data("ID3 new sound".utf8))
        let path = soundPath()
        let result = try await WarningSound.sync(
            downloader: dl, soundPath: path, cachedVersion: "", serverVersion: "v2")
        XCTAssertEqual(result, .updated(version: "v2"))
        XCTAssertEqual(dl.downloadCount, 1)
        XCTAssertEqual(try Data(contentsOf: path), Data("ID3 new sound".utf8))
    }

    func testEmptyServerVersionClearsCache() async throws {
        let path = soundPath()
        try Data("old".utf8).write(to: path)
        let dl = StubDownloader()
        let result = try await WarningSound.sync(
            downloader: dl, soundPath: path, cachedVersion: "v1", serverVersion: "")
        XCTAssertEqual(result, .cleared)
        XCTAssertEqual(dl.downloadCount, 0)
        XCTAssertFalse(FileManager.default.fileExists(atPath: path.path))
    }

    func testDownloadErrorLeavesCacheUntouchedAndThrows() async throws {
        let path = soundPath()
        try Data("good".utf8).write(to: path)
        let dl = StubDownloader(error: StubError())
        do {
            _ = try await WarningSound.sync(
                downloader: dl, soundPath: path, cachedVersion: "v1", serverVersion: "v2")
            XCTFail("expected error")
        } catch {
            // Existing cached file must survive a transient download failure.
            XCTAssertEqual(try Data(contentsOf: path), Data("good".utf8))
        }
    }

    func testCachedPathEmptyWhenNoVersion() {
        var cfg = Config.makeDefault()
        cfg.warningSoundVersion = ""
        XCTAssertEqual(WarningSound.cachedPath(cfg), "")
    }
}
