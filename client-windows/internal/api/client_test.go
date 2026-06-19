package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"rootaika/client-windows/internal/model"
)

func TestClientPayloadsAndBasicAuth(t *testing.T) {
	ctx := context.Background()

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
			if r.URL.Query().Get("status") != "locked" {
				t.Fatalf("missing status query, got %q", r.URL.Query().Get("status"))
			}
			if r.URL.Query().Get("client_version") != "v1.0.0" {
				t.Fatalf("missing client_version query, got %q", r.URL.Query().Get("client_version"))
			}
			return testResponse(http.StatusOK, `{"config_version":"abc123","idle_threshold_seconds":30,"upload_interval_seconds":15,"poll_interval_seconds":5,"locked":true,"lock_message":"Aika lopettaa"}`), nil
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

	cfg, err := client.FetchConfig(ctx, "client-1", "locked", "", "v1.0.0", 0)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.IdleThresholdSeconds != 30 || cfg.UploadIntervalSeconds != 15 || cfg.PollIntervalSeconds != 5 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Locked == nil || !*cfg.Locked || cfg.LockMessage != "Aika lopettaa" {
		t.Fatalf("unexpected lock in config: %+v", cfg)
	}
	if cfg.ConfigVersion != "abc123" {
		t.Fatalf("unexpected config_version: %q", cfg.ConfigVersion)
	}
}

func TestFetchConfigSendsWaitAndConfigVersion(t *testing.T) {
	var gotWait, gotConfigVersion string
	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotWait = r.URL.Query().Get("wait")
			gotConfigVersion = r.URL.Query().Get("config_version")
			return testResponse(http.StatusOK, `{"config_version":"v2","idle_threshold_seconds":30,"upload_interval_seconds":15,"poll_interval_seconds":5}`), nil
		}),
	})

	cfg, err := client.FetchConfig(context.Background(), "client-1", "", "v1", "", 25)
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if gotWait != "25" {
		t.Fatalf("wait query = %q, want 25", gotWait)
	}
	if gotConfigVersion != "v1" {
		t.Fatalf("config_version query = %q, want v1", gotConfigVersion)
	}
	if cfg.ConfigVersion != "v2" {
		t.Fatalf("config_version = %q, want v2", cfg.ConfigVersion)
	}
}

func TestFetchConfigOmitsWaitWhenZero(t *testing.T) {
	var hadWait, hadConfigVersion bool
	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			_, hadWait = r.URL.Query()["wait"]
			_, hadConfigVersion = r.URL.Query()["config_version"]
			return testResponse(http.StatusOK, `{"idle_threshold_seconds":30,"upload_interval_seconds":15,"poll_interval_seconds":5}`), nil
		}),
	})

	if _, err := client.FetchConfig(context.Background(), "client-1", "", "v1", "", 0); err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if hadWait || hadConfigVersion {
		t.Fatalf("legacy poll must omit wait/config_version, got wait=%v config_version=%v", hadWait, hadConfigVersion)
	}
}

func testClientNoSleep(transport http.RoundTripper) *Client {
	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(&http.Client{Transport: transport})
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client
}

func TestRetriesOnServerErrorThenSucceeds(t *testing.T) {
	calls := 0
	client := testClientNoSleep(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return testResponse(http.StatusServiceUnavailable, "down"), nil
		}
		return testResponse(http.StatusOK, `{"commands":[]}`), nil
	}))

	if _, err := client.FetchConfig(context.Background(), "client-1", "", "", "", 0); err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestDoesNotRetryClientError(t *testing.T) {
	calls := 0
	client := testClientNoSleep(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return testResponse(http.StatusBadRequest, "nope"), nil
	}))

	if err := client.PostEvents(context.Background(), model.EventBatch{ClientID: "c", Events: []model.Event{{State: model.StateIdle}}}); err == nil {
		t.Fatalf("expected error on 400")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt for 4xx, got %d", calls)
	}
}

func TestRetriesOnTransportErrorAndGivesUp(t *testing.T) {
	calls := 0
	client := testClientNoSleep(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection refused")
	}))
	client.WithRetry(3, time.Millisecond, time.Millisecond)
	client.sleep = func(context.Context, time.Duration) error { return nil }

	if _, err := client.FetchConfig(context.Background(), "client-1", "", "", "", 0); err == nil {
		t.Fatalf("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestStopsRetryingWhenContextCancelled(t *testing.T) {
	calls := 0
	client := testClientNoSleep(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, r.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.FetchConfig(ctx, "client-1", "", "", "", 0); err == nil {
		t.Fatalf("expected error on cancelled context")
	}
	if calls != 1 {
		t.Fatalf("cancelled context should not retry, got %d attempts", calls)
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	client := New("http://x", "u", "p").WithRetry(5, 100*time.Millisecond, 250*time.Millisecond)
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 250 * time.Millisecond},
		{4, 250 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := client.backoff(tc.attempt); got != tc.want {
			t.Fatalf("backoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestDownloadWarningSound(t *testing.T) {
	ctx := context.Background()
	want := append([]byte("ID3"), bytes.Repeat([]byte(" fake mp3 bytes"), 90_000)...)

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/warning-sound" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != "client" || pass != "secret" {
			t.Fatalf("missing or wrong basic auth: %q/%q", user, pass)
		}
		if got := r.Header.Get("Accept"); got != "audio/mpeg" {
			t.Fatalf("Accept = %q, want audio/mpeg", got)
		}
		resp := testResponseBytes(http.StatusOK, want)
		resp.Header.Set("Content-Type", "audio/mpeg")
		return resp, nil
	})}

	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(httpClient)
	got, err := client.DownloadWarningSound(ctx)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes = %q, want %q", got, want)
	}
}

func TestDownloadWarningSoundRejectsOversizedResponse(t *testing.T) {
	body := bytes.Repeat([]byte("x"), maxWarningSoundBytes+1)
	client := New("http://rootaika.test", "client", "secret").WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testResponseBytes(http.StatusOK, body), nil
		}),
	})

	if _, err := client.DownloadWarningSound(context.Background()); err == nil {
		t.Fatal("expected oversized warning sound to fail")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testResponse(status int, body string) *http.Response {
	return testResponseBytes(status, []byte(body))
}

func testResponseBytes(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
