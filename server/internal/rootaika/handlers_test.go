package rootaika

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	store, err := OpenStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	app := NewApp(store)
	app.now = func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) }
	return app
}

func TestEventsBatchRequiresClientAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", bytes.NewBufferString(`{}`))
	recorder := httptest.NewRecorder()

	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestEventsBatchStoresEventsAndDeduplicates(t *testing.T) {
	app := testApp(t)
	payload := `{
	  "client_id": "2f0c70e7-70e5-4a1a-b9c4-5dd92dd9cf9b",
	  "events": [
	    {
	      "event_id": "4c516bf0-a6c1-4d80-a227-f8022c5c8f3c",
	      "type": "activity_observed",
	      "occurred_at": "2026-06-11T12:00:00Z",
	      "state": "active",
	      "process_name": "steam.exe",
	      "sequence": 1
	    }
	  ]
	}`

	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", bytes.NewBufferString(payload))
		request.SetBasicAuth("client", "client")
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("round %d status = %d body=%s", i, recorder.Code, recorder.Body.String())
		}
		var response map[string]int
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if i == 0 && response["accepted"] != 1 {
			t.Fatalf("first accepted = %d", response["accepted"])
		}
		if i == 1 && response["duplicate_or_ignored"] != 1 {
			t.Fatalf("second duplicate_or_ignored = %d", response["duplicate_or_ignored"])
		}
	}

	device, err := app.store.EnsureDevice(context.Background(), "2f0c70e7-70e5-4a1a-b9c4-5dd92dd9cf9b", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	events, err := app.store.ReportEvents(context.Background(), device.ID, app.now().Add(-time.Hour), app.now().Add(time.Hour))
	if err != nil {
		t.Fatalf("report events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d", len(events))
	}
}

func TestAdminLockReflectsInClientConfig(t *testing.T) {
	app := testApp(t)
	device, err := app.store.EnsureDevice(context.Background(), "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	lock := httptest.NewRequest(http.MethodPost, "/admin/devices/"+strconvInt(device.ID)+"/lock",
		strings.NewReader("message=Aika+lopettaa"))
	lock.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lock.SetBasicAuth("admin", "admin")
	lockRecorder := httptest.NewRecorder()
	app.ServeHTTP(lockRecorder, lock)
	if lockRecorder.Code != http.StatusSeeOther {
		t.Fatalf("lock status = %d", lockRecorder.Code)
	}

	if locked, message := clientConfigLock(t, app); !locked || message != "Aika lopettaa" {
		t.Fatalf("after lock: locked=%v message=%q", locked, message)
	}

	unlock := httptest.NewRequest(http.MethodPost, "/admin/devices/"+strconvInt(device.ID)+"/unlock", nil)
	unlock.SetBasicAuth("admin", "admin")
	unlockRecorder := httptest.NewRecorder()
	app.ServeHTTP(unlockRecorder, unlock)
	if unlockRecorder.Code != http.StatusSeeOther {
		t.Fatalf("unlock status = %d", unlockRecorder.Code)
	}

	if locked, message := clientConfigLock(t, app); locked || message != "" {
		t.Fatalf("after unlock: locked=%v message=%q", locked, message)
	}
}

func clientConfigLock(t *testing.T, app *App) (bool, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id=client-1", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Locked      bool   `json:"locked"`
		LockMessage string `json:"lock_message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return response.Locked, response.LockMessage
}

func strconvInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
