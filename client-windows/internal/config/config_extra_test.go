package config

import (
	"path/filepath"
	"testing"

	"rootaika/client-windows/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestApplyServerConfigDebugMode(t *testing.T) {
	t.Run("nil leaves unchanged", func(t *testing.T) {
		cfg := &Config{DebugMode: true}
		if changed := cfg.ApplyServerConfig(model.ClientConfig{}); changed {
			t.Fatalf("nil DebugMode should not report change")
		}
		if !cfg.DebugMode {
			t.Fatalf("nil DebugMode must not modify the value")
		}
	})
	t.Run("true sets", func(t *testing.T) {
		cfg := &Config{DebugMode: false}
		if changed := cfg.ApplyServerConfig(model.ClientConfig{DebugMode: boolPtr(true)}); !changed {
			t.Fatalf("setting DebugMode true should report change")
		}
		if !cfg.DebugMode {
			t.Fatalf("DebugMode was not set to true")
		}
	})
	t.Run("false clears", func(t *testing.T) {
		cfg := &Config{DebugMode: true}
		if changed := cfg.ApplyServerConfig(model.ClientConfig{DebugMode: boolPtr(false)}); !changed {
			t.Fatalf("clearing DebugMode should report change")
		}
		if cfg.DebugMode {
			t.Fatalf("DebugMode was not cleared")
		}
	})
	t.Run("same value no change", func(t *testing.T) {
		cfg := &Config{DebugMode: true}
		if changed := cfg.ApplyServerConfig(model.ClientConfig{DebugMode: boolPtr(true)}); changed {
			t.Fatalf("identical DebugMode should not report change")
		}
	})
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("ROOTAIKA_SERVER_URL", "http://override.test")
	t.Setenv("ROOTAIKA_CLIENT_USERNAME", "envuser")
	t.Setenv("ROOTAIKA_CLIENT_PASSWORD", "envpass")
	t.Setenv("ROOTAIKA_AGENT_LISTEN_ADDRESS", "127.0.0.1:1234")

	cfg := &Config{
		ServerURL:          "http://local",
		ClientUsername:     "local",
		ClientPassword:     "localpass",
		AgentListenAddress: "127.0.0.1:9999",
	}
	cfg.ApplyEnvOverrides()

	if cfg.ServerURL != "http://override.test" {
		t.Fatalf("server URL not overridden: %s", cfg.ServerURL)
	}
	if cfg.ClientUsername != "envuser" {
		t.Fatalf("username not overridden: %s", cfg.ClientUsername)
	}
	if cfg.ClientPassword != "envpass" {
		t.Fatalf("password not overridden: %s", cfg.ClientPassword)
	}
	if cfg.AgentListenAddress != "127.0.0.1:1234" {
		t.Fatalf("listen address not overridden: %s", cfg.AgentListenAddress)
	}
}

func TestApplyEnvOverridesEmptyDoesNotOverride(t *testing.T) {
	t.Setenv("ROOTAIKA_SERVER_URL", "")
	cfg := &Config{ServerURL: "http://local"}
	cfg.ApplyEnvOverrides()
	if cfg.ServerURL != "http://local" {
		t.Fatalf("empty env var should not override existing value")
	}
}

func TestApplyDefaultsInvalidClientID(t *testing.T) {
	cfg := &Config{ClientID: "not-a-uuid"}
	if _, err := cfg.applyDefaults(filepath.Join(t.TempDir(), "client.json")); err == nil {
		t.Fatalf("expected error for invalid client_id")
	}
}

func TestSaveLoadRoundTripPersistsDebugMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	cfg.DebugMode = true
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.DebugMode {
		t.Fatalf("DebugMode was not persisted across save/load")
	}
}

func TestResolveAndDefaultPathNonEmpty(t *testing.T) {
	if DefaultPath() == "" {
		t.Fatalf("DefaultPath should not be empty")
	}
	if ResolvePath("") != DefaultPath() {
		t.Fatalf("ResolvePath(\"\") should equal DefaultPath")
	}
	if got := ResolvePath("/tmp/custom.json"); got != "/tmp/custom.json" {
		t.Fatalf("ResolvePath should preserve explicit path, got %q", got)
	}
}
