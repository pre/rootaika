package agentapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"rootaika/client-windows/internal/activity"
	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/consolewin"
	"rootaika/client-windows/internal/lock"
	"rootaika/client-windows/internal/model"
)

// heartbeat is the longest interval between activity_observed events when the
// state and foreground process stay unchanged. It keeps server-side segments
// bounded well below max_countable_gap_seconds even on quiet periods.
const heartbeat = 60 * time.Second

// Run executes the user-session agent loop until the context is cancelled.
func Run(ctx context.Context, cfgPath string) error {
	cfg, err := config.LoadOrCreate(cfgPath)
	if err != nil {
		return err
	}
	probe := activity.NewProbe()
	locker := lock.NewController()
	defer locker.Close()
	debugShutdown := locker.DebugShutdown()
	console := consolewin.New()
	defer console.Close()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	local := localClient{httpClient: httpClient, address: cfg.AgentListenAddress, token: cfg.AgentToken}
	state := serviceState{
		Locked:                 cfg.Locked,
		LockMessage:            cfg.LockMessage,
		LockWarningSeconds:     cfg.LockWarningSeconds,
		IdleThresholdSeconds:   cfg.IdleThresholdSeconds,
		ObserveIntervalSeconds: cfg.ObserveIntervalSeconds,
		DebugMode:              cfg.DebugMode,
	}

	log.Printf("rootaika-agent started, service endpoint %s", cfg.AgentListenAddress)

	var last *model.Event
	var lastSentAt time.Time

	// Lock warning state. When the server requests a lock with LockWarningSeconds
	// > 0, the agent first runs a non-blocking countdown (looping MP3 + click-
	// through overlay) in a goroutine so the game stays playable, and only engages
	// the black lock overlay once the countdown elapses. An unlock during the
	// countdown cancels it.
	var (
		warnCancel context.CancelFunc
		warnDone   chan struct{}
		warning    bool
		warned     bool
	)

	for {
		if latest, err := local.fetchState(ctx); err == nil {
			state = latest
			log.Printf("received state from service: debug=%t locked=%t warning=%ds idle_threshold=%ds observe=%ds",
				state.DebugMode, state.Locked, state.LockWarningSeconds, state.IdleThresholdSeconds, state.ObserveIntervalSeconds)
		} else {
			log.Printf("fetch service state failed: %v", err)
		}
		if err := console.SetVisible(state.DebugMode); err != nil {
			log.Printf("set console visibility failed: %v", err)
		}

		// screenLocked is the actual overlay state, which lags state.Locked while a
		// warning countdown is in progress. The reported activity event uses this,
		// not the server intent, so play during the countdown is still counted.
		screenLocked := false
		if state.Locked {
			switch {
			case warned || state.LockWarningSeconds <= 0:
				if err := locker.SetLocked(ctx, true, state.LockMessage, state.DebugMode); err != nil {
					log.Printf("set locked state failed: %v", err)
				}
				screenLocked = true
			case warning:
				select {
				case <-warnDone:
					warning = false
					warned = true
					if err := locker.SetLocked(ctx, true, state.LockMessage, state.DebugMode); err != nil {
						log.Printf("set locked state failed: %v", err)
					}
					screenLocked = true
				default:
				}
			default:
				wctx, cancel := context.WithCancel(ctx)
				warnCancel = cancel
				warnDone = make(chan struct{})
				warning = true
				go func(msg string, secs int, soundPath string, done chan struct{}) {
					defer close(done)
					if err := locker.Warn(wctx, msg, secs, soundPath); err != nil {
						log.Printf("lock warning failed: %v", err)
					}
				}(state.LockMessage, state.LockWarningSeconds, state.WarningSoundPath, warnDone)
				log.Printf("lock warning started: %ds", state.LockWarningSeconds)
			}
		} else {
			if warnCancel != nil {
				warnCancel()
				warnCancel = nil
			}
			warning = false
			warned = false
			if err := locker.SetLocked(ctx, false, "", false); err != nil {
				log.Printf("set locked state failed: %v", err)
			}
		}

		if consumeDebugShutdown(ctx, local, state, debugShutdown) {
			return nil
		}

		snapshot, err := probe.Snapshot(ctx)
		if err != nil {
			log.Printf("activity probe failed: %v", err)
		} else {
			event := eventFromSnapshot(snapshot, state, screenLocked)
			if shouldEmit(last, event, lastSentAt, event.OccurredAt, heartbeat) {
				if err := local.postEvent(ctx, event); err != nil {
					log.Printf("post event to service failed: %v", err)
				} else {
					stored := event
					last = &stored
					lastSentAt = event.OccurredAt
					log.Printf("sent event to service: state=%s process=%q at=%s",
						event.State, event.ProcessName, event.OccurredAt.Format(time.RFC3339))
				}
			}
		}

		interval := time.Duration(state.ObserveIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 5 * time.Second
		}
		if !sleepOrDebugShutdown(ctx, interval, local, state, debugShutdown) {
			return nil
		}
	}
}

