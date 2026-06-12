//go:build !windows

package consolewin

import "sync"

type stubController struct {
	mu      sync.Mutex
	visible bool
}

// New returns a no-op controller on non-Windows platforms. It still records the
// requested visibility so the agent loop and tests can exercise the logic.
func New() Controller {
	return &stubController{}
}

func (c *stubController) SetVisible(visible bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.visible = visible
	return nil
}

func (c *stubController) Close() error {
	return c.SetVisible(false)
}

// Visible reports the last requested visibility. Exposed for tests.
func (c *stubController) Visible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.visible
}
