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
