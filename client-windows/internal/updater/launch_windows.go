//go:build windows

package updater

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// LaunchDetached starts exe with args as a fully detached process that outlives
// the caller. The service launches the staged apply-update helper this way so
// the helper can stop the service and swap the file after the service exits.
func LaunchDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release so the helper is not tied to this process's lifetime.
	return cmd.Process.Release()
}
