package serviceapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"rootaika/client-windows/internal/agentrunner"
	"rootaika/client-windows/internal/api"
	"rootaika/client-windows/internal/buffer"
	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/model"
	"rootaika/client-windows/internal/updater"
	"rootaika/client-windows/internal/version"
)

type stateStore struct {
	mu            sync.RWMutex
	path          string
	current       config.Config
	lastReported  string
	configVersion string
	// failedUpdate records the last OTA version that failed to download/apply and
	// when, so the poll loop can skip retrying a bad release for a cooldown window
	// instead of spinning on every poll.
	failedUpdate     string
	failedUpdateTime time.Time
}

func Run(ctx context.Context, cfgPath string) error {
	cfgPath = config.ResolvePath(cfgPath)
	cfg, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return err
	}

	eventBuffer, err := buffer.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer eventBuffer.Close()

	store := &stateStore{path: cfgPath, current: *cfg}
	server, err := startAgentHTTP(ctx, store, eventBuffer)
	if err != nil {
		return err
	}
	defer server.Close()

	go uploadLoop(ctx, store, eventBuffer)
	go pollLoop(ctx, store)
	go watchdogLoop(ctx, store, cfgPath)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	return nil
}

func startAgentHTTP(ctx context.Context, store *stateStore, eventBuffer *buffer.Buffer) (*http.Server, error) {
	cfg := store.snapshot()
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeAgent(store, w, r) {
			return
		}
		cfg := store.snapshot()
		writeJSON(w, http.StatusOK, agentStateResponse{
			Locked:                 cfg.Locked,
			LockMessage:            cfg.LockMessage,
			LockWarningSeconds:     cfg.LockWarningSeconds,
			IdleThresholdSeconds:   cfg.IdleThresholdSeconds,
			ObserveIntervalSeconds: cfg.ObserveIntervalSeconds,
			DebugMode:              cfg.DebugMode,
			WarningSoundPath:       cachedWarningSoundPath(cfg, store.path),
		})
	})
	mux.HandleFunc("/agent/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeAgent(store, w, r) {
			return
		}
		var req agentEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Events) == 0 {
			http.Error(w, "events is empty", http.StatusBadRequest)
			return
		}
		queued := 0
		for _, event := range req.Events {
			if _, err := eventBuffer.Enqueue(r.Context(), event); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			queued++
		}
		// Remember the most recent state the agent observed so the poll loop can
		// report it back to the server as the device's current status.
		if queued > 0 {
			store.setReported(string(req.Events[len(req.Events)-1].State))
		}
		writeJSON(w, http.StatusAccepted, map[string]int{"queued": queued})
	})

	listener, err := net.Listen("tcp", cfg.AgentListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen agent endpoint %s: %w", cfg.AgentListenAddress, err)
	}
	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		log.Printf("agent endpoint listening on %s", listener.Addr())
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("agent endpoint stopped: %v", err)
		}
	}()
	return server, nil
}

func uploadLoop(ctx context.Context, store *stateStore, eventBuffer *buffer.Buffer) {
	for {
		if err := uploadOnce(ctx, store, eventBuffer); err != nil {
			log.Printf("event upload failed: %v", err)
		}
		cfg := store.snapshot()
		if !sleep(ctx, secondsOrDefault(cfg.UploadIntervalSeconds, 60)) {
			return
		}
	}
}

func uploadOnce(ctx context.Context, store *stateStore, eventBuffer *buffer.Buffer) error {
	cfg := store.snapshot()
	events, err := eventBuffer.Pending(ctx, cfg.BatchSize)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	client := api.New(cfg.ServerURL, cfg.ClientUsername, cfg.ClientPassword)
	if err := client.PostEvents(ctx, model.EventBatch{ClientID: cfg.ClientID, Events: events}); err != nil {
		return err
	}
	log.Printf("uploaded %d event(s) to server %s", len(events), cfg.ServerURL)
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.EventID)
	}
	return eventBuffer.MarkSent(ctx, ids)
}

// minPollGapSeconds is the floor between consecutive long-poll requests. A
// successful long poll returns either when config changed or when the server's
// wait budget elapsed, so the loop normally re-hangs at once; this small gap
// only guards against a misconfigured server that returns instantly, so the
// client cannot spin into a hot loop.
const minPollGapSeconds = 1

func pollLoop(ctx context.Context, store *stateStore) {
	for {
		gap := minPollGapSeconds
		if err := pollOnce(ctx, store); err != nil {
			log.Printf("poll failed: %v", err)
			// Back off to the configured interval only on error; a healthy long
			// poll already paced itself by blocking on the server.
			gap = secondsOrDefaultInt(store.snapshot().PollIntervalSeconds, 30)
		}
		if !sleep(ctx, time.Duration(gap)*time.Second) {
			return
		}
	}
}

