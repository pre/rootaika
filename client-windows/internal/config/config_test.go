package config

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"rootaika/client-windows/internal/model"
)

func TestLoadOrCreateFillsPersistentDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")

	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if _, err := uuid.Parse(cfg.ClientID); err != nil {
		t.Fatalf("client id is not a UUID: %v", err)
	}
	if cfg.AgentToken == "" {
		t.Fatalf("agent token was not generated")
	}
	if cfg.DBPath != filepath.Join(filepath.Dir(path), "rootaika-client.db") {
		t.Fatalf("unexpected db path: %s", cfg.DBPath)
	}
	if cfg.IdleThresholdSeconds != defaultIdleThresholdSeconds {
		t.Fatalf("unexpected idle threshold: %d", cfg.IdleThresholdSeconds)
	}

	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.ClientID != cfg.ClientID {
		t.Fatalf("client id was not persisted")
	}
	if reloaded.AgentToken != cfg.AgentToken {
		t.Fatalf("agent token was not persisted")
	}
}

func TestApplyServerConfigOnlyUsesPositiveValues(t *testing.T) {
	cfg := &Config{
		IdleThresholdSeconds:   60,
		UploadIntervalSeconds:  60,
		PollIntervalSeconds:    30,
		ObserveIntervalSeconds: 5,
		MaxCountableGapSeconds: 300,
	}

	changed := cfg.ApplyServerConfig(model.ClientConfig{
		IdleThresholdSeconds:   45,
		UploadIntervalSeconds:  0,
		PollIntervalSeconds:    10,
		ObserveIntervalSeconds: 2,
		MaxCountableGapSeconds: 120,
	})
	if !changed {
		t.Fatalf("expected config to change")
	}
	if cfg.IdleThresholdSeconds != 45 {
		t.Fatalf("idle threshold was not updated")
	}
	if cfg.UploadIntervalSeconds != 60 {
		t.Fatalf("zero upload interval should not overwrite local value")
	}
	if cfg.PollIntervalSeconds != 10 || cfg.ObserveIntervalSeconds != 2 || cfg.MaxCountableGapSeconds != 120 {
		t.Fatalf("server config not applied: %+v", cfg)
	}
}
