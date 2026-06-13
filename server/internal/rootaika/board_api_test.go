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

func TestSecondsToWholeMinutesRounds(t *testing.T) {
	cases := map[int64]int{0: 0, 29: 0, 30: 1, 89: 1, 90: 2, 180: 3}
	for seconds, want := range cases {
		if got := secondsToWholeMinutes(seconds); got != want {
			t.Fatalf("secondsToWholeMinutes(%d) = %d, want %d", seconds, got, want)
		}
	}
}