func pollOnce(ctx context.Context, store *stateStore) error {
	cfg := store.snapshot()
	wait := secondsOrDefaultInt(cfg.PollWaitSeconds, 25)
	// Cap the round trip beyond the server's wait budget so the transport does
	// not abort a healthy held request the instant it is about to return.
	client := api.New(cfg.ServerURL, cfg.ClientUsername, cfg.ClientPassword).
		WithTimeout(time.Duration(wait+10) * time.Second)

	serverConfig, err := client.FetchConfig(ctx, cfg.ClientID, store.reported(), store.version(), version.Version, wait)
	if err != nil {
		return err
	}
	store.setVersion(serverConfig.ConfigVersion)
	log.Printf("received config from server %s: version=%s debug=%t idle_threshold=%ds upload=%ds poll=%ds",
		cfg.ServerURL, serverConfig.ConfigVersion, serverConfig.DebugMode != nil && *serverConfig.DebugMode,
		serverConfig.IdleThresholdSeconds, serverConfig.UploadIntervalSeconds, serverConfig.PollIntervalSeconds)
	if err := store.update(func(cfg *config.Config) bool {
		return cfg.ApplyServerConfig(serverConfig)
	}); err != nil {
		return err
	}

	// Reconcile the cached warning MP3 with the server's version. A download
	// failure is logged but not fatal: the existing cached sound (if any) keeps
	// working and the next poll retries.
	if err := store.update(func(cfg *config.Config) bool {
		changed, err := syncWarningSound(ctx, client, cfg, store.path, serverConfig.WarningSoundVersion)
		if err != nil {
			log.Printf("warning sound sync failed: %v", err)
		}
		return changed
	}); err != nil {
		return err
	}

	log.Printf("config applied: locked=%t", store.snapshot().Locked)

	// OTA update check. The desired-version triple is transient (never persisted),
	// so it is read straight off the server response. A failure is logged but not
	// fatal: the client keeps running the current version and retries after a
	// cooldown, so a bad release cannot wedge the device.
	if err := maybeUpdate(ctx, store, serverConfig); err != nil {
		log.Printf("self-update failed: %v", err)
	}
	return nil
}

// updateRetryCooldown is how long the client waits before retrying a desired
// version that previously failed to download or apply, so a broken release does
// not spin on every poll.
const updateRetryCooldown = 30 * time.Minute

// updateExeName / stagedExeName are the on-disk names of the live binary and the
// staged download next to it in the install directory.
const (
	updateExeName = "rootaika.exe"
	stagedExeName = "rootaika.update.exe"
)

// maybeUpdate downloads and launches an OTA update when the server's desired
// version differs from the running one. It returns nil when no update is needed.
// The actual swap happens in a detached apply-update helper; this function only
// stages the verified binary and launches that helper, then asks the process to
// stop so the helper can replace the file.
func maybeUpdate(ctx context.Context, store *stateStore, sc model.ClientConfig) error {
	plan := updater.Plan{
		Version:  sc.DesiredVersion,
		Artifact: sc.ArtifactName,
		SHA256:   sc.SHA256,
	}
	if !updater.NeedsUpdate(version.Version, plan) {
		return nil
	}
	if store.updateOnCooldown(plan.Version, time.Now()) {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	installDir := filepath.Dir(exe)
	staged := filepath.Join(installDir, stagedExeName)
	target := filepath.Join(installDir, updateExeName)

	log.Printf("self-update: downloading %s (%s)", plan.Version, plan.Artifact)
	if err := updater.Download(ctx, plan, staged); err != nil {
		store.recordFailedUpdate(plan.Version, time.Now())
		return fmt.Errorf("download %s: %w", plan.Version, err)
	}

	log.Printf("self-update: launching apply-update helper")
	args := []string{"apply-update", "-target", target, "-staged", staged, "-service", serviceName, "-agent-process", updateExeName}
	if err := updater.LaunchDetached(staged, args); err != nil {
		store.recordFailedUpdate(plan.Version, time.Now())
		return fmt.Errorf("launch apply-update: %w", err)
	}
	return nil
}

// serviceName is the Windows service name install.ps1 registers; the apply-update
// helper stops and starts it around the file swap.
const serviceName = "rootaika-service"

func watchdogLoop(ctx context.Context, store *stateStore, cfgPath string) {
	runner := &agentrunner.Runner{ConfigPath: cfgPath}
	for {
		cfg := store.snapshot()
		runner.Path = cfg.AgentPath
		if err := runner.Ensure(ctx); err != nil {
			log.Printf("agent ensure failed: %v", err)
		}
		if !sleep(ctx, 15*time.Second) {
			return
		}
	}
}

func (s *stateStore) snapshot() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *stateStore) setReported(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReported = state
}

func (s *stateStore) reported() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastReported
}

func (s *stateStore) setVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configVersion = version
}

func (s *stateStore) version() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configVersion
}

// recordFailedUpdate remembers a version that failed so updateOnCooldown can
// suppress retries of the same version for updateRetryCooldown.
func (s *stateStore) recordFailedUpdate(ver string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedUpdate = ver
	s.failedUpdateTime = now
}

// updateOnCooldown reports whether the given version recently failed and is still
// within the retry cooldown window. A different version always clears the guard.
func (s *stateStore) updateOnCooldown(ver string, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.failedUpdate != ver {
		return false
	}
	return now.Sub(s.failedUpdateTime) < updateRetryCooldown
}

func (s *stateStore) update(fn func(*config.Config) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !fn(&s.current) {
		return nil
	}
	return config.Save(s.path, &s.current)
}

func authorizeAgent(store *stateStore, w http.ResponseWriter, r *http.Request) bool {
	cfg := store.snapshot()
	if r.Header.Get("X-Rootaika-Agent-Token") != cfg.AgentToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func secondsOrDefault(value int, fallback int) time.Duration {
	return time.Duration(secondsOrDefaultInt(value, fallback)) * time.Second
}

func secondsOrDefaultInt(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

type agentEventsRequest struct {
	Events []model.Event `json:"events"`
}

type agentStateResponse struct {
	Locked                 bool   `json:"locked"`
	LockMessage            string `json:"lock_message"`
	LockWarningSeconds     int    `json:"lock_warning_seconds"`
	IdleThresholdSeconds   int    `json:"idle_threshold_seconds"`
	ObserveIntervalSeconds int    `json:"observe_interval_seconds"`
	DebugMode              bool   `json:"debug_mode"`
	WarningSoundPath       string `json:"warning_sound_path"`
}
