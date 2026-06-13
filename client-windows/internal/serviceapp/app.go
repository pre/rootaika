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
	mu           sync.RWMutex
	path         string
	current      config.Config
	lastReported string
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

func pollLoop(ctx context.Context, store *stateStore) {
	for {
		if err := pollOnce(ctx, store); err != nil {
			log.Printf("poll failed: %v", err)
		}
		cfg := store.snapshot()
		if !sleep(ctx, secondsOrDefault(cfg.PollIntervalSeconds, 30)) {
			return
		}
	}
}

func pollOnce(ctx context.Context, store *stateStore) error {
	cfg := store.snapshot()
	client := api.New(cfg.ServerURL, cfg.ClientUsername, cfg.ClientPassword)

	serverConfig, err := client.FetchConfig(ctx, cfg.ClientID, store.reported())
	if err != nil {
		return err
	}
	log.Printf("received config from server %s: debug=%t idle_threshold=%ds upload=%ds poll=%ds",
		cfg.ServerURL, serverConfig.DebugMode != nil && *serverConfig.DebugMode,
		serverConfig.IdleThresholdSeconds, serverConfig.UploadIntervalSeconds, serverConfig.PollIntervalSeconds)
	if err := store.update(func(cfg *config.Config) bool {
		return cfg.ApplyServerConfig(serverConfig)
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
	if value <= 0 {
		value = fallback
	}
	return time.Duration(value) * time.Second
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
}
