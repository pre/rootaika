package model

import "testing"

func TestActivityStateValid(t *testing.T) {
	cases := []struct {
		state ActivityState
		want  bool
	}{
		{StateActive, true},
		{StateIdle, true},
		{StateLocked, true},
		{ActivityState(""), false},
		{ActivityState("unknown"), false},
		{ActivityState("ACTIVE"), false},
	}
	for _, tc := range cases {
		if got := tc.state.Valid(); got != tc.want {
			t.Fatalf("ActivityState(%q).Valid() = %v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestCommandIdentifier(t *testing.T) {
	if got := (Command{CommandID: "cmd-1", ID: "id-1"}).Identifier(); got != "cmd-1" {
		t.Fatalf("Identifier prefers CommandID, got %q", got)
	}
	if got := (Command{ID: "id-1"}).Identifier(); got != "id-1" {
		t.Fatalf("Identifier falls back to ID, got %q", got)
	}
	if got := (Command{}).Identifier(); got != "" {
		t.Fatalf("empty command Identifier should be empty, got %q", got)
	}
}
