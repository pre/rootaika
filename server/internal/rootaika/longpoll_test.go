package rootaika

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fetchConfigVersion performs a non-blocking config poll and returns the
// reported config_version plus the lock state, so tests can capture the
// baseline a long poll will compare against.
func fetchConfigVersion(t *testing.T, app *App, clientID string) (string, bool) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id="+clientID, nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ConfigVersion string `json:"config_version"`
		Locked        bool   `json:"locked"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if response.ConfigVersion == "" {
		t.Fatalf("config_version missing in response: %s", recorder.Body.String())
	}
	return response.ConfigVersion, response.Locked
}

func TestLongPollReturnsImmediatelyWhenVersionStale(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	// A stale (non-matching) version must return at once, never block, even with
	// a large wait budget.
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/client/config?client_id=client-1&wait=30&config_version=staleversion", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("long poll blocked despite stale version")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	_ = device
}

func TestLongPollWakesOnConfigChange(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	if _, err := app.store.EnsureDevice(ctx, "client-1", app.now()); err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	version, locked := fetchConfigVersion(t, app, "client-1")
	if locked {
		t.Fatalf("device should start unlocked")
	}

	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/client/config?client_id=client-1&wait=30&config_version="+version, nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.ServeHTTP(recorder, request)
		close(done)
	}()

	// The poll must still be blocked: nothing has changed yet.
	select {
	case <-done:
		t.Fatalf("long poll returned before any config change")
	case <-time.After(150 * time.Millisecond):
	}

	// Lock the device via the store and fire the notifier, simulating an admin
	// action. The blocked poll must wake and return the new locked config.
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	if err := app.store.SetDeviceLock(ctx, device.ID, true, "stop", 0, app.now()); err != nil {
		t.Fatalf("set lock: %v", err)
	}
	app.notifier.notify()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("long poll did not wake on config change")
	}

	var response struct {
		Locked        bool   `json:"locked"`
		ConfigVersion string `json:"config_version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.Locked {
		t.Fatalf("expected locked config after wake, got %s", recorder.Body.String())
	}
	if response.ConfigVersion == version {
		t.Fatalf("config_version should change after lock")
	}
}

func TestLongPollReturnsOnTimeoutUnchanged(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	if _, err := app.store.EnsureDevice(ctx, "client-1", app.now()); err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	version, _ := fetchConfigVersion(t, app, "client-1")

	// wait=1 with a matching version: no change arrives, so it must return after
	// roughly the wait budget with the same version.
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/client/config?client_id=client-1&wait=1&config_version="+version, nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()

	start := time.Now()
	app.ServeHTTP(recorder, request)
	elapsed := time.Since(start)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("returned too early (%v); long poll should hold ~1s", elapsed)
	}
	var response struct {
		ConfigVersion string `json:"config_version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.ConfigVersion != version {
		t.Fatalf("version changed on timeout: %q != %q", response.ConfigVersion, version)
	}
}

func TestLongPollReturnsWhenClientDisconnects(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	if _, err := app.store.EnsureDevice(ctx, "client-1", app.now()); err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	version, _ := fetchConfigVersion(t, app, "client-1")

	reqCtx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/client/config?client_id=client-1&wait=30&config_version="+version, nil)
	request = request.WithContext(reqCtx)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		app.ServeHTTP(recorder, request)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("long poll did not return after client disconnect")
	}
}
