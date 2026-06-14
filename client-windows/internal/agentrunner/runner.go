package agentrunner

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
)

type Runner struct {
	// Path is the combined rootaika exe to launch the agent from. When empty it
	// resolves to the currently running executable, so the service and agent
	// always come from the one on-disk file and a single OTA swap updates both.
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

	cmd := exec.CommandContext(ctx, path, r.args()...)
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

// resolvePath returns the combined exe to run. An explicit Path wins; otherwise
// the running executable is used so both processes share one file.
func (r *Runner) resolvePath() (string, error) {
	if r.Path != "" {
		return r.Path, nil
	}
	return os.Executable()
}

// args is the argument vector for the combined binary: the agent subcommand
// followed by the optional config path.
func (r *Runner) args() []string {
	args := []string{"agent"}
	if r.ConfigPath != "" {
		args = append(args, "-config", r.ConfigPath)
	}
	return args
}
