//go:build !windows

package updater

import "fmt"

// ApplyUpdate is a no-op stub on non-Windows platforms. The OTA self-swap is a
// Windows-only feature; this exists so the combined binary's dispatcher and the
// package compile and test on the Linux dev/CI machine.
func ApplyUpdate(a ApplyArgs) error {
	return fmt.Errorf("apply-update is only supported on Windows")
}
