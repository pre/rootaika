package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rootaika/client-windows/internal/model"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

func New(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	if httpClient != nil {
		c.httpClient = httpClient
	}
	return c
}

func (c *Client) PostEvents(ctx context.Context, batch model.EventBatch) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/api/v1/events/batch", nil, batch)
	return err
}

func (c *Client) FetchConfig(ctx context.Context, clientID string) (model.ClientConfig, error) {
	q := url.Values{"client_id": []string{clientID}}
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

func (c *Client) FetchCommands(ctx context.Context, clientID string) ([]model.Command, error) {
	q := url.Values{"client_id": []string{clientID}}
	body, err := c.doJSON(ctx, http.MethodGet, "/api/v1/client/commands", q, nil)
	if err != nil {
		return nil, err
	}
	var wrapped model.CommandsResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Commands != nil {
		return wrapped.Commands, nil
	}
	var commands []model.Command
	if err := json.Unmarshal(body, &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

func (c *Client) AckCommand(ctx context.Context, commandID string) error {
	if commandID == "" {
		return fmt.Errorf("command id is empty")
	}
	path := fmt.Sprintf("/api/v1/client/commands/%s/ack", url.PathEscape(commandID))
	_, err := c.doJSON(ctx, http.MethodPost, path, nil, nil)
	return err
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

	var body io.Reader
	if payload != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return nil, err
		}
		body = &buf
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if readErr != nil {
			return nil, fmt.Errorf("%s %s failed with %s", method, path, resp.Status)
		}
		return nil, fmt.Errorf("%s %s failed with %s: %s", method, path, resp.Status, strings.TrimSpace(string(responseBody)))
	}
	if readErr != nil {
		return nil, readErr
	}
	return responseBody, nil
}
