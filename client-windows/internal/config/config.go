package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"

	"rootaika/client-windows/internal/model"
)

const (
	defaultServerURL              = "http://127.0.0.1:8080"
	defaultClientUsername         = "client"
	defaultAgentListenAddress     = "127.0.0.1:48611"
	defaultIdleThresholdSeconds   = 60
	defaultUploadIntervalSeconds  = 60
	defaultPollIntervalSeconds    = 30
	defaultObserveIntervalSeconds = 5
	defaultBatchSize              = 100
	defaultMaxCountableGapSeconds = 300
)

type Config struct {
	ClientID               string `json:"client_id"`
	ServerURL              string `json:"server_url"`
	ClientUsername         string `json:"client_username"`
	ClientPassword         string `json:"client_password"`
	DBPath                 string `json:"db_path"`
	AgentPath              string `json:"agent_path,omitempty"`
	AgentListenAddress     string `json:"agent_listen_address"`
	AgentToken             string `json:"agent_token"`
	IdleThresholdSeconds   int    `json:"idle_threshold_seconds"`
	UploadIntervalSeconds  int    `json:"upload_interval_seconds"`
	PollIntervalSeconds    int    `json:"poll_interval_seconds"`
	ObserveIntervalSeconds int    `json:"observe_interval_seconds"`
	MaxCountableGapSeconds int    `json:"max_countable_gap_seconds"`
	BatchSize              int    `json:"batch_size"`
	Locked                 bool   `json:"locked"`
	DebugMode              bool   `json:"debug_mode"`
}

func DefaultBaseDir() string {
	if home := os.Getenv("ROOTAIKA_HOME"); home != "" {
		return home
	}
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "rootaika")
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "rootaika")
	}
	return filepath.Join(".", "rootaika-data")
}

func DefaultPath() string {
	return filepath.Join(DefaultBaseDir(), "client.json")
}

func ResolvePath(path string) string {
	if path == "" {
		return DefaultPath()
	}
	return path
}

func LoadOrCreate(path string) (*Config, error) {
	path = ResolvePath(path)
	cfg := &Config{}
	exists := true

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		exists = false
	} else if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}

	changed, err := cfg.applyDefaults(path)
	if err != nil {
		return nil, err
	}
	cfg.ApplyEnvOverrides()

	if !exists || changed {
		if err := Save(path, cfg); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	path = ResolvePath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("ROOTAIKA_SERVER_URL"); v != "" {
		c.ServerURL = v
	}
	if v := os.Getenv("ROOTAIKA_CLIENT_USERNAME"); v != "" {
		c.ClientUsername = v
	}
	if v := os.Getenv("ROOTAIKA_CLIENT_PASSWORD"); v != "" {
		c.ClientPassword = v
	}
	if v := os.Getenv("ROOTAIKA_AGENT_LISTEN_ADDRESS"); v != "" {
		c.AgentListenAddress = v
	}
}

func (c *Config) ApplyServerConfig(sc model.ClientConfig) bool {
	changed := false
	if sc.IdleThresholdSeconds > 0 && c.IdleThresholdSeconds != sc.IdleThresholdSeconds {
		c.IdleThresholdSeconds = sc.IdleThresholdSeconds
		changed = true
	}
	if sc.UploadIntervalSeconds > 0 && c.UploadIntervalSeconds != sc.UploadIntervalSeconds {
		c.UploadIntervalSeconds = sc.UploadIntervalSeconds
		changed = true
	}
	if sc.PollIntervalSeconds > 0 && c.PollIntervalSeconds != sc.PollIntervalSeconds {
		c.PollIntervalSeconds = sc.PollIntervalSeconds
		changed = true
	}
	if sc.ObserveIntervalSeconds > 0 && c.ObserveIntervalSeconds != sc.ObserveIntervalSeconds {
		c.ObserveIntervalSeconds = sc.ObserveIntervalSeconds
		changed = true
	}
	if sc.MaxCountableGapSeconds > 0 && c.MaxCountableGapSeconds != sc.MaxCountableGapSeconds {
		c.MaxCountableGapSeconds = sc.MaxCountableGapSeconds
		changed = true
	}
	if sc.DebugMode != nil && c.DebugMode != *sc.DebugMode {
		c.DebugMode = *sc.DebugMode
		changed = true
	}
	return changed
}

func (c *Config) applyDefaults(path string) (bool, error) {
	changed := false
	baseDir := filepath.Dir(path)
	if c.ClientID == "" {
		c.ClientID = uuid.NewString()
		changed = true
	} else if _, err := uuid.Parse(c.ClientID); err != nil {
		return false, fmt.Errorf("invalid client_id %q: %w", c.ClientID, err)
	}
	if c.ServerURL == "" {
		c.ServerURL = defaultServerURL
		changed = true
	}
	if c.ClientUsername == "" {
		c.ClientUsername = defaultClientUsername
		changed = true
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(baseDir, "rootaika-client.db")
		changed = true
	}
	if c.AgentListenAddress == "" {
		c.AgentListenAddress = defaultAgentListenAddress
		changed = true
	}
	if c.AgentToken == "" {
		c.AgentToken = uuid.NewString()
		changed = true
	}
	if c.IdleThresholdSeconds <= 0 {
		c.IdleThresholdSeconds = defaultIdleThresholdSeconds
		changed = true
	}
	if c.UploadIntervalSeconds <= 0 {
		c.UploadIntervalSeconds = defaultUploadIntervalSeconds
		changed = true
	}
	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = defaultPollIntervalSeconds
		changed = true
	}
	if c.ObserveIntervalSeconds <= 0 {
		c.ObserveIntervalSeconds = defaultObserveIntervalSeconds
		changed = true
	}
	if c.MaxCountableGapSeconds <= 0 {
		c.MaxCountableGapSeconds = defaultMaxCountableGapSeconds
		changed = true
	}
	if c.BatchSize <= 0 {
		c.BatchSize = defaultBatchSize
		changed = true
	}
	return changed, nil
}
