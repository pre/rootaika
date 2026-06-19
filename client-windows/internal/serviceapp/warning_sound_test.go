package serviceapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"rootaika/client-windows/internal/config"
)

type stubDownloader struct {
	data  []byte
	err   error
	calls int
}

func (s *stubDownloader) DownloadWarningSound(context.Context) ([]byte, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.data, nil
}

func TestSyncWarningSoundDownloadsOnVersionChange(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{}
	dl := &stubDownloader{data: []byte("ID3 mp3 loop")}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "12-345")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first download")
	}
	if cfg.WarningSoundVersion != "12-345" {
		t.Fatalf("version = %q, want 12-345", cfg.WarningSoundVersion)
	}
	got, err := os.ReadFile(cfg.WarningSoundPath(cfgPath))
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(got) != "ID3 mp3 loop" {
		t.Fatalf("cached bytes = %q", got)
	}
}

func TestSyncWarningSoundNoOpWhenVersionMatches(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{WarningSoundVersion: "12-345"}
	if err := os.WriteFile(cfg.WarningSoundPath(cfgPath), []byte("123456789012"), 0o600); err != nil {
		t.Fatalf("seed sound: %v", err)
	}
	dl := &stubDownloader{data: []byte("should not be fetched")}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "12-345")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when versions match")
	}
	if dl.calls != 0 {
		t.Fatalf("downloader called %d times, want 0", dl.calls)
	}
}

func TestSyncWarningSoundRedownloadsWhenVersionMatchesButCacheMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{WarningSoundVersion: "12-345"}
	dl := &stubDownloader{data: []byte("fresh sound")}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "12-345")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when cache file is missing")
	}
	if dl.calls != 1 {
		t.Fatalf("downloader called %d times, want 1", dl.calls)
	}
	got, err := os.ReadFile(cfg.WarningSoundPath(cfgPath))
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(got) != "fresh sound" {
		t.Fatalf("cached bytes = %q", got)
	}
}

func TestSyncWarningSoundRedownloadsWhenVersionMatchesButCacheSizeDiffers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{WarningSoundVersion: "20-345"}
	if err := os.WriteFile(cfg.WarningSoundPath(cfgPath), []byte("truncated"), 0o600); err != nil {
		t.Fatalf("seed sound: %v", err)
	}
	dl := &stubDownloader{data: []byte("fresh full sound")}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "20-345")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when cache size differs")
	}
	if dl.calls != 1 {
		t.Fatalf("downloader called %d times, want 1", dl.calls)
	}
	got, err := os.ReadFile(cfg.WarningSoundPath(cfgPath))
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(got) != "fresh full sound" {
		t.Fatalf("cached bytes = %q", got)
	}
}

func TestSyncWarningSoundClearsWhenServerHasNoSound(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{WarningSoundVersion: "12-345"}
	soundPath := cfg.WarningSoundPath(cfgPath)
	if err := os.WriteFile(soundPath, []byte("old sound"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	dl := &stubDownloader{}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when clearing")
	}
	if cfg.WarningSoundVersion != "" {
		t.Fatalf("version = %q, want empty", cfg.WarningSoundVersion)
	}
	if _, err := os.Stat(soundPath); !os.IsNotExist(err) {
		t.Fatalf("cached file should be removed, stat err = %v", err)
	}
	if dl.calls != 0 {
		t.Fatalf("downloader called %d times, want 0", dl.calls)
	}
}

func TestSyncWarningSoundKeepsCacheOnDownloadError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")
	cfg := &config.Config{WarningSoundVersion: "old"}
	soundPath := cfg.WarningSoundPath(cfgPath)
	if err := os.WriteFile(soundPath, []byte("good sound"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	dl := &stubDownloader{err: errors.New("network down")}

	changed, err := syncWarningSound(context.Background(), dl, cfg, cfgPath, "new")
	if err == nil {
		t.Fatal("expected error on download failure")
	}
	if changed {
		t.Fatal("expected changed=false on failure")
	}
	if cfg.WarningSoundVersion != "old" {
		t.Fatalf("version changed to %q on failure", cfg.WarningSoundVersion)
	}
	if got, _ := os.ReadFile(soundPath); string(got) != "good sound" {
		t.Fatalf("cached file altered on failure: %q", got)
	}
}

func TestCachedWarningSoundPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.json")

	// No version => no path.
	if p := cachedWarningSoundPath(config.Config{}, cfgPath); p != "" {
		t.Fatalf("path with no version = %q, want empty", p)
	}

	// Version set but file missing => no path.
	cfg := config.Config{WarningSoundVersion: "12-345"}
	if p := cachedWarningSoundPath(cfg, cfgPath); p != "" {
		t.Fatalf("path with missing file = %q, want empty", p)
	}

	// Version set and file present => returns the path.
	soundPath := cfg.WarningSoundPath(cfgPath)
	if err := os.WriteFile(soundPath, []byte("sound"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if p := cachedWarningSoundPath(cfg, cfgPath); p != soundPath {
		t.Fatalf("path = %q, want %q", p, soundPath)
	}
}
