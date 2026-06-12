package consolewin

// Controller shows or hides the process console window. The console is hidden
// by default and only made visible when the server enables debug mode, so a
// non-admin user does not normally see the agent.
type Controller interface {
	SetVisible(visible bool) error
	Close() error
}
