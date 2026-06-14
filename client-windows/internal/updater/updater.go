// Package updater downloads and applies over-the-air updates of the combined
// rootaika client binary. The pure parts (NeedsUpdate, Download) are testable on
// any platform; the self-swap helper is split into apply_windows.go and
// apply_other.go.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"rootaika/client-windows/internal/version"
)

// Plan is the server-declared update target: which release tag to fetch, which
// asset within it, and the expected SHA256 of that asset. An empty Version means
// no update is desired.
type Plan struct {
	Version  string
	Artifact string
	SHA256   string
}

// retry policy for the download. Matches the api client stance: retry network
// and 5xx, never 4xx, and a SHA256 mismatch is terminal (handled in Download).
const (
	maxAttempts = 4
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 5 * time.Second
)

// NeedsUpdate reports whether the plan asks for a version different from the one
// currently running. Comparison is plain string inequality (no semver ordering),
// so any difference, including a downgrade, triggers an update. An empty desired
// version or an incomplete plan never triggers.
func NeedsUpdate(current string, p Plan) bool {
	if p.Version == "" || p.Artifact == "" || p.SHA256 == "" {
		return false
	}
	return p.Version != current
}

// gitHubBase is the public release-download host. It is a var only so tests can
// point Download at an httptest server; production never overrides it, and the
// owner/repo path segments remain compile-time constants.
const gitHubBase = "https://github.com"

// Download streams the plan's release asset to destPath and verifies its SHA256.
// On any error, including a hash mismatch, destPath is removed and no usable file
// is left behind. The URL is built only from the compile-time-fixed owner/repo
// plus the plan, so the server can never redirect the download elsewhere.
func Download(ctx context.Context, p Plan, destPath string) error {
	return downloadFrom(ctx, gitHubBase, p, destPath)
}

// downloadFrom is Download with an injectable base host for testing. The path is
// always {owner}/{repo}/releases/download/{tag}/{asset}, matching
// version.DownloadURL, so the production call and the test exercise the same
// path construction.
func downloadFrom(ctx context.Context, base string, p Plan, destPath string) error {
	if p.Version == "" || p.Artifact == "" || p.SHA256 == "" {
		return fmt.Errorf("incomplete update plan: %+v", p)
	}
	url := base + "/" + version.GitHubOwner + "/" + version.GitHubRepo +
		"/releases/download/" + p.Version + "/" + p.Artifact

	body, err := fetch(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()

	if err := streamToFile(body, destPath, p.SHA256); err != nil {
		_ = os.Remove(destPath)
		return err
	}
	return nil
}

// fetch performs the GET with retry/backoff, returning the response body on a
// 2xx. The caller closes the body. Network errors and 5xx/429 are retried; 4xx
// is terminal.
func fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			lastErr = err
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.Body, nil
		}
		resp.Body.Close()
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		lastErr = fmt.Errorf("download %s failed with %s", url, resp.Status)
		if !retryable {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("download failed after %d attempts: %w", maxAttempts, lastErr)
}

// streamToFile copies src to destPath while hashing, then fails if the hash does
// not match wantHex (case-insensitive). The file is synced before returning so
// the subsequent swap sees the full contents.
func streamToFile(src io.Reader, destPath, wantHex string) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if _, err := io.Copy(out, io.TeeReader(src, hasher)); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(wantHex)) {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, wantHex)
	}
	return nil
}

func backoff(attempt int) time.Duration {
	delay := baseBackoff << (attempt - 1)
	if delay > maxBackoff {
		delay = maxBackoff
	}
	return delay
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
