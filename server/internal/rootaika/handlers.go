package rootaika

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxWarningSeconds caps the lock warning countdown the admin can request, so a
// typo cannot leave a device unlockable for an unreasonable time (10 minutes).
const maxWarningSeconds = 600

type batchRequest struct {
	ClientID string              `json:"client_id"`
	Events   []batchEventRequest `json:"events"`
}

type batchEventRequest struct {
	EventID     string    `json:"event_id"`
	Type        string    `json:"type"`
	OccurredAt  time.Time `json:"occurred_at"`
	State       string    `json:"state"`
	ProcessName string    `json:"process_name,omitempty"`
	Sequence    int64     `json:"sequence"`
}

func (a *App) handleEventsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleClient); !ok {
		return
	}

	var request batchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	events, err := validateBatch(request)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := a.now()
	device, err := a.store.EnsureDevice(r.Context(), request.ClientID, now)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	accepted, ignored, err := a.store.InsertEvents(r.Context(), device.ID, events, now)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "insert events failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accepted":             accepted,
		"duplicate_or_ignored": ignored,
		"device_id":            device.ID,
	})
}

func validateBatch(request batchRequest) ([]EventInput, error) {
	if strings.TrimSpace(request.ClientID) == "" {
		return nil, errors.New("client_id is required")
	}
	if len(request.Events) == 0 {
		return nil, errors.New("events is required")
	}
	if len(request.Events) > 10000 {
		return nil, errors.New("too many events")
	}

	events := make([]EventInput, 0, len(request.Events))
	seen := map[string]struct{}{}
	for i, event := range request.Events {
		eventID := strings.TrimSpace(event.EventID)
		if eventID == "" {
			return nil, fmt.Errorf("events[%d].event_id is required", i)
		}
		if _, ok := seen[eventID]; ok {
			return nil, fmt.Errorf("events[%d].event_id is duplicated in request", i)
		}
		seen[eventID] = struct{}{}

		if event.Type != EventTypeActivityObserved {
			return nil, fmt.Errorf("events[%d].type must be %s", i, EventTypeActivityObserved)
		}
		if !validState(event.State) {
			return nil, fmt.Errorf("events[%d].state is invalid", i)
		}
		if event.OccurredAt.IsZero() {
			return nil, fmt.Errorf("events[%d].occurred_at is required", i)
		}
		processName := strings.TrimSpace(event.ProcessName)
		if event.State == StateActive && processName == "" {
			processName = "unknown"
		}
		events = append(events, EventInput{
			EventUUID:   eventID,
			Type:        event.Type,
			OccurredAt:  event.OccurredAt.UTC(),
			State:       event.State,
			ProcessName: processName,
			Sequence:    event.Sequence,
		})
	}
	return events, nil
}

// maxConfigWaitSeconds caps how long a long-poll config request may block. It
// bounds server-side resource use and keeps the held request well under any
// reverse-proxy idle timeout a future deployment might introduce.
const maxConfigWaitSeconds = 60

// handleWarningSound serves the admin-uploaded lock-warning MP3 to clients. It
// returns 404 when no sound is configured so a client treats "no sound" as a
// normal state and simply stays silent.
func (a *App) handleWarningSound(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRole(w, r, RoleClient); !ok {
		return
	}
	path := a.warningSound.path()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("ETag", `"`+a.warningSound.version()+`"`)
	http.ServeContent(w, r, warningSoundFileName, info.ModTime(), file)
}

