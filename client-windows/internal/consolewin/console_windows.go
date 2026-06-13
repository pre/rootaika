//go:build windows

package consolewin

import (
	"log"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

const (
	swHide = 0
	swShow = 5
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	user32            = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWin = kernel32.NewProc("GetConsoleWindow")
	procShowWindow    = user32.NewProc("ShowWindow")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
)

type windowsController struct {
	mu      sync.Mutex
	visible bool
}

// New returns a console controller that hides the console immediately so the
// agent starts invisible regardless of how it was launched.
func New() Controller {
	c := &windowsController{visible: true}
	_ = c.SetVisible(false)
	return c
}

func (c *windowsController) SetVisible(visible bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if visible == c.visible {
		return nil
	}

	hwnd := consoleWindow()
	if hwnd == 0 && visible {
		// No console exists (e.g. launched with -H=windowsgui): allocate one
		// so debug output has somewhere to go.
		procAllocConsole.Call()
		hwnd = consoleWindow()
		// AllocConsole creates the window but leaves Go's std handles pointing at
		// the inherited (null) handles, so log output would go nowhere visible.
		// Rebind stdout/stderr to the new console buffer.
		redirectStdHandles()
	}
	if hwnd != 0 {
		cmd := uintptr(swHide)
		if visible {
			cmd = swShow
		}
		procShowWindow.Call(hwnd, cmd)
	}
	c.visible = visible
	return nil
}

func (c *windowsController) Close() error {
	return c.SetVisible(false)
}

func consoleWindow() uintptr {
	hwnd, _, _ := procGetConsoleWin.Call()
	return hwnd
}

// redirectStdHandles points os.Stdout/os.Stderr and the default logger at the
// freshly allocated console. Without this the agent (built with -H=windowsgui)
// writes to the inherited null handles and the debug window stays empty.
func redirectStdHandles() {
	conout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = conout
	os.Stderr = conout
	log.SetOutput(conout)
}
