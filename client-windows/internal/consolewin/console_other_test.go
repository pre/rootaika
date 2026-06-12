//go:build !windows

package consolewin

import "testing"

func TestStubControllerVisibility(t *testing.T) {
	ctrl := New()
	stub, ok := ctrl.(*stubController)
	if !ok {
		t.Fatalf("New did not return *stubController, got %T", ctrl)
	}
	if stub.Visible() {
		t.Fatalf("new controller should start hidden")
	}

	if err := stub.SetVisible(true); err != nil {
		t.Fatalf("SetVisible(true): %v", err)
	}
	if !stub.Visible() {
		t.Fatalf("controller should be visible after SetVisible(true)")
	}

	if err := stub.SetVisible(false); err != nil {
		t.Fatalf("SetVisible(false): %v", err)
	}
	if stub.Visible() {
		t.Fatalf("controller should be hidden after SetVisible(false)")
	}

	if err := stub.SetVisible(true); err != nil {
		t.Fatalf("SetVisible(true): %v", err)
	}
	if err := stub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if stub.Visible() {
		t.Fatalf("Close should hide the console")
	}
}
