import SQLite3
import XCTest
@testable import RootaikaMac

final class EventBufferTests: XCTestCase {
    private var dbPath: URL!

    override func setUp() {
        super.setUp()
        dbPath = FileManager.default.temporaryDirectory
            .appendingPathComponent("rootaika-buffer-test-\(UUID().uuidString)", isDirectory: true)
            .appendingPathComponent("events.db", isDirectory: false)
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: dbPath.deletingLastPathComponent())
        super.tearDown()
    }

    private func makeEvent(state: ActivityState = .active, process: String? = "game.app") -> Event {
        Event(eventID: UUID().uuidString, occurredAt: Date(), state: state, processName: process)
    }

    func testEnqueueAssignsMonotonicSequenceAndPersistsAcrossReopen() throws {
        do {
            let buffer = try EventBuffer(path: dbPath)
            let first = try buffer.enqueue(makeEvent())
            let second = try buffer.enqueue(makeEvent())
            XCTAssertEqual(first.sequence, 1)
            XCTAssertEqual(second.sequence, 2)
        }

        // Reopen: unsent events survive and the sequence counter continues.
        let reopened = try EventBuffer(path: dbPath)
        XCTAssertEqual(try reopened.countPending(), 2)
        let third = try reopened.enqueue(makeEvent())
        XCTAssertEqual(third.sequence, 3)
    }

    func testMarkSentRemovesFromPending() throws {
        let buffer = try EventBuffer(path: dbPath)
        let first = try buffer.enqueue(makeEvent())
        let second = try buffer.enqueue(makeEvent())

        try buffer.markSent([first.eventID])
        let pending = try buffer.pending(limit: 10)
        XCTAssertEqual(pending.map { $0.eventID }, [second.eventID])
        XCTAssertEqual(try buffer.countPending(), 1)
    }

    func testPendingIsOldestFirstAndHonorsLimit() throws {
        let buffer = try EventBuffer(path: dbPath)
        let events = try (0..<5).map { _ in try buffer.enqueue(makeEvent()) }
        let pending = try buffer.pending(limit: 3)
        XCTAssertEqual(pending.map { $0.sequence }, Array(events.prefix(3)).map { $0.sequence })
    }

    func testEnqueueClearsProcessNameForNonActiveStates() throws {
        let buffer = try EventBuffer(path: dbPath)
        try buffer.enqueue(makeEvent(state: .idle, process: "leaks-through.app"))
        let pending = try buffer.pending(limit: 1)
        XCTAssertNil(pending[0].processName)
        XCTAssertEqual(pending[0].state, .idle)
    }

    func testMarkSentPurgesOldSentEvents() throws {
        let buffer = try EventBuffer(path: dbPath)
        let old = try buffer.enqueue(makeEvent())
        try buffer.markSent([old.eventID])

        // Backdate the sent stamp past retention via a second connection.
        var handle: OpaquePointer?
        XCTAssertEqual(sqlite3_open(dbPath.path, &handle), SQLITE_OK)
        defer { sqlite3_close(handle) }
        let backdated = RootaikaJSON.rfc3339String(from: Date(timeIntervalSinceNow: -8 * 24 * 3600))
        XCTAssertEqual(sqlite3_exec(handle, "UPDATE events SET sent_at_utc = '\(backdated)'", nil, nil, nil), SQLITE_OK)

        let fresh = try buffer.enqueue(makeEvent())
        try buffer.markSent([fresh.eventID])

        var count: OpaquePointer?
        XCTAssertEqual(sqlite3_prepare_v2(handle, "SELECT COUNT(*) FROM events", -1, &count, nil), SQLITE_OK)
        defer { sqlite3_finalize(count) }
        XCTAssertEqual(sqlite3_step(count), SQLITE_ROW)
        // Only the freshly sent row remains.
        XCTAssertEqual(sqlite3_column_int64(count, 0), 1)
    }

    func testEnqueueFillsEventIDAndRejectsGarbage() throws {
        let buffer = try EventBuffer(path: dbPath)
        var blank = makeEvent()
        blank.eventID = ""
        let stored = try buffer.enqueue(blank)
        XCTAssertNotNil(UUID(uuidString: stored.eventID))

        var garbage = makeEvent()
        garbage.eventID = "not-a-uuid"
        XCTAssertThrowsError(try buffer.enqueue(garbage))
    }
}
