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
//
// The overlay is hardened so the user cannot dismiss it: WS_EX_TOOLWINDOW hides
// it from the Alt+Tab switcher, and FormClosing is cancelled so Alt+F4 and the
// window menu cannot close it. The service still closes it with Process.Kill,
// which bypasses FormClosing, so an unlock removes the overlay.
const overlayScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class RootaikaNative {
  [DllImport("user32.dll", SetLastError=true)]
  public static extern int GetWindowLong(IntPtr hWnd, int nIndex);
  [DllImport("user32.dll", SetLastError=true)]
  public static extern int SetWindowLong(IntPtr hWnd, int nIndex, int dwNewLong);
}
"@
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
# GWL_EXSTYLE = -20, WS_EX_TOOLWINDOW = 0x80. A tool window is omitted from the
# Alt+Tab list, so the user cannot switch to and close the overlay that way.
$form.Add_HandleCreated({
    $exStyle = [RootaikaNative]::GetWindowLong($form.Handle, -20)
    [void][RootaikaNative]::SetWindowLong($form.Handle, -20, $exStyle -bor 0x80)
})
# Block every user-initiated close (Alt+F4, window menu). Process.Kill still ends it.
$form.Add_FormClosing({ param($sender, $e) $e.Cancel = $true })
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
	// exited reports that the overlay process for the current lock died on its
	// own (the user killed it, it crashed). SetLocked relaunches when this is set
	// and the device is still locked, so a dismissed overlay reappears.
	exited bool
}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) SetLocked(ctx context.Context, locked bool, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if locked {
		// The overlay is up to date only when it is still running (not exited) and
		// shows the current message. Otherwise re-render: a changed message or a
		// dead overlay both fall through to a fresh launch.
		if c.locked && c.cmd != nil && !c.exited && c.message == message {
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
		c.exited = false
		go func() {
			_ = cmd.Wait()
			c.mu.Lock()
			// Only flag exit if this is still the active overlay; an unlock or a
			// relaunch swaps c.cmd and must not be marked as a spurious exit.
			if c.cmd == cmd {
				c.exited = true
			}
			c.mu.Unlock()
		}()
		return nil
	}

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.cmd = nil
	c.locked = false
	c.message = ""
	c.exited = false
	return nil
}

func (c *Controller) Close() error {
	return c.SetLocked(context.Background(), false, "")
}
