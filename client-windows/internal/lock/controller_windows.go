//go:build windows

package lock

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
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

// warnOverlayScript renders a click-through, non-activating countdown overlay
// that floats on top of a running game without stealing focus or input, so the
// player can keep playing until the timer reaches zero. Unlike the lock overlay
// it is layered + transparent + noactivate, and it self-closes when the counter
// hits zero. The total seconds and lock message arrive via environment
// variables (same injection-safe pattern as the lock overlay).
const warnOverlayScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class RootaikaWarnNative {
  [DllImport("user32.dll", SetLastError=true)]
  public static extern int GetWindowLong(IntPtr hWnd, int nIndex);
  [DllImport("user32.dll", SetLastError=true)]
  public static extern int SetWindowLong(IntPtr hWnd, int nIndex, int dwNewLong);
}
"@
$total = [int]$env:ROOTAIKA_WARN_SECONDS
if ($total -le 0) { $total = 1 }
$message = $env:ROOTAIKA_LOCK_MESSAGE
$script:remaining = $total
function Format-Remaining([int]$s) {
  if ($s -gt 60) {
    $m = [math]::Ceiling($s / 60.0)
    if ($m -eq 1) { return "1 minuutti jaljella ennen lukitusta" }
    return "$m minuuttia jaljella ennen lukitusta"
  }
  if ($s -eq 1) { return "1 sekunti jaljella ennen lukitusta" }
  return "$s sekuntia jaljella ennen lukitusta"
}
$form = New-Object System.Windows.Forms.Form
$form.FormBorderStyle = 'None'
$form.StartPosition = 'Manual'
$form.ShowInTaskbar = $false
$form.TopMost = $true
# Whole-window translucency so the game behind stays visible. Opacity 0.35 =
# 35% opaque / 65% see-through. A short bar covers as little of the game as
# possible while staying readable.
$form.Opacity = 0.35
$form.BackColor = [System.Drawing.Color]::FromArgb(138, 90, 0)
$screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$form.Width = $screen.Width
$form.Height = 90
$form.Left = 0
$form.Top = 40
$label = New-Object System.Windows.Forms.Label
$label.AutoSize = $false
$label.Dock = 'Fill'
$label.TextAlign = 'MiddleCenter'
$label.ForeColor = [System.Drawing.Color]::White
$label.BackColor = [System.Drawing.Color]::Transparent
$label.Font = New-Object System.Drawing.Font('Segoe UI', 22, [System.Drawing.FontStyle]::Bold)
$nl = [Environment]::NewLine
$label.Text = (Format-Remaining $script:remaining) + $nl + $message
$form.Controls.Add($label)
# GWL_EXSTYLE = -20. WS_EX_LAYERED=0x80000, WS_EX_TRANSPARENT=0x20 (click-through),
# WS_EX_NOACTIVATE=0x8000000, WS_EX_TOOLWINDOW=0x80 (hidden from Alt+Tab),
# WS_EX_TOPMOST=0x8. The game keeps focus and receives all mouse/keyboard input.
$form.Add_HandleCreated({
    $ex = [RootaikaWarnNative]::GetWindowLong($form.Handle, -20)
    $ex = $ex -bor 0x80000 -bor 0x20 -bor 0x8000000 -bor 0x80 -bor 0x8
    [void][RootaikaWarnNative]::SetWindowLong($form.Handle, -20, $ex)
})
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 1000
$timer.Add_Tick({
    $script:remaining -= 1
    if ($script:remaining -le 0) { $timer.Stop(); $form.Close(); return }
    $label.Text = (Format-Remaining $script:remaining) + $nl + $message
    $form.TopMost = $true
})
$timer.Start()
[System.Windows.Forms.Application]::Run($form)
`

// sayScript speaks the text in ROOTAIKA_SAY_TEXT once, preferring an installed
// Finnish (fi-FI) voice and falling back to the default voice. The text travels
// in an environment variable so message content cannot break out of the script.
const sayScript = `
Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
try {
  $fi = $s.GetInstalledVoices() |
        Where-Object { $_.Enabled -and $_.VoiceInfo.Culture.Name -eq 'fi-FI' } |
        Select-Object -First 1
  if ($fi) { $s.SelectVoice($fi.VoiceInfo.Name) }
} catch { }
$s.Speak($env:ROOTAIKA_SAY_TEXT)
`

// Warn runs the pre-lock warning: it shows a click-through countdown overlay and
// speaks a Finnish time-remaining reminder, repeating the reminder on a cadence
// that tightens as the deadline nears (every 60s above a minute, every 10s in
// the final minute). It blocks until the countdown elapses or ctx is cancelled
// (an unlock during the warning), then removes the overlay.
func (c *Controller) Warn(ctx context.Context, message string, seconds int) error {
	if seconds <= 0 {
		return nil
	}

	overlay := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-Command", warnOverlayScript,
	)
	overlay.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	overlay.Env = append(os.Environ(),
		"ROOTAIKA_WARN_SECONDS="+strconv.Itoa(seconds),
		"ROOTAIKA_LOCK_MESSAGE="+message,
	)
	if err := overlay.Start(); err != nil {
		return err
	}
	defer func() {
		if overlay.Process != nil {
			_ = overlay.Process.Kill()
		}
	}()

	// Drive TTS from Go so the cadence matches speakSchedule exactly and so an
	// unlock (ctx cancel) silences further reminders immediately.
	marks := speakSchedule(seconds)
	start := time.Now()
	for _, remaining := range marks {
		elapsed := seconds - remaining
		target := start.Add(time.Duration(elapsed) * time.Second)
		if wait := time.Until(target); wait > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
		}
		c.speak(ctx, countdownPhrase(remaining, message))
	}

	// Hold until the full countdown elapses (or unlock) before returning, so the
	// caller engages the lock overlay only when the warning is truly over.
	if wait := time.Until(start.Add(time.Duration(seconds) * time.Second)); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return nil
}

// speak fires a one-shot TTS process for text and waits for it to finish so
// reminders do not overlap. Failures are swallowed: TTS is best-effort and must
// never block the lock from proceeding.
func (c *Controller) speak(ctx context.Context, text string) {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-Command", sayScript,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(), "ROOTAIKA_SAY_TEXT="+text)
	_ = cmd.Run()
}

func (c *Controller) Close() error {
	return c.SetLocked(context.Background(), false, "")
}
