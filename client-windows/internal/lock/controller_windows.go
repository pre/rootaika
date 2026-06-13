//go:build windows

package lock

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// overlayScript renders a fullscreen topmost green overlay with a centered
// "rootaika" heading and the per-lock message below it. The message text is
// passed via the ROOTAIKA_LOCK_MESSAGE environment variable rather than being
// interpolated into the script, so arbitrary message content cannot break out
// of the PowerShell string or inject commands.
const overlayScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$form = New-Object System.Windows.Forms.Form
$form.WindowState = 'Maximized'
$form.FormBorderStyle = 'None'
$form.BackColor = [System.Drawing.Color]::FromArgb(22, 163, 74)
$form.TopMost = $true
$form.ShowInTaskbar = $false
$label = New-Object System.Windows.Forms.Label
$label.AutoSize = $false
$label.Dock = 'Fill'
$label.TextAlign = 'MiddleCenter'
$label.ForeColor = [System.Drawing.Color]::White
$label.Font = New-Object System.Drawing.Font('Segoe UI', 36, [System.Drawing.FontStyle]::Bold)
$nl = [Environment]::NewLine
$message = $env:ROOTAIKA_LOCK_MESSAGE
$label.Text = "rootaika" + $nl + $nl + $message
$form.Controls.Add($label)
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 500
$timer.Add_Tick({ $form.TopMost = $true; $form.Activate() })
$timer.Start()
[System.Windows.Forms.Cursor]::Hide()
[System.Windows.Forms.Application]::Run($form)
[System.Windows.Forms.Cursor]::Show()
`

type Controller struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	locked  bool
	message string
}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) SetLocked(ctx context.Context, locked bool, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if locked {
		// Re-render when already locked but the message changed, so an updated
		// lock command replaces the overlay text instead of leaving the old one.
		if c.locked && c.cmd != nil && c.message == message {
			return nil
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			c.cmd = nil
		}
		cmd := exec.CommandContext(ctx, "powershell.exe",
			"-NoProfile",
			"-ExecutionPolicy", "Bypass",
			"-WindowStyle", "Hidden",
			"-Command", overlayScript,
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Env = append(os.Environ(), "ROOTAIKA_LOCK_MESSAGE="+message)
		if err := cmd.Start(); err != nil {
			return err
		}
		c.cmd = cmd
		c.locked = true
		c.message = message
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
	c.message = ""
	return nil
}

func (c *Controller) Close() error {
	return c.SetLocked(context.Background(), false, "")
}
