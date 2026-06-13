package rootaika

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateBatch(t *testing.T) {
	occurred := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	valid := batchEventRequest{EventID: "e1", Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred, ProcessName: "steam.exe"}

	tests := []struct {
		name    string
		request batchRequest
		wantErr bool
	}{
		{
			name:    "empty client id",
			request: batchRequest{ClientID: "  ", Events: []batchEventRequest{valid}},
			wantErr: true,
		},
		{
			name:    "empty events",
			request: batchRequest{ClientID: "c1"},
			wantErr: true,
		},
		{
			name:    "missing event id",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{{Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred}}},
			wantErr: true,
		},
		{
			name: "duplicate event id",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{
				{EventID: "dup", Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred, ProcessName: "a"},
				{EventID: "dup", Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred, ProcessName: "b"},
			}},
			wantErr: true,
		},
		{
			name:    "bad type",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{{EventID: "e1", Type: "nope", State: StateActive, OccurredAt: occurred}}},
			wantErr: true,
		},
		{
			name:    "bad state",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{{EventID: "e1", Type: EventTypeActivityObserved, State: "weird", OccurredAt: occurred}}},
			wantErr: true,
		},
		{
			name:    "missing occurred_at",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{{EventID: "e1", Type: EventTypeActivityObserved, State: StateActive}}},
			wantErr: true,
		},
		{
			name:    "valid",
			request: batchRequest{ClientID: "c1", Events: []batchEventRequest{valid}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateBatch(tt.request)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateBatchActiveWithoutProcessDefaultsToUnknown(t *testing.T) {
	occurred := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	request := batchRequest{ClientID: "c1", Events: []batchEventRequest{
		{EventID: "e1", Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred},
	}}
	events, err := validateBatch(request)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if events[0].ProcessName != "unknown" {
		t.Fatalf("process = %q want unknown", events[0].ProcessName)
	}
}

func TestValidateBatchTooManyEvents(t *testing.T) {
	occurred := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	events := make([]batchEventRequest, 10001)
	for i := range events {
		events[i] = batchEventRequest{EventID: strconvInt(int64(i)), Type: EventTypeActivityObserved, State: StateActive, OccurredAt: occurred, ProcessName: "x"}
	}
	if _, err := validateBatch(batchRequest{ClientID: "c1", Events: events}); err == nil {
		t.Fatalf("expected too-many-events error")
	}
}

func TestHandleClientConfigReturnsDebugMode(t *testing.T) {
	app := testApp(t)
	if err := app.store.UpdateSettings(context.Background(), Settings{
		IdleThresholdSeconds:   60,
		UploadIntervalSeconds:  60,
		PollIntervalSeconds:    30,
		MaxCountableGapSeconds: 300,
		DebugMode:              true,
	}, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id=client-1", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if response["debug_mode"] != true {
		t.Fatalf("debug_mode = %v want true", response["debug_mode"])
	}
	if response["max_countable_gap_seconds"] != float64(300) {
		t.Fatalf("max gap = %v", response["max_countable_gap_seconds"])
	}
}

func TestHandleClientConfigDebugsUnassignedClient(t *testing.T) {
	app := testApp(t)
	if err := app.store.UpdateSettings(context.Background(), Settings{
		IdleThresholdSeconds:   60,
		UploadIntervalSeconds:  60,
		PollIntervalSeconds:    30,
		MaxCountableGapSeconds: 300,
		DebugUnassignedClients: true,
	}, app.now()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/config?client_id=client-1", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("config status = %d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if response["debug_mode"] != true {
		t.Fatalf("debug_mode = %v want true", response["debug_mode"])
	}
}

func TestHandleClientCommandsEmpty(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/commands?client_id=client-1", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("commands status = %d", recorder.Code)
	}
}

func TestHandleCommandAckUnknownCommand(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/client/commands/424242/ack?client_id=client-1", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("ack unknown status = %d", recorder.Code)
	}
}

func TestHandleCommandAckReadsClientIDFromBody(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	id, err := app.store.CreateCommand(ctx, device.ID, CommandLock, "", app.now())
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/client/commands/"+strconvInt(id)+"/ack", strings.NewReader(`{"client_id":"client-1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ack via body status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleEventsBatchRejectsInvalidJSON(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", strings.NewReader(`{not json`))
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid json status = %d", recorder.Code)
	}
}

func TestHandleEventsBatchRejectsInvalidPayload(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/events/batch", strings.NewReader(`{"client_id":"","events":[]}`))
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid payload status = %d", recorder.Code)
	}
}

func TestHandleCommandAckBadPath(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/client/commands/not-a-number/ack", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("ack bad path status = %d", recorder.Code)
	}
}

func postForm(t *testing.T, app *App, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	return recorder
}

