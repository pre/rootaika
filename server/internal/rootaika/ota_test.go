package rootaika

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// postAdminForm submits an urlencoded admin form POST and requires the
// PRG redirect the admin handlers answer with.
func postAdminForm(t *testing.T, app *App, path string, form url.Values) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("POST %s status = %d body=%s", path, recorder.Code, recorder.Body.String())
	}
}

func fetchClientConfig(t *testing.T, app *App, query string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?"+query, nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return payload
}

func TestClientConfigResolvesGlobalAndDeviceVersionSelection(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	// No versions registered: the triple is empty = no update offered.
	payload := fetchClientConfig(t, app, "client_id=client-1")
	if payload["desired_version"] != "" || payload["artifact_name"] != "" || payload["sha256"] != "" {
		t.Fatalf("expected empty OTA triple, got %v", payload)
	}

	postAdminForm(t, app, "/admin/versions", url.Values{
		"version": {"v1.2.0"}, "artifact": {"rootaika.exe"}, "sha256": {"abc123"},
	})
	postAdminForm(t, app, "/admin/versions", url.Values{
		"version": {"v1.3.0"}, "artifact": {"rootaika.exe"}, "sha256": {"def456"},
	})
	versions, err := app.store.Versions(ctx)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions = %v err = %v, want 2", versions, err)
	}

	// Global selection reaches a device with no override.
	settings, _ := app.store.Settings(ctx)
	settings.SelectedVersionID = int(versions[0].ID)
	if err := app.store.UpdateSettings(ctx, settings, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	payload = fetchClientConfig(t, app, "client_id=client-1")
	if payload["desired_version"] != "v1.2.0" || payload["artifact_name"] != "rootaika.exe" || payload["sha256"] != "abc123" {
		t.Fatalf("global selection not resolved: %v", payload)
	}

	// A per-device override wins over the global selection.
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	postAdminForm(t, app, "/admin/devices/"+strconv.FormatInt(device.ID, 10)+"/version", url.Values{
		"selected_version_id": {strconv.FormatInt(versions[1].ID, 10)},
	})
	payload = fetchClientConfig(t, app, "client_id=client-1")
	if payload["desired_version"] != "v1.3.0" || payload["sha256"] != "def456" {
		t.Fatalf("device override not resolved: %v", payload)
	}

	// Clearing the override (0) falls back to the global selection.
	postAdminForm(t, app, "/admin/devices/"+strconv.FormatInt(device.ID, 10)+"/version", url.Values{
		"selected_version_id": {"0"},
	})
	payload = fetchClientConfig(t, app, "client_id=client-1")
	if payload["desired_version"] != "v1.2.0" {
		t.Fatalf("clearing override did not restore global: %v", payload)
	}
}

func TestClientConfigRecordsReportedVersion(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	fetchClientConfig(t, app, "client_id=client-1&client_version=v1.1.0")

	devices, err := app.store.Devices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices = %v err = %v", devices, err)
	}
	if devices[0].LastClientVersion != "v1.1.0" {
		t.Fatalf("last_client_version = %q, want v1.1.0", devices[0].LastClientVersion)
	}
	if devices[0].LastClientVersionAt.IsZero() {
		t.Fatalf("last_client_version_at not recorded")
	}
}

func TestDeleteVersionResetsSelections(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	postAdminForm(t, app, "/admin/versions", url.Values{"version": {"v1.2.0"}})
	versions, _ := app.store.Versions(ctx)
	id := versions[0].ID

	settings, _ := app.store.Settings(ctx)
	settings.SelectedVersionID = int(id)
	if err := app.store.UpdateSettings(ctx, settings, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	device, _ := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err := app.store.SetDeviceVersion(ctx, device.ID, &id); err != nil {
		t.Fatalf("set device version: %v", err)
	}

	postAdminForm(t, app, "/admin/versions/"+strconv.FormatInt(id, 10)+"/delete", url.Values{})

	settings, _ = app.store.Settings(ctx)
	if settings.SelectedVersionID != 0 {
		t.Fatalf("global selection = %d, want 0 after delete", settings.SelectedVersionID)
	}
	devices, _ := app.store.Devices(ctx)
	if devices[0].SelectedVersionID != nil {
		t.Fatalf("device selection = %v, want nil after delete", *devices[0].SelectedVersionID)
	}
	payload := fetchClientConfig(t, app, "client_id=client-1")
	if payload["desired_version"] != "" {
		t.Fatalf("expected no update after delete, got %v", payload)
	}
}

func TestEditVersionUpdatesSelectionsInPlace(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	postAdminForm(t, app, "/admin/versions", url.Values{
		"version": {"v1.2.0"}, "artifact": {"rootaika.exe"}, "sha256": {"wrong"},
	})
	versions, _ := app.store.Versions(ctx)
	settings, _ := app.store.Settings(ctx)
	settings.SelectedVersionID = int(versions[0].ID)
	if err := app.store.UpdateSettings(ctx, settings, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	postAdminForm(t, app, "/admin/versions/"+strconv.FormatInt(versions[0].ID, 10)+"/edit", url.Values{
		"version": {"v1.2.0"}, "artifact": {"rootaika.exe"}, "sha256": {"corrected"},
	})

	payload := fetchClientConfig(t, app, "client_id=client-1")
	if payload["sha256"] != "corrected" {
		t.Fatalf("edit did not reach config: %v", payload)
	}
}

// TestConfigVersionChangesWhenSelectionChanges pins the OTA triple into the
// config fingerprint, so a long-polling client is woken the moment the admin
// selects a new version instead of at its next poll.
func TestConfigVersionChangesWhenSelectionChanges(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	before := fetchClientConfig(t, app, "client_id=client-1")["config_version"]

	postAdminForm(t, app, "/admin/versions", url.Values{"version": {"v1.2.0"}})
	versions, _ := app.store.Versions(ctx)
	settings, _ := app.store.Settings(ctx)
	settings.SelectedVersionID = int(versions[0].ID)
	if err := app.store.UpdateSettings(ctx, settings, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	after := fetchClientConfig(t, app, "client_id=client-1")["config_version"]
	if before == after {
		t.Fatalf("config_version unchanged (%v) after version selection", before)
	}
}

// TestSettingsPageRendersVersionSections executes the settings template with
// versions, selections and a reported client version present, so an
// execute-time template error (bad field, wrong type) fails here instead of in
// production.
func TestSettingsPageRendersVersionSections(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	postAdminForm(t, app, "/admin/versions", url.Values{
		"version": {"v1.2.0"}, "artifact": {"rootaika.exe"}, "sha256": {"abc123"},
	})
	versions, _ := app.store.Versions(ctx)
	settings, _ := app.store.Settings(ctx)
	settings.SelectedVersionID = int(versions[0].ID)
	if err := app.store.UpdateSettings(ctx, settings, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	fetchClientConfig(t, app, "client_id=client-1&client_version=v1.1.0")

	for _, user := range []string{"admin", "client"} {
		request := httptest.NewRequest(http.MethodGet, "/settings", nil)
		request.SetBasicAuth(user, user)
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("settings status as %s = %d body=%s", user, recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		for _, want := range []string{"Versiot", "v1.2.0", "v1.1.0", "→ v1.2.0", "Haluttu client-versio"} {
			if !strings.Contains(body, want) {
				t.Fatalf("settings page as %s missing %q", user, want)
			}
		}
	}
}

func TestVersionAdminActionsRequireAdminRole(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/versions", strings.NewReader("version=v1.2.0"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden && recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401/403", recorder.Code)
	}
}
