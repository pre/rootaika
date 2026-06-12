package serviceapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rootaika/client-windows/internal/buffer"
	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/model"
)

func newStore(t *testing.T, cfg config.Config) *stateStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.json")
	if err := config.Save(path, &cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return &stateStore{path: path, current: cfg}
}

func TestHandleCommandLockUnlock(t *testing.T) {
	store := newStore(t, config.Config{})

	if err := handleCommand(store, model.Command{Type: model.CommandLock}); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if !store.snapshot().Locked {
		t.Fatalf("lock command did not set Locked")
	}
	// Persisted to disk.
	reloaded, err := config.LoadOrCreate(store.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.Locked {
		t.Fatalf("lock state was not persisted")
	}

	// No-op when already locked.
	if err := handleCommand(store, model.Command{Type: model.CommandLock}); err != nil {
		t.Fatalf("repeat lock: %v", err)
	}

	if err := handleCommand(store, model.Command{Type: model.CommandUnlock}); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if store.snapshot().Locked {
		t.Fatalf("unlock command did not clear Locked")
	}
	// No-op when already unlocked.
	if err := handleCommand(store, model.Command{Type: model.CommandUnlock}); err != nil {
		t.Fatalf("repeat unlock: %v", err)
	}
}

func TestHandleCommandUnknownType(t *testing.T) {
	store := newStore(t, config.Config{})
	if err := handleCommand(store, model.Command{Type: model.CommandType("frobnicate")}); err == nil {
		t.Fatalf("expected error for unknown command type")
	}
}

func TestSecondsOrDefault(t *testing.T) {
	if got := secondsOrDefault(0, 60); got != 60*time.Second {
		t.Fatalf("zero should use fallback, got %v", got)
	}
	if got := secondsOrDefault(-5, 30); got != 30*time.Second {
		t.Fatalf("negative should use fallback, got %v", got)
	}
	if got := secondsOrDefault(10, 60); got != 10*time.Second {
		t.Fatalf("positive should be used, got %v", got)
	}
}

func TestStateStoreUpdateSnapshot(t *testing.T) {
	store := newStore(t, config.Config{})

	// fn returns false -> no save, no error.
	if err := store.update(func(*config.Config) bool { return false }); err != nil {
		t.Fatalf("noop update: %v", err)
	}

	// fn returns true -> saved to disk.
	if err := store.update(func(c *config.Config) bool {
		c.DebugMode = true
		return true
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !store.snapshot().DebugMode {
		t.Fatalf("snapshot did not reflect update")
	}
	reloaded, err := config.LoadOrCreate(store.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.DebugMode {
		t.Fatalf("update with true was not persisted")
	}
}

func TestAuthorizeAgent(t *testing.T) {
	store := newStore(t, config.Config{AgentToken: "secret-token"})

	t.Run("correct token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/agent/state", nil)
		req.Header.Set("X-Rootaika-Agent-Token", "secret-token")
		if !authorizeAgent(store, rec, req) {
			t.Fatalf("expected authorization to succeed")
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/agent/state", nil)
		req.Header.Set("X-Rootaika-Agent-Token", "wrong")
		if authorizeAgent(store, rec, req) {
			t.Fatalf("expected authorization to fail")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestStartAgentHTTPEndpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newStore(t, config.Config{
		AgentListenAddress:     "127.0.0.1:0",
		AgentToken:             "tok",
		IdleThresholdSeconds:   45,
		ObserveIntervalSeconds: 5,
		DebugMode:              true,
	})
	eventBuffer, err := buffer.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("buffer open: %v", err)
	}
	defer eventBuffer.Close()

	server, err := startAgentHTTP(ctx, store, eventBuffer)
	if err != nil {
		t.Fatalf("startAgentHTTP: %v", err)
	}
	defer server.Close()

	// Drive the registered mux directly via httptest to avoid port races.
	handler := server.Handler

	t.Run("state requires token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/agent/state", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without token, got %d", rec.Code)
		}
	})

	t.Run("state ok with token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/agent/state", nil)
		req.Header.Set("X-Rootaika-Agent-Token", "tok")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("state wrong method", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/state", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("events queued", func(t *testing.T) {
		rec := httptest.NewRecorder()
		body := `{"events":[{"state":"active","process_name":"steam.exe"}]}`
		req := httptest.NewRequest(http.MethodPost, "/agent/events", strings.NewReader(body))
		req.Header.Set("X-Rootaika-Agent-Token", "tok")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
		}
		count, err := eventBuffer.CountPending(ctx)
		if err != nil {
			t.Fatalf("count pending: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 queued event, got %d", count)
		}
	})

	t.Run("events empty rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/agent/events", strings.NewReader(`{"events":[]}`))
		req.Header.Set("X-Rootaika-Agent-Token", "tok")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty events, got %d", rec.Code)
		}
	})
}
