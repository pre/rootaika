package buffer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"rootaika/client-windows/internal/model"
)

func TestBufferEnqueuePendingAndMarkSent(t *testing.T) {
	ctx := context.Background()
	b, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	queued, err := b.Enqueue(ctx, model.Event{
		OccurredAt:  time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC),
		State:       model.StateActive,
		ProcessName: "steam.exe",
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if queued.EventID == "" {
		t.Fatalf("event id was not generated")
	}
	if queued.Type != model.EventTypeActivityObserved {
		t.Fatalf("unexpected event type: %s", queued.Type)
	}
	if queued.Sequence != 1 {
		t.Fatalf("unexpected sequence: %d", queued.Sequence)
	}

	pending, err := b.Pending(ctx, 10)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(pending))
	}
	if pending[0].ProcessName != "steam.exe" {
		t.Fatalf("process name was not preserved")
	}

	if err := b.MarkSent(ctx, []string{queued.EventID}); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	count, err := b.CountPending(ctx)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending count after MarkSent = %d, want 0", count)
	}

	next, err := b.Enqueue(ctx, model.Event{State: model.StateIdle})
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if next.Sequence != 2 {
		t.Fatalf("sequence did not persist, got %d", next.Sequence)
	}
	if next.ProcessName != "" {
		t.Fatalf("idle event should not keep process name")
	}
}

func TestMarkSentPurgesOldSentEvents(t *testing.T) {
	ctx := context.Background()
	b, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	old, err := b.Enqueue(ctx, model.Event{State: model.StateActive, ProcessName: "steam.exe"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := b.MarkSent(ctx, []string{old.EventID}); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	// Backdate the sent stamp past retention.
	backdated := formatTime(time.Now().UTC().Add(-sentRetention - time.Hour))
	if _, err := b.db.ExecContext(ctx, `UPDATE events SET sent_at_utc = ? WHERE event_id = ?`, backdated, old.EventID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	fresh, err := b.Enqueue(ctx, model.Event{State: model.StateIdle})
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if err := b.MarkSent(ctx, []string{fresh.EventID}); err != nil {
		t.Fatalf("second MarkSent: %v", err)
	}

	var total int
	if err := b.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 1 {
		t.Fatalf("events after purge = %d, want 1 (only the freshly sent row)", total)
	}
}

func TestBufferRejectsInvalidState(t *testing.T) {
	ctx := context.Background()
	b, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	if _, err := b.Enqueue(ctx, model.Event{State: model.ActivityState("bad")}); err == nil {
		t.Fatalf("expected invalid state error")
	}
}
