package rootaika

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBoardTodayRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/board/today", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestBoardTodayAllowsClientRole(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/board/today", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var board BoardToday
	if err := json.Unmarshal(recorder.Body.Bytes(), &board); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if board.RefreshSeconds != 60 {
		t.Fatalf("refresh_seconds = %d, want 60 (seeded default)", board.RefreshSeconds)
	}
	if board.Now == "" {
		t.Fatalf("now is empty")
	}
}

func TestBoardTodayComputesMinutesAndRefresh(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	// A configurable refresh interval set from admin must surface in the payload.
	if err := app.store.UpdateSettings(ctx, Settings{
		IdleThresholdSeconds:   60,
		UploadIntervalSeconds:  60,
		PollIntervalSeconds:    30,
		MaxCountableGapSeconds: 300,
		ChartYMaxMinutes:       720,
		BoardRefreshSeconds:    120,
	}, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// testApp now = 2026-06-11 12:00:00 UTC. An active observation 3 minutes
	// before now, with no following event, counts the 3-minute gap up to now
	// (below the 300s cap).
	active := app.now().Add(-3 * time.Minute)
	if _, _, err := app.store.InsertEvents(ctx, device.ID, []EventInput{
		{EventUUID: "e1", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: active, Sequence: 1},
	}, app.now()); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/board/today", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var board BoardToday
	if err := json.Unmarshal(recorder.Body.Bytes(), &board); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if board.RefreshSeconds != 120 {
		t.Fatalf("refresh_seconds = %d, want 120", board.RefreshSeconds)
	}
	if len(board.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(board.Devices))
	}
	if board.Devices[0].Name != device.DisplayName {
		t.Fatalf("name = %q, want %q", board.Devices[0].Name, device.DisplayName)
	}
	if board.Devices[0].Minutes != 3 {
		t.Fatalf("minutes = %d, want 3", board.Devices[0].Minutes)
	}
}

func TestBoardButtonRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/lock", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestBoardButtonLocksAllAssignedDevices(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	if err := app.store.CreateUser(ctx, "Pekka", app.now()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := app.store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	userID := users[0].ID

	// Two assigned devices the button should lock, plus one unassigned device it
	// must leave untouched.
	for _, uuid := range []string{"client-1", "client-2"} {
		device, err := app.store.EnsureDevice(ctx, uuid, app.now())
		if err != nil {
			t.Fatalf("ensure device %s: %v", uuid, err)
		}
		if err := app.store.UpdateDevice(ctx, device.ID, device.DisplayName, &userID); err != nil {
			t.Fatalf("assign device %s: %v", uuid, err)
		}
	}
	if _, err := app.store.EnsureDevice(ctx, "client-unassigned", app.now()); err != nil {
		t.Fatalf("ensure unassigned device: %v", err)
	}

	// First press locks all assigned devices with the button message.
	locked, affected := pressBoardButton(t, app)
	if !locked || affected != 2 {
		t.Fatalf("first press: locked=%v affected=%d, want true/2", locked, affected)
	}
	assertDeviceLock(t, app, "client-1", true, boardButtonMessage)
	assertDeviceLock(t, app, "client-2", true, boardButtonMessage)
	assertDeviceLock(t, app, "client-unassigned", false, "")

	// The button locks with a warning countdown so clients play the warning sound
	// before the screen locks, rather than locking instantly.
	if config, err := app.store.ClientConfig(ctx, "client-1", app.now()); err != nil {
		t.Fatalf("client config: %v", err)
	} else if config.WarningSeconds != boardButtonWarningSeconds {
		t.Fatalf("warning_seconds = %d, want %d", config.WarningSeconds, boardButtonWarningSeconds)
	}

	// A second press keeps everything locked: the button always means "lock"
	// (release is the separate /api/v1/unlock call), so it is idempotent.
	locked, affected = pressBoardButton(t, app)
	if !locked || affected != 2 {
		t.Fatalf("second press: locked=%v affected=%d, want true/2", locked, affected)
	}
	assertDeviceLock(t, app, "client-1", true, boardButtonMessage)
	assertDeviceLock(t, app, "client-2", true, boardButtonMessage)
}

func TestBoardUnlockRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/unlock", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestBoardUnlockReleasesAssignedDevices(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	if err := app.store.CreateUser(ctx, "Pekka", app.now()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := app.store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	userID := users[0].ID

	for _, uuid := range []string{"client-1", "client-2"} {
		device, err := app.store.EnsureDevice(ctx, uuid, app.now())
		if err != nil {
			t.Fatalf("ensure device %s: %v", uuid, err)
		}
		if err := app.store.UpdateDevice(ctx, device.ID, device.DisplayName, &userID); err != nil {
			t.Fatalf("assign device %s: %v", uuid, err)
		}
		if err := app.store.SetDeviceLock(ctx, device.ID, true, "Aika lopettaa", 30, app.now()); err != nil {
			t.Fatalf("lock device %s: %v", uuid, err)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/unlock", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unlock status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Locked   bool `json:"locked"`
		Affected int  `json:"affected"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode unlock response: %v", err)
	}
	if response.Locked || response.Affected != 2 {
		t.Fatalf("unlock: locked=%v affected=%d, want false/2", response.Locked, response.Affected)
	}

	assertDeviceLock(t, app, "client-1", false, "")
	assertDeviceLock(t, app, "client-2", false, "")
}

func pressBoardButton(t *testing.T, app *App) (bool, int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/lock", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("button status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Locked   bool `json:"locked"`
		Affected int  `json:"affected"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode button response: %v", err)
	}
	return response.Locked, response.Affected
}