func (a *App) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleClient); !ok {
		return
	}

	clientID := r.URL.Query().Get("client_id")
	config, err := a.effectiveConfig(r.Context(), clientID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The client reports its own current state in the same poll request. An
	// invalid or absent status is ignored so the poll never fails over it.
	if status := strings.TrimSpace(r.URL.Query().Get("status")); validState(status) {
		if err := a.store.RecordDeviceStatus(r.Context(), config.DeviceID, status, a.now()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "record status failed")
			return
		}
	}

	// The client also reports its running version in the same poll (OTA update);
	// the admin UI shows it next to the desired version.
	if clientVersion := r.URL.Query().Get("client_version"); clientVersion != "" {
		if err := a.store.RecordDeviceVersion(r.Context(), config.DeviceID, clientVersion, a.now()); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "record version failed")
			return
		}
	}

	// Long poll: when the client passes wait and its last-seen version still
	// matches the current config, hold the request open so a later change is
	// delivered the instant it happens instead of at the next poll interval.
	wait := clampWaitSeconds(r.URL.Query().Get("wait"))
	known := r.URL.Query().Get("config_version")
	if wait > 0 && known != "" && configVersion(config) == known {
		updated, ok := a.waitForConfigChange(r.Context(), clientID, known, wait)
		if !ok {
			// Client disconnected mid-wait; nothing to write.
			return
		}
		config = updated
	}

	a.writeClientConfig(w, clientID, config)
}

// waitForConfigChange blocks until the device's effective config version
// differs from known, the wait budget elapses, or the client disconnects. It
// subscribes before re-reading so a change racing the read is never missed. The
// returned bool is false only when the request context is cancelled.
func (a *App) waitForConfigChange(ctx context.Context, clientID, known string, wait int) (ClientConfig, bool) {
	timer := time.NewTimer(time.Duration(wait) * time.Second)
	defer timer.Stop()
	for {
		changed := a.notifier.subscribe()

		config, err := a.effectiveConfig(ctx, clientID)
		if err != nil {
			// Treat a transient read error like a timeout: fall back to
			// returning the last good config to the caller.
			return ClientConfig{}, false
		}
		if configVersion(config) != known {
			return config, true
		}

		select {
		case <-ctx.Done():
			return ClientConfig{}, false
		case <-timer.C:
			return config, true
		case <-changed:
		}
	}
}

// effectiveConfig reads the device's stored config and stamps it with the
// current warning-sound version, which the App tracks via the filesystem rather
// than the store. Both the immediate response and the long-poll comparison use
// this so a fresh upload changes config_version and reaches clients at once.
func (a *App) effectiveConfig(ctx context.Context, clientID string) (ClientConfig, error) {
	config, err := a.store.ClientConfig(ctx, clientID, a.now())
	if err != nil {
		return ClientConfig{}, err
	}
	config.WarningSoundVersion = a.warningSound.version()
	return config, nil
}

