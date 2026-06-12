//go:build !windows

package servicehost

import (
	"context"
	"errors"
	"testing"
)

func TestRunInvokesRunFunc(t *testing.T) {
	called := false
	err := Run(context.Background(), "svc", func(context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !called {
		t.Fatalf("run func was not invoked")
	}

	want := errors.New("boom")
	got := Run(context.Background(), "svc", func(context.Context) error {
		return want
	})
	if !errors.Is(got, want) {
		t.Fatalf("Run did not propagate error, got %v", got)
	}
}
