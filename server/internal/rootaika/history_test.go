package rootaika

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLockTransitionsDerivesFromEventStream(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	base := fixedNow()
	events := []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: base, Sequence: 1},
		{EventUUID: "b", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: base.Add(1 * time.Minute), Sequence: 2},
		{EventUUID: "c", Type: EventTypeActivityObserved, State: StateLocked, OccurredAt: base.Add(2 * time.Minute), Sequence: 3},
		{EventUUID: "d", Type: EventTypeActivityObserved, State: StateLocked, OccurredAt: base.Add(3 * time.Minute), Sequence: 4},
		{EventUUID: "e", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: base.Add(4 * time.Minute), Sequence: 5},
	}
	if _, _, err := store.InsertEvents(ctx, device.ID, events, fixedNow()); err != nil {
		t.Fatalf("insert: %v", err)
	}

	transitions, err := store.LockTransitions(ctx, 100)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	// Expect exactly two transitions: lock at +2m, unlock at +4m. The runs of
	// repeated active/locked states must not produce extra rows.
	if len(transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d: %+v", len(transitions), transitions)
	}
	// Newest first: unlock then lock.
	if transitions[0].Locked || !transitions[0].OccurredAt.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("unexpected first transition: %+v", transitions[0])
	}
	if !transitions[1].Locked || !transitions[1].OccurredAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("unexpected second transition: %+v", transitions[1])
	}
	if transitions[0].DeviceName != device.DisplayName {
		t.Fatalf("device name = %q, want %q", transitions[0].DeviceName, device.DisplayName)
	}
}

func TestLockTransitionsFirstEventLockedIsLock(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if _, _, err := store.InsertEvents(ctx, device.ID, []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateLocked, OccurredAt: fixedNow(), Sequence: 1},
	}, fixedNow()); err != nil {
		t.Fatalf("insert: %v", err)
	}

	transitions, err := store.LockTransitions(ctx, 100)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	if len(transitions) != 1 || !transitions[0].Locked {
		t.Fatalf("expected single lock transition, got %+v", transitions)
	}
}

func TestSetDeviceLockRecordsAudit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if err := store.SetDeviceLock(ctx, device.ID, true, "syö läksyt", 60, fixedNow()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := store.SetDeviceLock(ctx, device.ID, false, "", 0, fixedNow().Add(time.Minute)); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	entries, err := store.LockAuditEntries(ctx, 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}
	// Newest first: the unlock.
	if entries[0].Locked || entries[0].Source != LockSourceAdmin {
		t.Fatalf("unexpected newest entry: %+v", entries[0])
	}
	if entries[0].DeviceID == nil || *entries[0].DeviceID != device.ID {
		t.Fatalf("device id not recorded: %+v", entries[0])
	}
	if !entries[1].Locked {
		t.Fatalf("expected oldest entry to be a lock: %+v", entries[1])
	}
}

func TestHistoryPageRequiresAuthAndRenders(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := app.store.SetDeviceLock(ctx, device.ID, true, "läksyt", 0, app.now()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, _, err := app.store.InsertEvents(ctx, device.ID, []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateLocked, OccurredAt: app.now(), Sequence: 1},
	}, app.now()); err != nil {
		t.Fatalf("insert: %v", err)
	}

	unauth := httptest.NewRequest(http.MethodGet, "/history", nil)
	unauthRec := httptest.NewRecorder()
	app.ServeHTTP(unauthRec, unauth)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", unauthRec.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/history", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Historia") {
		t.Fatalf("missing title in body")
	}
	if !strings.Contains(body, "Lukittu") {
		t.Fatalf("expected a lock row in body")
	}
}

func TestBoardLockActionsRecordAudit(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := store.UpdateDevice(ctx, device.ID, "Pelikone", nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	userID := int64(0)
	if err := store.CreateUser(ctx, "Liisa", fixedNow()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, _ := store.Users(ctx)
	userID = users[0].ID
	if err := store.UpdateDevice(ctx, device.ID, "Pelikone", &userID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if _, _, err := store.ToggleAllLocks(ctx, "kaikki kiinni", fixedNow()); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if _, err := store.UnlockAllLocks(ctx, fixedNow().Add(time.Minute)); err != nil {
		t.Fatalf("unlock all: %v", err)
	}

	entries, err := store.LockAuditEntries(ctx, 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Source != LockSourceBoardUnlock || entries[0].Locked {
		t.Fatalf("unexpected unlock entry: %+v", entries[0])
	}
	if entries[1].Source != LockSourceBoardButton || !entries[1].Locked {
		t.Fatalf("unexpected toggle entry: %+v", entries[1])
	}
	// Board actions are device-wide, so no single device is named.
	if entries[0].DeviceID != nil || entries[1].DeviceID != nil {
		t.Fatalf("board actions should not name a device: %+v", entries)
	}
	if entries[1].Affected != 1 {
		t.Fatalf("toggle affected = %d, want 1", entries[1].Affected)
	}
}