func (a *App) writeClientConfig(w http.ResponseWriter, clientID string, config ClientConfig) {
	categories := make([]map[string]string, 0, len(config.Categories))
	for _, category := range config.Categories {
		categories = append(categories, map[string]string{
			"match_type": category.MatchType,
			"pattern":    category.Pattern,
			"category":   category.Category,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"client_id":                 clientID,
		"config_version":            configVersion(config),
		"idle_threshold_seconds":    config.IdleThresholdSeconds,
		"upload_interval_seconds":   config.UploadIntervalSeconds,
		"poll_interval_seconds":     config.PollIntervalSeconds,
		"max_countable_gap_seconds": config.MaxCountableGapSeconds,
		"debug_mode":                config.DebugMode,
		"locked":                    config.Locked,
		"lock_message":              config.LockMessage,
		"warning_seconds":           config.WarningSeconds,
		"warning_sound_version":     config.WarningSoundVersion,
		"desired_version":           config.DesiredVersion,
		"artifact_name":             config.ArtifactName,
		"sha256":                    config.SHA256,
		"categories":                categories,
	})
}

// clampWaitSeconds parses the long-poll wait budget, clamping to
// 0..maxConfigWaitSeconds. Zero (blank/invalid/non-positive) disables long
// polling so the request returns immediately, preserving legacy poll behavior.
func clampWaitSeconds(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	if value > maxConfigWaitSeconds {
		return maxConfigWaitSeconds
	}
	return value
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(parts) == 3 && parts[0] == "devices" && (parts[2] == "lock" || parts[2] == "unlock"):
		a.adminDeviceCommand(w, r, parts[1], parts[2])
	case len(parts) == 3 && parts[0] == "devices" && parts[2] == "assign":
		a.adminAssignDevice(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "devices" && parts[2] == "delete":
		a.adminDeleteDevice(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "devices" && parts[2] == "version":
		a.adminDeviceVersion(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "versions":
		a.adminCreateVersion(w, r)
	case len(parts) == 3 && parts[0] == "versions" && parts[2] == "edit":
		a.adminEditVersion(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "versions" && parts[2] == "delete":
		a.adminDeleteVersion(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "settings":
		a.adminSettings(w, r)
	case len(parts) == 2 && parts[0] == "settings" && parts[1] == "warning-sound":
		a.adminWarningSound(w, r)
	case len(parts) == 1 && parts[0] == "categories":
		a.adminCreateCategory(w, r)
	case len(parts) == 3 && parts[0] == "categories" && parts[2] == "delete":
		a.adminDeleteCategory(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "users":
		a.adminCreateUser(w, r)
	case len(parts) == 3 && parts[0] == "users" && parts[2] == "rename":
		a.adminRenameUser(w, r, parts[1])
	default:
		http.NotFound(w, r)
	}
}

func (a *App) adminDeviceCommand(w http.ResponseWriter, r *http.Request, rawDeviceID, commandType string) {
	deviceID, err := strconv.ParseInt(rawDeviceID, 10, 64)
	if err != nil {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return
	}
	locked := commandType == "lock"
	message := ""
	warningSeconds := 0
	if locked {
		message = strings.TrimSpace(r.FormValue("message"))
		warningSeconds = clampWarningSeconds(r.FormValue("warning_seconds"))
	}
	if err := a.store.SetDeviceLock(r.Context(), deviceID, locked, message, warningSeconds, a.now()); err != nil {
		http.Error(w, "set device lock failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#devices")
}

func (a *App) adminAssignDevice(w http.ResponseWriter, r *http.Request, rawDeviceID string) {
	deviceID, err := strconv.ParseInt(rawDeviceID, 10, 64)
	if err != nil {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return
	}
	var userID *int64
	if rawUserID := strings.TrimSpace(r.FormValue("user_id")); rawUserID != "" {
		parsed, err := strconv.ParseInt(rawUserID, 10, 64)
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		userID = &parsed
	}
	if err := a.store.UpdateDevice(r.Context(), deviceID, r.FormValue("display_name"), userID); err != nil {
		http.Error(w, "update device failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#devices")
}

func (a *App) adminDeleteDevice(w http.ResponseWriter, r *http.Request, rawDeviceID string) {
	deviceID, err := strconv.ParseInt(rawDeviceID, 10, 64)
	if err != nil {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteDevice(r.Context(), deviceID); err != nil {
		http.Error(w, "delete device failed", http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/settings#devices")
}

func (a *App) adminSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := settingsFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.UpdateSettings(r.Context(), settings, a.now()); err != nil {
		http.Error(w, "update settings failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#settings")
}

// adminWarningSound stores the MP3 uploaded from the settings page and notifies
// pollers so clients pick up the new sound version immediately.
func (a *App) adminWarningSound(w http.ResponseWriter, r *http.Request) {
	if !a.warningSound.enabled() {
		http.Error(w, "warning sound storage is not configured", http.StatusServiceUnavailable)
		return
	}
	file, _, err := r.FormFile("sound")
	if err != nil {
		http.Error(w, "no file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if err := a.warningSound.save(file); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#settings")
}

func (a *App) adminCreateCategory(w http.ResponseWriter, r *http.Request) {
	err := a.store.CreateCategory(r.Context(), r.FormValue("match_type"), r.FormValue("pattern"), r.FormValue("category"), a.now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#categories")
}

func (a *App) adminDeleteCategory(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		http.Error(w, "invalid category id", http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteCategory(r.Context(), id); err != nil {
		http.Error(w, "delete category failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#categories")
}

func (a *App) adminCreateVersion(w http.ResponseWriter, r *http.Request) {
	err := a.store.CreateVersion(r.Context(), r.FormValue("version"), r.FormValue("artifact"), r.FormValue("sha256"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirect(w, r, "/settings#versions")
}

func (a *App) adminEditVersion(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		http.Error(w, "invalid version id", http.StatusBadRequest)
		return
	}
	if err := a.store.UpdateVersion(r.Context(), id, r.FormValue("version"), r.FormValue("artifact"), r.FormValue("sha256")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#versions")
}

func (a *App) adminDeleteVersion(w http.ResponseWriter, r *http.Request, rawID string) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		http.Error(w, "invalid version id", http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteVersion(r.Context(), id, a.now()); err != nil {
		http.Error(w, "delete version failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#versions")
}

// adminDeviceVersion sets the per-device OTA version selection. 0 (or blank)
// clears the override so the device inherits the global selection.
func (a *App) adminDeviceVersion(w http.ResponseWriter, r *http.Request, rawDeviceID string) {
	deviceID, err := strconv.ParseInt(rawDeviceID, 10, 64)
	if err != nil {
		http.Error(w, "invalid device id", http.StatusBadRequest)
		return
	}
	var versionID *int64
	if raw := strings.TrimSpace(r.FormValue("selected_version_id")); raw != "" && raw != "0" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid version id", http.StatusBadRequest)
			return
		}
		versionID = &parsed
	}
	if err := a.store.SetDeviceVersion(r.Context(), deviceID, versionID); err != nil {
		http.Error(w, "set device version failed", http.StatusInternalServerError)
		return
	}
	a.notifier.notify()
	redirect(w, r, "/settings#devices")
}

func (a *App) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	if err := a.store.CreateUser(r.Context(), r.FormValue("name"), a.now()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirect(w, r, "/settings#users")
}

func (a *App) adminRenameUser(w http.ResponseWriter, r *http.Request, rawUserID string) {
	userID, err := strconv.ParseInt(rawUserID, 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if err := a.store.RenameUser(r.Context(), userID, r.FormValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirect(w, r, "/settings#users")
}

func settingsFromForm(r *http.Request) (Settings, error) {
	idle, err := positiveIntForm(r, "idle_threshold_seconds")
	if err != nil {
		return Settings{}, err
	}
	upload, err := positiveIntForm(r, "upload_interval_seconds")
	if err != nil {
		return Settings{}, err
	}
	poll, err := positiveIntForm(r, "poll_interval_seconds")
	if err != nil {
		return Settings{}, err
	}
	maxGap, err := positiveIntForm(r, "max_countable_gap_seconds")
	if err != nil {
		return Settings{}, err
	}
	chartYMax, err := positiveIntForm(r, "chart_y_max_minutes")
	if err != nil {
		return Settings{}, err
	}
	boardRefresh, err := positiveIntForm(r, "board_refresh_seconds")
	if err != nil {
		return Settings{}, err
	}
	// Global OTA selection: 0 / blank / invalid all mean "no version selected",
	// so the field is optional and never fails the whole settings form.
	selectedVersion, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("selected_version_id")))
	if selectedVersion < 0 {
		selectedVersion = 0
	}
	return Settings{
		IdleThresholdSeconds:   idle,
		UploadIntervalSeconds:  upload,
		PollIntervalSeconds:    poll,
		MaxCountableGapSeconds: maxGap,
		ChartYMaxMinutes:       chartYMax,
		BoardRefreshSeconds:    boardRefresh,
		DebugMode:              checkboxForm(r, "debug_mode"),
		DebugUnassignedClients: checkboxForm(r, "debug_unassigned_clients"),
		SelectedVersionID:      selectedVersion,
	}, nil
}

func checkboxForm(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(key))) {
	case "on", "1", "true", "yes":
		return true
	default:
		return false
	}
}

// clampWarningSeconds parses the lock warning duration from the admin form,
// clamping it to 0..maxWarningSeconds. A blank or invalid value means no
// warning (lock immediately).
func clampWarningSeconds(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0
	}
	if value > maxWarningSeconds {
		return maxWarningSeconds
	}
	return value
}

func positiveIntForm(r *http.Request, key string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(r.FormValue(key)))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func redirect(w http.ResponseWriter, r *http.Request, target string) {
	http.Redirect(w, r, target, http.StatusSeeOther)
}
