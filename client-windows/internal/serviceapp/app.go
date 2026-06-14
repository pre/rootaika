package serviceapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"rootaika/client-windows/internal/agentrunner"
	"rootaika/client-windows/internal/api"
	"rootaika/client-windows/internal/buffer"
	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/model"
)

type stateStore struct {
	mu            sync.RWMutex
	path          string
	current       config.Config
	lastReported  string
	configVersion string
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

	serverConfig, err := client.FetchConfig(ctx, cfg.ClientID, store.reported(), store.version(), wait)
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
	return nil
}

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
