import XCTest
@testable import RootaikaMac

final class UpdaterTests: XCTestCase {
    private func plan(version: String = "v1.2.3", artifact: String = "rootaika-mac", sha256: String = "abc") -> Updater.Plan {
        Updater.Plan(version: version, artifact: artifact, sha256: sha256)
    }

    func testNeedsUpdateOnAnyDifferenceIncludingDowngrade() {
        XCTAssertTrue(Updater.needsUpdate(current: "v1.0.0", plan: plan(version: "v1.2.3")))
        XCTAssertTrue(Updater.needsUpdate(current: "v2.0.0", plan: plan(version: "v1.2.3")))
        XCTAssertTrue(Updater.needsUpdate(current: "dev", plan: plan(version: "v1.2.3")))
        XCTAssertFalse(Updater.needsUpdate(current: "v1.2.3", plan: plan(version: "v1.2.3")))
    }

    func testIncompletePlanNeverTriggers() {
        XCTAssertFalse(Updater.needsUpdate(current: "dev", plan: plan(version: "")))
        XCTAssertFalse(Updater.needsUpdate(current: "dev", plan: plan(artifact: "")))
        XCTAssertFalse(Updater.needsUpdate(current: "dev", plan: plan(sha256: "")))
    }

    func testDownloadURLIsBuiltFromCompileTimeOwnerRepo() {
        let url = Updater.downloadURL(plan: plan())
        XCTAssertEqual(
            url?.absoluteString,
            "https://github.com/\(Version.gitHubOwner)/\(Version.gitHubRepo)/releases/download/v1.2.3/rootaika-mac"
        )
    }

    func testSha256HexMatchesKnownVector() {
        // sha256("abc"), a standard test vector.
        XCTAssertEqual(
            Updater.sha256Hex(of: Data("abc".utf8)),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        )
    }

    func testApplyRenamesStagedOverTarget() throws {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("rootaika-updater-test-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }

        let target = dir.appendingPathComponent("binary")
        let staged = dir.appendingPathComponent("binary.update")
        try Data("old".utf8).write(to: target)
        try Data("new".utf8).write(to: staged)

        try Updater.apply(staged: staged, target: target)
        XCTAssertEqual(try Data(contentsOf: target), Data("new".utf8))
        XCTAssertFalse(FileManager.default.fileExists(atPath: staged.path))
    }
}
