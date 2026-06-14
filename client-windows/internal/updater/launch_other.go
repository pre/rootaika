//go:build !windows

package updater

import "os/exec"

// LaunchDetached starts exe with args without the Windows detached-process
// flags, which is enough for the non-Windows dev/CI path where OTA is not used.
func LaunchDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
