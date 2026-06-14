package agentrunner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolvePathExplicit(t *testing.T) {
	r := &Runner{Path: "/opt/custom/rootaika"}
	got, err := r.resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	if got != "/opt/custom/rootaika" {
		t.Fatalf("explicit path not returned, got %q", got)
	}
}

func TestResolvePathDerivesFromExecutable(t *testing.T) {
	r := &Runner{}
	got, err := r.resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if got != exe {
		t.Fatalf("derived path should be the running executable %q, got %q", exe, got)
	}
}

func TestArgsIncludeAgentSubcommand(t *testing.T) {
	if got := (&Runner{}).args(); !reflect.DeepEqual(got, []string{"agent"}) {
		t.Fatalf("args without config = %v, want [agent]", got)
	}
	r := &Runner{ConfigPath: "/etc/rootaika/client.json"}
	want := []string{"agent", "-config", "/etc/rootaika/client.json"}
	if got := r.args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("args with config = %v, want %v", got, want)
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
