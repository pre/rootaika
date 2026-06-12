package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"rootaika/client-windows/internal/model"
)

func TestClientPayloadsAndBasicAuth(t *testing.T) {
	ctx := context.Background()
	acked := false

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "client" || pass != "secret" {
			t.Fatalf("missing or wrong basic auth: %q/%q", user, pass)
		}
		switch r.URL.Path {
		case "/api/v1/events/batch":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			var batch model.EventBatch
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Fatalf("decode batch: %v", err)
			}
			if batch.ClientID != "client-1" || len(batch.Events) != 1 {
				t.Fatalf("unexpected batch: %+v", batch)
			}
			if batch.Events[0].ProcessName != "steam.exe" || batch.Events[0].Sequence != 7 {
				t.Fatalf("unexpected event payload: %+v", batch.Events[0])
			}
			return testResponse(http.StatusNoContent, ""), nil
		case "/api/v1/client/config":
			if r.URL.Query().Get("client_id") != "client-1" {
				t.Fatalf("missing client_id query")
			}
			return testResponse(http.StatusOK, `{"idle_threshold_seconds":30,"upload_interval_seconds":15,"poll_interval_seconds":5}`), nil
		case "/api/v1/client/commands":
			return testResponse(http.StatusOK, `{"commands":[{"command_id":"cmd-1","type":"lock"}]}`), nil
		case "/api/v1/client/commands/cmd-1/ack":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected ack method: %s", r.Method)
			}
			acked = true
			return testResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(httpClient)
	err := client.PostEvents(ctx, model.EventBatch{
		ClientID: "client-1",
		Events: []model.Event{{
			EventID:     "event-1",
			Type:        model.EventTypeActivityObserved,
			OccurredAt:  time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC),
			State:       model.StateActive,
			ProcessName: "steam.exe",
			Sequence:    7,
		}},
	})
	if err != nil {
		t.Fatalf("PostEvents: %v", err)
	}

	cfg, err := client.FetchConfig(ctx, "client-1")
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.IdleThresholdSeconds != 30 || cfg.UploadIntervalSeconds != 15 || cfg.PollIntervalSeconds != 5 {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	commands, err := client.FetchCommands(ctx, "client-1")
	if err != nil {
		t.Fatalf("FetchCommands: %v", err)
	}
	if len(commands) != 1 || commands[0].Identifier() != "cmd-1" || commands[0].Type != model.CommandLock {
		t.Fatalf("unexpected commands: %+v", commands)
	}
	if err := client.AckCommand(ctx, commands[0].Identifier()); err != nil {
		t.Fatalf("AckCommand: %v", err)
	}
	if !acked {
		t.Fatalf("ack endpoint was not called")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
