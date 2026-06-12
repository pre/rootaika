//go:build !windows

package activity

import (
	"context"
	"testing"
)

func TestStubProbeSnapshot(t *testing.T) {
	snapshot, err := NewProbe().Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.ForegroundProcess != "nonwindows-stub" {
		t.Fatalf("unexpected foreground process: %q", snapshot.ForegroundProcess)
	}
	if snapshot.At.IsZero() {
		t.Fatalf("snapshot timestamp should not be zero")
	}
	if snapshot.IdleFor != 0 {
		t.Fatalf("stub IdleFor should be zero, got %v", snapshot.IdleFor)
	}
}
