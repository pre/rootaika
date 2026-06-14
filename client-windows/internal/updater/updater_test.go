package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"rootaika/client-windows/internal/version"
)

func TestNeedsUpdate(t *testing.T) {
	full := Plan{Version: "v2", Artifact: "rootaika.exe", SHA256: "abc"}
	cases := []struct {
		name    string
		current string
		plan    Plan
		want    bool
	}{
		{"empty desired", "v1", Plan{Artifact: "rootaika.exe", SHA256: "abc"}, false},
		{"missing artifact", "v1", Plan{Version: "v2", SHA256: "abc"}, false},
		{"missing sha", "v1", Plan{Version: "v2", Artifact: "rootaika.exe"}, false},
		{"equal", "v2", full, false},
		{"different", "v1", full, true},
		{"downgrade", "v3", full, true},
		{"current empty triggers", "", full, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsUpdate(tc.current, tc.plan); got != tc.want {
				t.Fatalf("NeedsUpdate(%q, %+v) = %v, want %v", tc.current, tc.plan, got, tc.want)
			}
		})
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newAssetServer serves payload only at the exact GitHub release asset path the
// updater builds from the fixed owner/repo plus the plan, so the test also pins
// the URL construction.
func newAssetServer(t *testing.T, tag, asset string, payload []byte) *httptest.Server {
	t.Helper()
	wantPath := "/" + version.GitHubOwner + "/" + version.GitHubRepo + "/releases/download/" + tag + "/" + asset
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
}

func TestDownloadSuccess(t *testing.T) {
	payload := []byte("MZ fake windows exe payload")
	srv := newAssetServer(t, "v2", "rootaika.exe", payload)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rootaika.update.exe")
	p := Plan{Version: "v2", Artifact: "rootaika.exe", SHA256: sha256Hex(payload)}

	if err := downloadFrom(context.Background(), srv.URL, p, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded bytes mismatch")
	}
}

func TestDownloadSHA256Mismatch(t *testing.T) {
	payload := []byte("real payload")
	srv := newAssetServer(t, "v2", "rootaika.exe", payload)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rootaika.update.exe")
	p := Plan{Version: "v2", Artifact: "rootaika.exe", SHA256: sha256Hex([]byte("different"))}

	if err := downloadFrom(context.Background(), srv.URL, p, dest); err == nil {
		t.Fatalf("expected sha256 mismatch error")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination must not survive a hash mismatch, stat err = %v", err)
	}
}

func TestDownloadNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rootaika.update.exe")
	p := Plan{Version: "v2", Artifact: "missing.exe", SHA256: "abc"}
	if err := downloadFrom(context.Background(), srv.URL, p, dest); err == nil {
		t.Fatalf("expected error on 404")
	}
}