func TestHandleAdminRouting(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	// users
	if rec := postForm(t, app, "/admin/users", url.Values{"name": {"Alice"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create user status = %d", rec.Code)
	}
	users, _ := app.store.Users(ctx)
	if len(users) != 1 {
		t.Fatalf("user not created")
	}
	userID := strconvInt(users[0].ID)

	// assign
	if rec := postForm(t, app, "/admin/devices/"+strconvInt(device.ID)+"/assign", url.Values{
		"display_name": {"Box"}, "user_id": {userID},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("assign status = %d", rec.Code)
	}

	// lock + unlock
	if rec := postForm(t, app, "/admin/devices/"+strconvInt(device.ID)+"/lock", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("lock status = %d", rec.Code)
	}
	if rec := postForm(t, app, "/admin/devices/"+strconvInt(device.ID)+"/unlock", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("unlock status = %d", rec.Code)
	}
	if rec := postForm(t, app, "/admin/devices/"+strconvInt(device.ID)+"/delete", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete device status = %d", rec.Code)
	}
	devices, _ := app.store.Devices(ctx)
	if len(devices) != 0 {
		t.Fatalf("device not deleted: %+v", devices)
	}

	// settings
	if rec := postForm(t, app, "/admin/settings", url.Values{
		"idle_threshold_seconds":    {"60"},
		"upload_interval_seconds":   {"60"},
		"poll_interval_seconds":     {"30"},
		"max_countable_gap_seconds": {"300"},
		"debug_mode":                {"on"},
		"debug_unassigned_clients":  {"on"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("settings status = %d", rec.Code)
	}

	// categories create + delete
	if rec := postForm(t, app, "/admin/categories", url.Values{
		"match_type": {"exact"}, "pattern": {"steam.exe"}, "category": {"pelit"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("create category status = %d", rec.Code)
	}
	categories, _ := app.store.Categories(ctx)
	if len(categories) != 1 {
		t.Fatalf("category not created")
	}
	if rec := postForm(t, app, "/admin/categories/"+strconvInt(categories[0].ID)+"/delete", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete category status = %d", rec.Code)
	}
}

func TestHandleAdminRequiresAdminRole(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader("name=Bob"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("client admin status = %d", recorder.Code)
	}
}

func TestHandleAdminInvalidIDsAndBadForm(t *testing.T) {
	app := testApp(t)
	if rec := postForm(t, app, "/admin/devices/notanumber/lock", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad device id status = %d", rec.Code)
	}
	if rec := postForm(t, app, "/admin/devices/notanumber/delete", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad delete device id status = %d", rec.Code)
	}
	if rec := postForm(t, app, "/admin/categories/notanumber/delete", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad category id status = %d", rec.Code)
	}
	if rec := postForm(t, app, "/admin/settings", url.Values{"idle_threshold_seconds": {"0"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d", rec.Code)
	}
}

func TestHandleAdminUnknownRoute(t *testing.T) {
	app := testApp(t)
	if rec := postForm(t, app, "/admin/unknown", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown admin route status = %d", rec.Code)
	}
}

func TestSettingsFromForm(t *testing.T) {
	form := url.Values{
		"idle_threshold_seconds":    {"90"},
		"upload_interval_seconds":   {"120"},
		"poll_interval_seconds":     {"45"},
		"max_countable_gap_seconds": {"600"},
		"debug_mode":                {"on"},
		"debug_unassigned_clients":  {"on"},
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := request.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	settings, err := settingsFromForm(request)
	if err != nil {
		t.Fatalf("settings from form: %v", err)
	}
	want := Settings{
		IdleThresholdSeconds:   90,
		UploadIntervalSeconds:  120,
		PollIntervalSeconds:    45,
		MaxCountableGapSeconds: 600,
		DebugMode:              true,
		DebugUnassignedClients: true,
	}
	if settings != want {
		t.Fatalf("settings = %+v want %+v", settings, want)
	}
}

func TestSettingsFromFormPropagatesError(t *testing.T) {
	keys := []string{"idle_threshold_seconds", "upload_interval_seconds", "poll_interval_seconds", "max_countable_gap_seconds"}
	base := url.Values{
		"idle_threshold_seconds":    {"60"},
		"upload_interval_seconds":   {"60"},
		"poll_interval_seconds":     {"30"},
		"max_countable_gap_seconds": {"300"},
	}
	for _, key := range keys {
		form := url.Values{}
		for k, v := range base {
			form[k] = v
		}
		form.Set(key, "0")
		request := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_ = request.ParseForm()
		if _, err := settingsFromForm(request); err == nil {
			t.Fatalf("expected error for invalid %s", key)
		}
	}
}

func TestPositiveIntForm(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "valid", value: "42", want: 42},
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-3", wantErr: true},
		{name: "not a number", value: "abc", wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{"k": {tt.value}}
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			_ = request.ParseForm()
			got, err := positiveIntForm(request, "k")
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestCheckboxForm(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "on", want: true},
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "yes", want: true},
		{value: "YES", want: true},
		{value: "", want: false},
		{value: "off", want: false},
		{value: "0", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			form := url.Values{"flag": {tt.value}}
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			_ = request.ParseForm()
			if got := checkboxForm(request, "flag"); got != tt.want {
				t.Fatalf("checkboxForm(%q) = %v want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestCommandAckPathHelpers(t *testing.T) {
	if !isCommandAckPath("/api/v1/client/commands/5/ack") {
		t.Fatalf("valid ack path not recognized")
	}
	if isCommandAckPath("/api/v1/client/commands/5") {
		t.Fatalf("non-ack path wrongly recognized")
	}
	id, err := commandIDFromAckPath("/api/v1/client/commands/77/ack")
	if err != nil || id != 77 {
		t.Fatalf("commandIDFromAckPath = %d err=%v", id, err)
	}
	if _, err := commandIDFromAckPath("/api/v1/client/commands/x/ack"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestMethodNotAllowedBranches(t *testing.T) {
	app := testApp(t)
	tests := []string{
		"/api/v1/events/batch",
		"/api/v1/client/config",
		"/api/v1/client/commands",
	}
	for _, path := range tests {
		request := httptest.NewRequest(http.MethodDelete, path, nil)
		request.SetBasicAuth("client", "client")
		recorder := httptest.NewRecorder()
		app.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s method status = %d", path, recorder.Code)
		}
	}
}

func TestServeHTTPNotFound(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/nope", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d", recorder.Code)
	}
}

func TestStringsHasPrefix(t *testing.T) {
	if !stringsHasPrefix("/admin/users", "/admin/") {
		t.Fatalf("prefix match failed")
	}
	if stringsHasPrefix("/", "/admin/") {
		t.Fatalf("short string should not match")
	}
}
