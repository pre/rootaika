import Foundation
import SQLite3

/// Persistent event buffer backed by SQLite (the system libsqlite3, no
/// dependency). Mirrors the Windows client's internal/buffer package: the same
/// schema, a monotonic per-client sequence stored in a metadata row, and
/// mark-sent-after-upload semantics so a crash or restart never loses unsent
/// events.
///
/// Thread-safe: a mutex serializes all access, since the daemon enqueues from
/// the agent-HTTP handler thread while the upload loop drains concurrently.
final class EventBuffer {
    enum BufferError: Error, CustomStringConvertible {
        case sqlite(String)
        case invalidEvent(String)

        var description: String {
            switch self {
            case .sqlite(let message): return "sqlite: \(message)"
            case .invalidEvent(let message): return "invalid event: \(message)"
            }
        }
    }

    private let db: OpaquePointer
    private let mutex = NSLock()
    // sqlite3_bind_text destructor telling SQLite to copy the buffer.
    private static let transient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    init(path: URL) throws {
        try FileManager.default.createDirectory(
            at: path.deletingLastPathComponent(),
            withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700]
        )
        var handle: OpaquePointer?
        let rc = sqlite3_open(path.path, &handle)
        guard rc == SQLITE_OK, let opened = handle else {
            let message = handle.map { String(cString: sqlite3_errmsg($0)) } ?? "open failed (\(rc))"
            if let handle = handle { sqlite3_close(handle) }
            throw BufferError.sqlite(message)
        }
        db = opened
        do {
            try exec("""
                CREATE TABLE IF NOT EXISTS events (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    event_id TEXT NOT NULL UNIQUE,
                    type TEXT NOT NULL,
                    occurred_at_utc TEXT NOT NULL,
                    state TEXT NOT NULL,
                    process_name TEXT NOT NULL DEFAULT '',
                    sequence INTEGER NOT NULL,
                    created_at_utc TEXT NOT NULL,
                    sent_at_utc TEXT
                );
                CREATE INDEX IF NOT EXISTS idx_events_unsent ON events(sent_at_utc, id);
                CREATE TABLE IF NOT EXISTS metadata (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL
                );
                """)
        } catch {
            sqlite3_close(opened)
            throw error
        }
    }

    deinit {
        sqlite3_close(db)
    }

    /// Normalize and insert an event, assigning the next persistent sequence.
    /// Returns the stored event (with event_id and sequence filled in).
    @discardableResult
    func enqueue(_ event: Event) throws -> Event {
        mutex.lock()
        defer { mutex.unlock() }
        var stored = event
        if stored.eventID.isEmpty {
            stored.eventID = UUID().uuidString
        } else if UUID(uuidString: stored.eventID) == nil {
            throw BufferError.invalidEvent("invalid event_id \(stored.eventID)")
        }
        if stored.type.isEmpty {
            stored.type = Event.typeActivityObserved
        }
        guard stored.type == Event.typeActivityObserved else {
            throw BufferError.invalidEvent("unsupported event type \(stored.type)")
        }
        if stored.state != .active {
            stored.processName = nil
        }

        try exec("BEGIN IMMEDIATE")
        do {
            stored.sequence = try nextSequence()
            let insert = try prepare("""
                INSERT INTO events (event_id, type, occurred_at_utc, state, process_name, sequence, created_at_utc)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """)
            defer { sqlite3_finalize(insert) }
            bindText(insert, 1, stored.eventID)
            bindText(insert, 2, stored.type)
            bindText(insert, 3, RootaikaJSON.rfc3339String(from: stored.occurredAt))
            bindText(insert, 4, stored.state.rawValue)
            bindText(insert, 5, stored.processName ?? "")
            sqlite3_bind_int64(insert, 6, stored.sequence)
            bindText(insert, 7, RootaikaJSON.rfc3339String(from: Date()))
            try step(insert)
            try exec("COMMIT")
        } catch {
            try? exec("ROLLBACK")
            throw error
        }
        return stored
    }

    /// Oldest-first unsent events, at most `limit`.
    func pending(limit: Int) throws -> [Event] {
        mutex.lock()
        defer { mutex.unlock() }
        let effectiveLimit = limit > 0 ? limit : 100
        let query = try prepare("""
            SELECT event_id, type, occurred_at_utc, state, process_name, sequence
            FROM events
            WHERE sent_at_utc IS NULL
            ORDER BY id
            LIMIT ?
            """)
        defer { sqlite3_finalize(query) }
        sqlite3_bind_int(query, 1, Int32(effectiveLimit))

        var events: [Event] = []
        while sqlite3_step(query) == SQLITE_ROW {
            let occurredAtRaw = column(query, 2)
            guard let occurredAt = RootaikaJSON.rfc3339Date(from: occurredAtRaw) else {
                throw BufferError.sqlite("invalid occurred_at_utc \(occurredAtRaw)")
            }
            guard let state = ActivityState(rawValue: column(query, 3)) else {
                throw BufferError.sqlite("invalid state \(column(query, 3))")
            }
            let processName = column(query, 4)
            events.append(Event(
                eventID: column(query, 0),
                type: column(query, 1),
                occurredAt: occurredAt,
                state: state,
                processName: processName.isEmpty ? nil : processName,
                sequence: sqlite3_column_int64(query, 5)
            ))
        }
        return events
    }

    /// Stamp the given events as uploaded; they no longer appear in pending().
    func markSent(_ eventIDs: [String]) throws {
        guard !eventIDs.isEmpty else { return }
        mutex.lock()
        defer { mutex.unlock() }
        try exec("BEGIN IMMEDIATE")
        do {
            let update = try prepare("UPDATE events SET sent_at_utc = ? WHERE event_id = ?")
            defer { sqlite3_finalize(update) }
            let sentAt = RootaikaJSON.rfc3339String(from: Date())
            for id in eventIDs where !id.isEmpty {
                sqlite3_reset(update)
                bindText(update, 1, sentAt)
                bindText(update, 2, id)
                try step(update)
            }
            try exec("COMMIT")
        } catch {
            try? exec("ROLLBACK")
            throw error
        }
    }

    func countPending() throws -> Int {
        mutex.lock()
        defer { mutex.unlock() }
        let query = try prepare("SELECT COUNT(*) FROM events WHERE sent_at_utc IS NULL")
        defer { sqlite3_finalize(query) }
        guard sqlite3_step(query) == SQLITE_ROW else {
            throw BufferError.sqlite(String(cString: sqlite3_errmsg(db)))
        }
        return Int(sqlite3_column_int64(query, 0))
    }

    // MARK: SQLite plumbing

    /// Read-increment-write the persistent sequence counter. Must run inside
    /// the caller's transaction so concurrent enqueues cannot double-assign.
    private func nextSequence() throws -> Int64 {
        var current: Int64 = 0
        let query = try prepare("SELECT value FROM metadata WHERE key = 'next_sequence'")
        if sqlite3_step(query) == SQLITE_ROW {
            guard let parsed = Int64(column(query, 0)) else {
                sqlite3_finalize(query)
                throw BufferError.sqlite("corrupt next_sequence value")
            }
            current = parsed
        }
        sqlite3_finalize(query)

        let next = current + 1
        let upsert = try prepare("""
            INSERT INTO metadata(key, value) VALUES('next_sequence', ?)
            ON CONFLICT(key) DO UPDATE SET value = excluded.value
            """)
        defer { sqlite3_finalize(upsert) }
        bindText(upsert, 1, String(next))
        try step(upsert)
        return next
    }

    private func exec(_ sql: String) throws {
        var errorMessage: UnsafeMutablePointer<CChar>?
        guard sqlite3_exec(db, sql, nil, nil, &errorMessage) == SQLITE_OK else {
            let message = errorMessage.map { String(cString: $0) } ?? "unknown error"
            sqlite3_free(errorMessage)
            throw BufferError.sqlite(message)
        }
    }

    private func prepare(_ sql: String) throws -> OpaquePointer {
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK, let prepared = statement else {
            throw BufferError.sqlite(String(cString: sqlite3_errmsg(db)))
        }
        return prepared
    }

    private func step(_ statement: OpaquePointer) throws {
        guard sqlite3_step(statement) == SQLITE_DONE else {
            throw BufferError.sqlite(String(cString: sqlite3_errmsg(db)))
        }
    }

    private func bindText(_ statement: OpaquePointer, _ index: Int32, _ value: String) {
        sqlite3_bind_text(statement, index, value, -1, EventBuffer.transient)
    }

    private func column(_ statement: OpaquePointer, _ index: Int32) -> String {
        guard let text = sqlite3_column_text(statement, index) else { return "" }
        return String(cString: text)
    }
}
