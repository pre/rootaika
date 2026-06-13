//go:build !windows

package lock

import (
	"context"
	"testing"
)

func TestControllerNonWindowsNoOps(t *testing.T) {
	ctx := context.Background()
	c := NewController()
	if c == nil {
		t.Fatalf("NewController returned nil")
	}
	if err := c.SetLocked(ctx, true, "test message"); err != nil {
		t.Fatalf("SetLocked(true): %v", err)
	}
	if err := c.SetLocked(ctx, false, ""); err != nil {
		t.Fatalf("SetLocked(false): %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