func consumeDebugShutdown(ctx context.Context, local localClient, state serviceState, signal <-chan struct{}) bool {
	if signal == nil {
		return false
	}
	select {
	case <-signal:
		return requestDebugShutdown(ctx, local, state)
	default:
		return false
	}
}

func sleepOrDebugShutdown(ctx context.Context, duration time.Duration, local localClient, state serviceState, signal <-chan struct{}) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-signal:
		return !requestDebugShutdown(ctx, local, state)
	}
}

func requestDebugShutdown(ctx context.Context, local localClient, state serviceState) bool {
	if !state.DebugMode || !state.Locked {
		return false
	}
	if err := local.requestDebugShutdown(ctx); err != nil {
		log.Printf("debug shutdown request failed: %v", err)
		return false
	}
	log.Printf("debug shutdown accepted by service, exiting agent")
	return true
}

// shouldEmit decides whether the freshly observed event must be sent. An event
// is emitted when the state or foreground process changed since the last sent
// event, or when the heartbeat interval has elapsed, so the server sees changes
// immediately while still receiving periodic confirmation that the device is alive.
func shouldEmit(last *model.Event, current model.Event, lastSentAt, now time.Time, heartbeat time.Duration) bool {
	if last == nil {
		return true
	}
	if last.State != current.State || last.ProcessName != current.ProcessName {
		return true
	}
	if heartbeat <= 0 {
		return true
	}
	return !now.Before(lastSentAt.Add(heartbeat))
}

// eventFromSnapshot maps an activity snapshot to a reportable event. locked is
// the actual screen-overlay state, which is false during a lock warning
// countdown so the playable grace period is still counted as usage.
func eventFromSnapshot(snapshot activity.Snapshot, state serviceState, locked bool) model.Event {
	if snapshot.At.IsZero() {
		snapshot.At = time.Now().UTC()
	}
	event := model.Event{
		Type:       model.EventTypeActivityObserved,
		OccurredAt: snapshot.At.UTC(),
		State:      model.StateActive,
	}
	if locked {
		event.State = model.StateLocked
		return event
	}
	idleThreshold := time.Duration(state.IdleThresholdSeconds) * time.Second
	if idleThreshold <= 0 {
		idleThreshold = 60 * time.Second
	}
	if snapshot.IdleFor >= idleThreshold {
		event.State = model.StateIdle
		return event
	}
	event.ProcessName = normalizeProcessName(snapshot.ForegroundProcess)
	return event
}

func normalizeProcessName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, `\`, `/`)
	return strings.ToLower(path.Base(name))
}

type localClient struct {
	httpClient *http.Client
	address    string
	token      string
}

func (c localClient) fetchState(ctx context.Context) (serviceState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/agent/state"), nil)
	if err != nil {
		return serviceState{}, err
	}
	req.Header.Set("X-Rootaika-Agent-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return serviceState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return serviceState{}, fmt.Errorf("GET /agent/state failed with %s", resp.Status)
	}
	var state serviceState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return serviceState{}, err
	}
	return state, nil
}

func (c localClient) postEvent(ctx context.Context, event model.Event) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(agentEventsRequest{Events: []model.Event{event}}); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/agent/events"), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Rootaika-Agent-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST /agent/events failed with %s", resp.Status)
	}
	return nil
}

func (c localClient) requestDebugShutdown(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/agent/debug-shutdown"), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Rootaika-Agent-Token", c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST /agent/debug-shutdown failed with %s", resp.Status)
	}
	return nil
}

func (c localClient) endpoint(endpointPath string) string {
	base := c.address
	if _, err := url.ParseRequestURI(base); err != nil || !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return strings.TrimRight(base, "/") + endpointPath
}

type serviceState struct {
	Locked                 bool   `json:"locked"`
	LockMessage            string `json:"lock_message"`
	LockWarningSeconds     int    `json:"lock_warning_seconds"`
	IdleThresholdSeconds   int    `json:"idle_threshold_seconds"`
	ObserveIntervalSeconds int    `json:"observe_interval_seconds"`
	DebugMode              bool   `json:"debug_mode"`
	WarningSoundPath       string `json:"warning_sound_path"`
}

type agentEventsRequest struct {
	Events []model.Event `json:"events"`
}