func assertDeviceLock(t *testing.T, app *App, clientUUID string, wantLocked bool, wantMessage string) {
	t.Helper()
	config, err := app.store.ClientConfig(context.Background(), clientUUID, app.now())
	if err != nil {
		t.Fatalf("client config %s: %v", clientUUID, err)
	}
	if config.Locked != wantLocked || config.LockMessage != wantMessage {
		t.Fatalf("%s: locked=%v message=%q, want locked=%v message=%q",
			clientUUID, config.Locked, config.LockMessage, wantLocked, wantMessage)
	}
}

func TestLockStatusRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/lock", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestLockStatusReflectsGlobalStateAndTracksButton(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	if err := app.store.CreateUser(ctx, "Pekka", app.now()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := app.store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	userID := users[0].ID

	// Two assigned devices, plus one unassigned that must not affect the count.
	for _, uuid := range []string{"client-1", "client-2"} {
		device, err := app.store.EnsureDevice(ctx, uuid, app.now())
		if err != nil {
			t.Fatalf("ensure device %s: %v", uuid, err)
		}
		if err := app.store.UpdateDevice(ctx, device.ID, device.DisplayName, &userID); err != nil {
			t.Fatalf("assign device %s: %v", uuid, err)
		}
	}
	if _, err := app.store.EnsureDevice(ctx, "client-unassigned", app.now()); err != nil {
		t.Fatalf("ensure unassigned device: %v", err)
	}

	// Initially everything is unlocked.
	locked, lockedCount, totalCount := lockStatus(t, app)
	if locked || lockedCount != 0 || totalCount != 2 {
		t.Fatalf("initial status: locked=%v lockedCount=%d totalCount=%d, want false/0/2", locked, lockedCount, totalCount)
	}

	// After a button press the global state must read as locked.
	if pressed, _ := pressBoardButton(t, app); !pressed {
		t.Fatalf("expected press to lock")
	}
	locked, lockedCount, totalCount = lockStatus(t, app)
	if !locked || lockedCount != 2 || totalCount != 2 {
		t.Fatalf("locked status: locked=%v lockedCount=%d totalCount=%d, want true/2/2", locked, lockedCount, totalCount)
	}

	// The board unlock releases everything, and status returns to unlocked.
	request := httptest.NewRequest(http.MethodPost, "/api/v1/unlock", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unlock status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	locked, lockedCount, _ = lockStatus(t, app)
	if locked || lockedCount != 0 {
		t.Fatalf("unlocked status: locked=%v lockedCount=%d, want false/0", locked, lockedCount)
	}
}

// TestLockStatusLockedWhenAnyDeviceLocked verifies the global state is "locked"
// as soon as a single device is locked.
func TestLockStatusLockedWhenAnyDeviceLocked(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	if err := app.store.CreateUser(ctx, "Pekka", app.now()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, _ := app.store.Users(ctx)
	userID := users[0].ID

	var firstID int64
	for i, uuid := range []string{"client-1", "client-2"} {
		device, err := app.store.EnsureDevice(ctx, uuid, app.now())
		if err != nil {
			t.Fatalf("ensure device %s: %v", uuid, err)
		}
		if err := app.store.UpdateDevice(ctx, device.ID, device.DisplayName, &userID); err != nil {
			t.Fatalf("assign device %s: %v", uuid, err)
		}
		if i == 0 {
			firstID = device.ID
		}
	}
	if err := app.store.SetDeviceLock(ctx, firstID, true, "Aika lopettaa", 0, app.now()); err != nil {
		t.Fatalf("lock one device: %v", err)
	}

	locked, lockedCount, totalCount := lockStatus(t, app)
	if !locked || lockedCount != 1 || totalCount != 2 {
		t.Fatalf("status: locked=%v lockedCount=%d totalCount=%d, want true/1/2", locked, lockedCount, totalCount)
	}
}

func lockStatus(t *testing.T, app *App) (bool, int, int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/lock", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Locked      bool `json:"locked"`
		LockedCount int  `json:"locked_count"`
		TotalCount  int  `json:"total_count"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	return response.Locked, response.LockedCount, response.TotalCount
}

func TestSecondsToWholeMinutesRounds(t *testing.T) {
	cases := map[int64]int{0: 0, 29: 0, 30: 1, 89: 1, 90: 2, 180: 3}
	for seconds, want := range cases {
		if got := secondsToWholeMinutes(seconds); got != want {
			t.Fatalf("secondsToWholeMinutes(%d) = %d, want %d", seconds, got, want)
		}
	}
}
