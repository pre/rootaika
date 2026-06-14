package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rootaika/client-windows/internal/model"
)

const (
	defaultMaxAttempts = 4
	defaultBaseBackoff = 500 * time.Millisecond
	defaultMaxBackoff  = 5 * time.Second
)

type Client struct {
	baseURL     string
	username    string
	password    string
	httpClient  *http.Client
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	sleep       func(ctx context.Context, d time.Duration) error
}

func New(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxAttempts: defaultMaxAttempts,
		baseBackoff: defaultBaseBackoff,
		maxBackoff:  defaultMaxBackoff,
		sleep:       sleepCtx,
	}
}

// WithRetry overrides the retry policy. A non-positive attempts disables
// retrying (a single attempt is always made). Exposed for tests.
func (c *Client) WithRetry(attempts int, base, max time.Duration) *Client {
	if attempts < 1 {
		attempts = 1
	}
	c.maxAttempts = attempts
	c.baseBackoff = base
	c.maxBackoff = max
	return c
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

// WithTimeout sets the per-request transport timeout. Long-poll config requests
// need a cap larger than the server's wait budget, so the poll loop raises this
// above the 10s default; a non-positive value disables the cap.
func (c *Client) WithTimeout(d time.Duration) *Client {
	if c.httpClient != nil {
		c.httpClient.Timeout = d
	}
	return c
}

func (c *Client) PostEvents(ctx context.Context, batch model.EventBatch) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/api/v1/events/batch", nil, batch)
	return err
}

// DownloadWarningSound fetches the admin-uploaded lock-warning MP3. It reuses
// the shared retry/auth path, so a transient failure is retried like any other
// request. The returned bytes are the raw MP3 the caller caches locally.
func (c *Client) DownloadWarningSound(ctx context.Context) ([]byte, error) {
	return c.doJSON(ctx, http.MethodGet, "/api/v1/warning-sound", nil, nil)
}

// FetchConfig polls the server for this client's config. When waitSeconds > 0
// it long-polls: the server holds the request open until the config changes
// away from knownConfigVersion or waitSeconds elapses, so a lock/unlock or
// settings change reaches the client within milliseconds instead of at the next
// poll. knownConfigVersion is the config_version from the previous successful
// fetch; empty disables blocking for this call and the server returns at once.
// It travels as config_version, kept explicitly distinct from client_version.
// clientVersion is this client's own build version, reported so the server can
// record it and decide whether an OTA update is due; it travels as
// client_version.
func (c *Client) FetchConfig(ctx context.Context, clientID, status, knownConfigVersion, clientVersion string, waitSeconds int) (model.ClientConfig, error) {
	q := url.Values{"client_id": []string{clientID}}
	if status != "" {
		q.Set("status", status)
	}
	if clientVersion != "" {
		q.Set("client_version", clientVersion)
	}
	if waitSeconds > 0 {
		q.Set("wait", strconv.Itoa(waitSeconds))
		if knownConfigVersion != "" {
			q.Set("config_version", knownConfigVersion)
		}
	}
	body, err := c.doJSON(ctx, http.MethodGet, "/api/v1/client/config", q, nil)
	if err != nil {
		return model.ClientConfig{}, err
	}
	var cfg model.ClientConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return model.ClientConfig{}, err
	}
	return cfg, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("server URL is empty")
	}
	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}

	var payloadBytes []byte
	if payload != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return nil, err
		}
		payloadBytes = buf.Bytes()
	}

	attempts := c.maxAttempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := c.sleep(ctx, c.backoff(attempt)); err != nil {
				return nil, err
			}
		}

		body, retryable, err := c.attempt(ctx, method, path, endpoint.String(), payloadBytes, payload != nil)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s %s failed after %d attempts: %w", method, path, attempts, lastErr)
}

// attempt performs a single request. The returned bool reports whether the
// error is transient and worth retrying (network errors, 5xx, 429). 4xx and
// decode errors are terminal.
func (c *Client) attempt(ctx context.Context, method, path, endpoint string, payloadBytes []byte, hasPayload bool) ([]byte, bool, error) {
	var body io.Reader
	if hasPayload {
		body = bytes.NewReader(payloadBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, false, err
	}
	req.SetBasicAuth(c.username, c.password)
	if hasPayload {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Context cancellation is terminal; other transport errors are transient.
		if ctx.Err() != nil {
			return nil, false, err
		}
		return nil, true, err
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		if readErr != nil {
			return nil, retryable, fmt.Errorf("%s %s failed with %s", method, path, resp.Status)
		}
		return nil, retryable, fmt.Errorf("%s %s failed with %s: %s", method, path, resp.Status, strings.TrimSpace(string(responseBody)))
	}
	if readErr != nil {
		return nil, true, readErr
	}
	return responseBody, false, nil
}

// backoff returns the delay before the given attempt (1-based for delays),
// doubling from baseBackoff and capped at maxBackoff.
func (c *Client) backoff(attempt int) time.Duration {
	if c.baseBackoff <= 0 {
		return 0
	}
	delay := c.baseBackoff << (attempt - 1)
	if c.maxBackoff > 0 && delay > c.maxBackoff {
		delay = c.maxBackoff
	}
	return delay
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
