//go:build windows

package updater

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ApplyUpdate runs as a detached, short-lived process launched from the staged
// exe (rootaika.update.exe). The running service cannot replace its own on-disk
// file, so this helper stops both processes, swaps the file, and restarts the
// service, whose watchdog then respawns the agent from the new exe. The helper's
// own image name differs from the target, so killing the agent does not kill it.
func ApplyUpdate(a ApplyArgs) error {
	log.Printf("apply-update: stopping service %s", a.Service)
	stopService(a.Service)
	waitServiceStopped(a.Service, 30*time.Second)

	log.Printf("apply-update: killing agent %s", a.AgentProcess)
	killImage(a.AgentProcess)

	oldPath := a.Target + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(a.Target, oldPath); err != nil {
		// A missing target is unexpected but not fatal: fall through to copy so a
		// previously failed swap can still recover.
		log.Printf("apply-update: rename %s failed: %v", a.Target, err)
	}

	if err := copyFile(a.Staged, a.Target); err != nil {
		// Restore the old binary so the service can still start.
		_ = os.Rename(oldPath, a.Target)
		startService(a.Service)
		return fmt.Errorf("copy staged exe: %w", err)
	}

	log.Printf("apply-update: starting service %s", a.Service)
	startService(a.Service)

	// Best-effort cleanup; the file may still be locked briefly by the OS.
	_ = os.Remove(oldPath)
	_ = os.Remove(a.Staged)
	log.Printf("apply-update: done")
	return nil
}

func stopService(name string) {
	_ = exec.Command("sc", "stop", name).Run()
}

func startService(name string) {
	_ = exec.Command("sc", "start", name).Run()
}

// waitServiceStopped polls `sc query` until the service reports STOPPED or the
// timeout elapses, so the swap does not race a still-running service holding the
// exe open.
func waitServiceStopped(name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("sc", "query", name).CombinedOutput()
		if err == nil && strings.Contains(string(out), "STOPPED") {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// killImage force-terminates every process with the given image name. The agent
// runs from rootaika.exe; this helper runs from rootaika.update.exe, so it is not
// affected. The service is already stopped by the time this runs.
func killImage(image string) {
	_ = exec.Command("taskkill", "/F", "/IM", image).Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
