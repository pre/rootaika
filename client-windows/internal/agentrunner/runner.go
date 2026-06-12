package agentrunner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

type Runner struct {
	Path       string
	ConfigPath string

	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan error
}

func (r *Runner) Ensure(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.done != nil {
		select {
		case err := <-r.done:
			if err != nil {
				log.Printf("agent exited: %v", err)
			}
			r.done = nil
			r.cmd = nil
		default:
			return nil
		}
	}

	path, err := r.resolvePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}

	args := []string{}
	if r.ConfigPath != "" {
		args = append(args, "-config", r.ConfigPath)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	r.cmd = cmd
	r.done = done
	log.Printf("agent started: %s", path)
	return nil
}

func (r *Runner) resolvePath() (string, error) {
	if r.Path != "" {
		return r.Path, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	name := "rootaika-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(exe), name)
	if path == "" {
		return "", fmt.Errorf("agent path is empty")
	}
	return path, nil
}
