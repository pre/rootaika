package serviceapp

import (
	"context"
	"os"
	"path/filepath"

	"rootaika/client-windows/internal/config"
)

// soundDownloader fetches the warning MP3 bytes from the server. *api.Client
// satisfies it; tests supply a stub.
type soundDownloader interface {
	DownloadWarningSound(ctx context.Context) ([]byte, error)
}

// syncWarningSound reconciles the locally cached MP3 with the server's reported
// version. When the server version differs from the cached one it downloads and
// atomically replaces the file, then records the new version. An empty server
// version means the admin removed/never set a sound: the cache is cleared so the
// agent falls back to silence. It returns whether the cached version changed (so
// the caller persists config) and any error; on download error the cache is left
// untouched so a transient failure does not lose a working sound.
func syncWarningSound(ctx context.Context, dl soundDownloader, cached *config.Config, configPath, serverVersion string) (bool, error) {
	if cached.WarningSoundVersion == serverVersion {
		return false, nil
	}
	soundPath := cached.WarningSoundPath(configPath)

	if serverVersion == "" {
		_ = os.Remove(soundPath)
		cached.WarningSoundVersion = ""
		return true, nil
	}

	data, err := dl.DownloadWarningSound(ctx)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(soundPath, data); err != nil {
		return false, err
	}
	cached.WarningSoundVersion = serverVersion
	return true, nil
}

// writeFileAtomic writes data to a temp file in the destination dir and renames
// it over the target, so a crash mid-write never leaves a truncated MP3.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// cachedWarningSoundPath returns the local MP3 path when a sound is cached, else
// "". The agent reports this to its overlay so an absent sound means silence.
func cachedWarningSoundPath(cfg config.Config, configPath string) string {
	if cfg.WarningSoundVersion == "" {
		return ""
	}
	path := cfg.WarningSoundPath(configPath)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
