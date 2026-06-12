package buffer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"rootaika/client-windows/internal/model"
)

type Buffer struct {
	db *sql.DB
}

func Open(path string) (*Buffer, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	b := &Buffer{db: db}
	if err := b.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return b, nil
}

func (b *Buffer) Close() error {
	return b.db.Close()
}

func (b *Buffer) Enqueue(ctx context.Context, event model.Event) (model.Event, error) {
	event, err := normalizeEvent(event)
	if err != nil {
		return model.Event{}, err
	}

	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Event{}, err
	}
	defer rollbackUnlessCommitted(tx)

	sequence, err := nextSequence(ctx, tx)
	if err != nil {
		return model.Event{}, err
	}
	event.Sequence = sequence

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (event_id, type, occurred_at_utc, state, process_name, sequence, created_at_utc)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.EventID, event.Type, formatTime(event.OccurredAt), event.State, event.ProcessName, event.Sequence, formatTime(time.Now().UTC()))
	if err != nil {
		return model.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Event{}, err
	}
	return event, nil
}

func (b *Buffer) Pending(ctx context.Context, limit int) ([]model.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := b.db.QueryContext(ctx, `
		SELECT event_id, type, occurred_at_utc, state, process_name, sequence
		FROM events
		WHERE sent_at_utc IS NULL
		ORDER BY id
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var event model.Event
		var occurredAt string
		if err := rows.Scan(&event.EventID, &event.Type, &occurredAt, &event.State, &event.ProcessName, &event.Sequence); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, err
		}
		event.OccurredAt = t
		events = append(events, event)
	}
	return events, rows.Err()
}

func (b *Buffer) MarkSent(ctx context.Context, eventIDs []string) error {
	if len(eventIDs) == 0 {
		return nil
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	stmt, err := tx.PrepareContext(ctx, `UPDATE events SET sent_at_utc = ? WHERE event_id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	sentAt := formatTime(time.Now().UTC())
	for _, id := range eventIDs {
		if id == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, sentAt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (b *Buffer) CountPending(ctx context.Context) (int, error) {
	var count int
	err := b.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE sent_at_utc IS NULL`).Scan(&count)
	return count, err
}

func (b *Buffer) migrate(ctx context.Context) error {
	_, err := b.db.ExecContext(ctx, `
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
	`)
	return err
}

func normalizeEvent(event model.Event) (model.Event, error) {
	if event.EventID == "" {
		event.EventID = uuid.NewString()
	} else if _, err := uuid.Parse(event.EventID); err != nil {
		return model.Event{}, fmt.Errorf("invalid event_id %q: %w", event.EventID, err)
	}
	if event.Type == "" {
		event.Type = model.EventTypeActivityObserved
	}
	if event.Type != model.EventTypeActivityObserved {
		return model.Event{}, fmt.Errorf("unsupported event type %q", event.Type)
	}
	if !event.State.Valid() {
		return model.Event{}, fmt.Errorf("invalid activity state %q", event.State)
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if event.State != model.StateActive {
		event.ProcessName = ""
	}
	return event, nil
}

func nextSequence(ctx context.Context, tx *sql.Tx) (int64, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = 'next_sequence'`).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	current := int64(0)
	if err == nil {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil {
			return 0, parseErr
		}
		current = parsed
	}
	next := current + 1
	_, err = tx.ExecContext(ctx, `
		INSERT INTO metadata(key, value) VALUES('next_sequence', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, strconv.FormatInt(next, 10))
	if err != nil {
		return 0, err
	}
	return next, nil
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	_ = tx.Rollback()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
