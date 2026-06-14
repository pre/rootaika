package rootaika

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// maxWarningSoundBytes caps the uploadable warning MP3 so an oversized file
// cannot exhaust disk or memory. 10 MB is generous for a short loop clip.
const maxWarningSoundBytes = 10 << 20

// warningSoundFileName is the fixed on-disk name of the single warning sound the
// server serves to every client.
const warningSoundFileName = "warning.mp3"

// warningSoundStore persists the single admin-uploaded lock-warning MP3 on the
// filesystem and derives a content version from the file's size and mtime so
// clients can tell when a new file was uploaded without hashing the bytes. A
// store with an empty dir is disabled: it reports no sound and rejects saves,
// which keeps NewApp(store) usable in tests that never set a data directory.
type warningSoundStore struct {
	dir string
}

func newWarningSoundStore(dir string) *warningSoundStore {
	return &warningSoundStore{dir: dir}
}

func (s *warningSoundStore) enabled() bool {
	return s != nil && s.dir != ""
}

func (s *warningSoundStore) path() string {
	if !s.enabled() {
		return ""
	}
	return filepath.Join(s.dir, warningSoundFileName)
}

// version returns "" when no sound is present, otherwise a "<size>-<mtimeUnix>"
// fingerprint that changes whenever a new file is uploaded.
func (s *warningSoundStore) version() string {
	if !s.enabled() {
		return ""
	}
	info, err := os.Stat(s.path())
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().Unix())
}

// save atomically writes the uploaded MP3, rejecting anything over the size cap.
// It writes to a temp file in the same dir and renames so a partial upload never
// replaces a good file.
func (s *warningSoundStore) save(r io.Reader) error {
	if !s.enabled() {
		return fmt.Errorf("warning sound storage is not configured")
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, warningSoundFileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	limited := io.LimitReader(r, maxWarningSoundBytes+1)
	written, err := io.Copy(tmp, limited)
	if err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if written == 0 {
		return fmt.Errorf("uploaded file is empty")
	}
	if written > maxWarningSoundBytes {
		return fmt.Errorf("uploaded file exceeds %d bytes", maxWarningSoundBytes)
	}
	return os.Rename(tmpName, s.path())
}
