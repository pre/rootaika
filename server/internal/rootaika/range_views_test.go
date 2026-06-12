package rootaika

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDailySpans(t *testing.T) {
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	days := dailySpans(now, location, 7)

	if len(days) != 7 {
		t.Fatalf("days len = %d", len(days))
	}

	last := days[len(days)-1]
	if !now.After(last.Start) || !now.Before(last.End) {
		t.Fatalf("now not contained in last span: %+v", last)
	}

	// Helsinki is UTC+3 in June, so local midnight is 21:00 the previous UTC day.
	localMidnight := time.Date(2026, 6, 11, 0, 0, 0, 0, location)
	if !last.Start.Equal(localMidnight.UTC()) {
		t.Fatalf("last start = %v want %v", last.Start, localMidnight.UTC())
	}
	if last.Label != "11.06" {
		t.Fatalf("last label = %q", last.Label)
	}

	// Each span is exactly 24h apart from the previous.
	for i := 1; i < len(days); i++ {
		if !days[i].Start.Equal(days[i-1].End) {
			t.Fatalf("span %d not contiguous", i)
		}
	}
}

func TestDailySpansClampsDayCount(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	days := dailySpans(now, location, 0)
	if len(days) != 1 {
		t.Fatalf("dayCount<1 should clamp to 1, got %d", len(days))
	}
}

func TestDailyUsageMultipleDaysWithCarryOver(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	days := dailySpans(now, location, 3)

	firstStart := days[0].Start
	secondStart := days[1].Start
	maxGap := 30 * time.Minute

	events := []ActivityEvent{
		// Carry-over before the first span: active right up to the boundary.
		{State: StateActive, ProcessName: "game.exe", OccurredAt: firstStart.Add(-10 * time.Minute), Sequence: 1},
		{State: StateIdle, OccurredAt: firstStart.Add(5 * time.Minute), Sequence: 2},
		// Activity on day 2.
		{State: StateActive, ProcessName: "browser.exe", OccurredAt: secondStart.Add(time.Hour), Sequence: 3},
		{State: StateIdle, OccurredAt: secondStart.Add(time.Hour + 20*time.Minute), Sequence: 4},
	}

	totals := DailyUsage(events, days, now, maxGap)
	if len(totals) != 3 {
		t.Fatalf("totals len = %d", len(totals))
	}
	if totals[0] != int64(5*60) {
		t.Fatalf("day 0 carry-over total = %d", totals[0])
	}
	if totals[1] != int64(20*60) {
		t.Fatalf("day 1 total = %d", totals[1])
	}
	if totals[2] != 0 {
		t.Fatalf("day 2 total = %d", totals[2])
	}
}

func TestHandleWeekAndMonth(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "week", path: "/week"},
		{name: "month", path: "/month"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := testApp(t)
			if _, err := app.store.EnsureDevice(context.Background(), "client-1", app.now()); err != nil {
				t.Fatalf("ensure device: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			request.SetBasicAuth("admin", "admin")
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("%s status = %d", tt.path, recorder.Code)
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "Yhteensä") {
				t.Fatalf("%s missing totals column", tt.path)
			}
			if !strings.Contains(body, "11.06") {
				t.Fatalf("%s missing today label", tt.path)
			}
		})
	}
}

func TestHandleWeekAllowsClientReadOnly(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/week", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("client week status = %d", recorder.Code)
	}
}

func TestHandleRangeRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/week", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated week status = %d", recorder.Code)
	}
}
