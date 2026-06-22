//go:build windows

package lock

import (
	"context"
	"errors"
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
$debugShutdownAllowed = $env:ROOTAIKA_LOCK_DEBUG_SHUTDOWN -eq '1'
$script:allowClose = $false
$script:debugShutdown = $false
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
if ($debugShutdownAllowed) {
    $button = New-Object System.Windows.Forms.Button
    $button.Text = 'Sammuta client'
    $button.Width = 170
    $button.Height = 44
    $button.Anchor = [System.Windows.Forms.AnchorStyles]::Right -bor [System.Windows.Forms.AnchorStyles]::Bottom
    $button.Font = New-Object System.Drawing.Font('Segoe UI', 11, [System.Drawing.FontStyle]::Bold)
    $button.Left = $form.ClientSize.Width - $button.Width - 32
    $button.Top = $form.ClientSize.Height - $button.Height - 28
    $button.Add_Click({
        $script:debugShutdown = $true
        $script:allowClose = $true
        $timer.Stop()
        $form.Close()
    })
    $form.Add_Resize({
        $button.Left = $form.ClientSize.Width - $button.Width - 32
        $button.Top = $form.ClientSize.Height - $button.Height - 28
    })
    $form.Controls.Add($button)
    $button.BringToFront()
}
# GWL_EXSTYLE = -20, WS_EX_TOOLWINDOW = 0x80. A tool window is omitted from the
# Alt+Tab list, so the user cannot switch to and close the overlay that way.
$form.Add_HandleCreated({
    $exStyle = [RootaikaNative]::GetWindowLong($form.Handle, -20)
    [void][RootaikaNative]::SetWindowLong($form.Handle, -20, $exStyle -bor 0x80)
})
# Block every user-initiated close (Alt+F4, window menu). Process.Kill still ends it.
$form.Add_FormClosing({ param($sender, $e) if (-not $script:allowClose) { $e.Cancel = $true } })
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 500
$timer.Add_Tick({ $form.TopMost = $true; $form.Activate() })
$timer.Start()
if (-not $debugShutdownAllowed) { [System.Windows.Forms.Cursor]::Hide() }
[System.Windows.Forms.Application]::Run($form)
if (-not $debugShutdownAllowed) { [System.Windows.Forms.Cursor]::Show() }
if ($script:debugShutdown) { exit 42 }
`

const debugShutdownExitCode = 42
const createNoWindow = 0x08000000

type Controller struct {
	mu                   sync.Mutex
	cmd                  *exec.Cmd
	locked               bool
	message              string
	debugShutdownAllowed bool
	// exited reports that the overlay process for the current lock died on its
	// own (the user killed it, it crashed). SetLocked relaunches when this is set
	// and the device is still locked, so a dismissed overlay reappears.
	exited        bool
	debugShutdown chan struct{}
}

func NewController() *Controller {
	return &Controller{debugShutdown: make(chan struct{}, 1)}
}

func (c *Controller) DebugShutdown() <-chan struct{} {
	return c.debugShutdown
}

func (c *Controller) SetLocked(ctx context.Context, locked bool, message string, debugShutdownAllowed bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if locked {
		// The overlay is up to date only when it is still running (not exited) and
		// shows the current message. Otherwise re-render: a changed message or a
		// dead overlay both fall through to a fresh launch.
		if c.locked && c.cmd != nil && !c.exited && c.message == message && c.debugShutdownAllowed == debugShutdownAllowed {
			return nil
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
			c.cmd = nil
		}
		cmd := powerShellCommand(ctx, overlayScript)
		cmd.Env = append(os.Environ(),
			"ROOTAIKA_LOCK_MESSAGE="+message,
			"ROOTAIKA_LOCK_DEBUG_SHUTDOWN="+boolEnv(debugShutdownAllowed),
		)
		if err := cmd.Start(); err != nil {
			return err
		}
		c.cmd = cmd
		c.locked = true
		c.message = message
		c.debugShutdownAllowed = debugShutdownAllowed
		c.exited = false
		go func() {
			err := cmd.Wait()
			debugShutdown := commandExitCode(err) == debugShutdownExitCode
			c.mu.Lock()
			// Only flag exit if this is still the active overlay; an unlock or a
			// relaunch swaps c.cmd and must not be marked as a spurious exit.
			if c.cmd == cmd {
				if debugShutdown {
					c.signalDebugShutdown()
				}
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
	c.debugShutdownAllowed = false
	c.exited = false
	return nil
}

func (c *Controller) signalDebugShutdown() {
	select {
	case c.debugShutdown <- struct{}{}:
	default:
	}
}

func boolEnv(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
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
$script:completed = $false
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
    if ($script:remaining -le 0) { $script:completed = $true; $timer.Stop(); $form.Close(); return }
    $label.Text = (Format-Remaining $script:remaining) + $nl + $message
    $form.TopMost = $true
})
$timer.Start()
[System.Windows.Forms.Application]::Run($form)
if (-not $script:completed) { exit 43 }
`

// playLoopScript plays the MP3 at ROOTAIKA_SOUND_PATH on repeat until the
// process is killed. It uses the Windows Media Player COM object (WMPlayer.OCX),
// which is present on every desktop Windows install and decodes MP3 without any
// extra dependency. The path travels in an environment variable so it cannot
// break out of the script. The script idles in a sleep loop; killing the process
// (when the countdown ends or an unlock cancels the warning) stops playback.
const playLoopScript = `
$path = $env:ROOTAIKA_SOUND_PATH
if (-not (Test-Path -LiteralPath $path)) { exit 0 }
Add-Type -AssemblyName PresentationCore
$player = New-Object System.Windows.Media.MediaPlayer
$dispatcher = [System.Windows.Threading.Dispatcher]::CurrentDispatcher
$player.Volume = 1.0
$player.add_MediaEnded({
    $player.Position = [TimeSpan]::Zero
    $player.Play()
})
$player.add_MediaFailed({
    $dispatcher.InvokeShutdown()
})
$player.Open([System.Uri]::new((Resolve-Path -LiteralPath $path).Path))
$player.Play()
[System.Windows.Threading.Dispatcher]::Run()
`

// Warn runs the pre-lock warning: it shows a click-through countdown overlay and,
// when a warning sound is cached, loops that MP3 for the duration. It blocks
// until the countdown elapses or ctx is cancelled (an unlock during the
// warning), then removes the overlay and stops the sound. A missing or empty
// soundPath means no audio is played, which is a normal, non-fatal state.
func (c *Controller) Warn(ctx context.Context, message string, seconds int, soundPath string) error {
	if seconds <= 0 {
		return nil
	}

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		remaining := secondsUntil(deadline)
		if remaining <= 0 {
			return nil
		}
		overlay, err := c.startWarnOverlay(ctx, message, remaining)
		if err != nil {
			return err
		}
		exited := make(chan struct{})
		go func() {
			_ = overlay.Wait()
			close(exited)
		}()

		// Start audio after the overlay process is launched and keep it tied to
		// the overlay's own countdown, not to a separate Go timer that can get
		// ahead of PowerShell/WinForms startup.
		stopSound := c.playLoop(ctx, soundPath)
		select {
		case <-ctx.Done():
			if stopSound != nil {
				stopSound()
			}
			killAndWait(overlay, exited)
			return nil
		case <-exited:
			if stopSound != nil {
				stopSound()
			}
			if overlay.ProcessState != nil && overlay.ProcessState.ExitCode() == 0 {
				return nil
			}
			if !pause(ctx, 200*time.Millisecond) {
				return nil
			}
		}
	}
}

