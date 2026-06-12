package agentrunner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathExplicit(t *testing.T) {
	r := &Runner{Path: "/opt/custom/agent"}
	got, err := r.resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	if got != "/opt/custom/agent" {
		t.Fatalf("explicit path not returned, got %q", got)
	}
}

func TestResolvePathDerivesFromExecutable(t *testing.T) {
	r := &Runner{}
	got, err := r.resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	if got == "" {
		t.Fatalf("derived path should not be empty")
	}
	if base := filepath.Base(got); base != "rootaika-agent" {
		t.Fatalf("derived path should end with rootaika-agent on linux, got %q", base)
	}
}

func TestEnsureMissingPathReturnsStatError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	r := &Runner{Path: missing}
	err := r.Ensure(context.Background())
	if err == nil {
		t.Fatalf("expected stat error for missing agent binary")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error should reference the missing path, got %v", err)
	}
}
