//go:build windows

package lock

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
)

const overlayScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$form = New-Object System.Windows.Forms.Form
$form.WindowState = 'Maximized'
$form.FormBorderStyle = 'None'
$form.BackColor = [System.Drawing.Color]::Black
$form.TopMost = $true
$form.ShowInTaskbar = $false
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 500
$timer.Add_Tick({ $form.TopMost = $true; $form.Activate() })
$timer.Start()
[System.Windows.Forms.Cursor]::Hide()
[System.Windows.Forms.Application]::Run($form)
[System.Windows.Forms.Cursor]::Show()
`

type Controller struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	locked bool
}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) SetLocked(ctx context.Context, locked bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if locked {
		if c.locked && c.cmd != nil {
			return nil
		}
		cmd := exec.CommandContext(ctx, "powershell.exe",
			"-NoProfile",
			"-ExecutionPolicy", "Bypass",
			"-WindowStyle", "Hidden",
			"-Command", overlayScript,
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		c.cmd = cmd
		c.locked = true
		go func() {
			_ = cmd.Wait()
		}()
		return nil
	}

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.cmd = nil
	c.locked = false
	return nil
}

func (c *Controller) Close() error {
	return c.SetLocked(context.Background(), false)
}