func (c *Controller) startWarnOverlay(ctx context.Context, message string, seconds int) (*exec.Cmd, error) {
	overlay := powerShellCommand(ctx, warnOverlayScript)
	overlay.Env = append(os.Environ(),
		"ROOTAIKA_WARN_SECONDS="+strconv.Itoa(seconds),
		"ROOTAIKA_LOCK_MESSAGE="+message,
	)
	if err := overlay.Start(); err != nil {
		return nil, err
	}
	return overlay, nil
}

// playLoop starts a background process that loops soundPath and returns a stop
// function that kills it. It returns nil when there is nothing to play (empty
// path) or the process could not start, so the caller can safely skip stopping.
func (c *Controller) playLoop(ctx context.Context, soundPath string) func() {
	if soundPath == "" {
		return nil
	}
	cmd := powerShellCommand(ctx, playLoopScript)
	cmd.Env = append(os.Environ(), "ROOTAIKA_SOUND_PATH="+soundPath)
	if err := cmd.Start(); err != nil {
		return nil
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

func powerShellCommand(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-STA",
		"-Command", script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:    createNoWindow,
		NoInheritHandles: true,
	}
	return cmd
}

func killAndWait(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	<-exited
}

func secondsUntil(deadline time.Time) int {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	return int((remaining + time.Second - time.Nanosecond) / time.Second)
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Controller) Close() error {
	return c.SetLocked(context.Background(), false, "", false)
}
