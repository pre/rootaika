package rootaika

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func (a *App) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleClient); !ok {
		return
	}

	clientID := r.URL.Query().Get("client_id")
	config, err := a.store.ClientConfig(r.Context(), clientID, a.now())
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
		"idle_threshold_seconds":    config.IdleThresholdSeconds,
		"upload_interval_seconds":   config.UploadIntervalSeconds,
		"poll_interval_seconds":     config.PollIntervalSeconds,
		"max_countable_gap_seconds": config.MaxCountableGapSeconds,
		"debug_mode":                config.DebugMode,
		"locked":                    config.Locked,
		"lock_message":              config.LockMessage,
		"warning_seconds":           config.WarningSeconds,
		"categories":                categories,
	})
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
	case len(parts) == 1 && parts[0] == "settings":
		a.adminSettings(w, r)
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
	redirect(w, r, "/#devices")
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
	redirect(w, r, "/#devices")
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
	redirect(w, r, "/#devices")
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
	redirect(w, r, "/#settings")
}

func (a *App) adminCreateCategory(w http.ResponseWriter, r *http.Request) {
	err := a.store.CreateCategory(r.Context(), r.FormValue("match_type"), r.FormValue("pattern"), r.FormValue("category"), a.now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirect(w, r, "/#categories")
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
	redirect(w, r, "/#categories")
}

func (a *App) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	if err := a.store.CreateUser(r.Context(), r.FormValue("name"), a.now()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	redirect(w, r, "/#users")
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
	redirect(w, r, "/#users")
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
	return Settings{
		IdleThresholdSeconds:   idle,
		UploadIntervalSeconds:  upload,
		PollIntervalSeconds:    poll,
		MaxCountableGapSeconds: maxGap,
		ChartYMaxMinutes:       chartYMax,
		DebugMode:              checkboxForm(r, "debug_mode"),
		DebugUnassignedClients: checkboxForm(r, "debug_unassigned_clients"),
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
