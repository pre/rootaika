package rootaika

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHumanSeconds(t *testing.T) {
	tests := []struct {
		name    string
		seconds int64
		want    string
	}{
		{name: "zero", seconds: 0, want: "0 min"},
		{name: "negative", seconds: -5, want: "0 min"},
		{name: "seconds only", seconds: 45, want: "45 s"},
		{name: "minutes only", seconds: 120, want: "2 min"},
		{name: "whole hours", seconds: 7200, want: "2 h"},
		{name: "hours and minutes", seconds: 3600 + 1800, want: "1 h 30 min"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanSeconds(tt.seconds); got != tt.want {
				t.Fatalf("humanSeconds(%d) = %q want %q", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestLockState(t *testing.T) {
	if got := lockState(true); got != "lukittu" {
		t.Fatalf("lockState(true) = %q want %q", got, "lukittu")
	}
	if got := lockState(false); got != "avattu" {
		t.Fatalf("lockState(false) = %q want %q", got, "avattu")
	}
}

func TestProcessViewsSortedDescThenName(t *testing.T) {
	byProcess := map[string]int64{
		"a.exe": 100,
		"b.exe": 100,
		"c.exe": 300,
	}
	views := processViews(byProcess)
	if len(views) != 3 {
		t.Fatalf("views len = %d", len(views))
	}
	if views[0].Name != "c.exe" || views[0].Seconds != 300 {
		t.Fatalf("top view = %+v", views[0])
	}
	// Equal seconds sorted by name ascending.
	if views[1].Name != "a.exe" || views[2].Name != "b.exe" {
		t.Fatalf("tie-break order wrong: %+v", views)
	}
}

func TestSelectedUser(t *testing.T) {
	id := int64(5)
	if !selectedUser(5, &id) {
		t.Fatalf("matching user should be selected")
	}
	other := int64(6)
	if selectedUser(5, &other) {
		t.Fatalf("non-matching user should not be selected")
	}
	if selectedUser(5, nil) {
		t.Fatalf("nil device user should not be selected")
	}
}

func TestFormatLocalAndPtr(t *testing.T) {
	if formatLocal(time.Time{}) != "-" {
		t.Fatalf("zero time should format to -")
	}
	if formatLocalPtr(nil) != "-" {
		t.Fatalf("nil pointer should format to -")
	}
	moment := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	got := formatLocalPtr(&moment)
	if got == "-" || !strings.HasPrefix(got, "2026-06-11") {
		t.Fatalf("formatLocalPtr = %q", got)
	}
}

func TestDashboardRendersWithDeviceAndDebugCheckbox(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	events := []EventInput{
		{EventUUID: "x", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: app.now().Add(-30 * time.Minute), Sequence: 1},
	}
	if _, _, err := app.store.InsertEvents(ctx, device.ID, events, app.now()); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, device.DisplayName) {
		t.Fatalf("dashboard missing device name")
	}
	if !strings.Contains(body, "steam.exe") {
		t.Fatalf("dashboard missing process data")
	}
	if !strings.Contains(body, `name="debug_mode"`) {
		t.Fatalf("dashboard missing debug_mode checkbox")
	}
	if !strings.Contains(body, `name="debug_unassigned_clients"`) {
		t.Fatalf("dashboard missing debug_unassigned_clients checkbox")
	}
	if !strings.Contains(body, `/admin/devices/`+strconvInt(device.ID)+`/delete`) {
		t.Fatalf("dashboard missing device delete form")
	}
	if !strings.Contains(body, `confirm('Poistetaanko laite ja sen tapahtumat pysyvästi?')`) {
		t.Fatalf("dashboard missing device delete confirmation")
	}
}

func TestDashboardClientIsReadOnly(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "read-only") {
		t.Fatalf("client dashboard should be read-only")
	}
}
