package rootaika

import "testing"

func TestValidState(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{state: StateActive, want: true},
		{state: StateIdle, want: true},
		{state: StateLocked, want: true},
		{state: "bogus", want: false},
		{state: "", want: false},
	}
	for _, tt := range tests {
		if got := validState(tt.state); got != tt.want {
			t.Fatalf("validState(%q) = %v want %v", tt.state, got, tt.want)
		}
	}
}

func TestValidCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: CommandLock, want: true},
		{command: CommandUnlock, want: true},
		{command: "explode", want: false},
		{command: "", want: false},
	}
	for _, tt := range tests {
		if got := validCommand(tt.command); got != tt.want {
			t.Fatalf("validCommand(%q) = %v want %v", tt.command, got, tt.want)
		}
	}
}

func TestNewAppDefaults(t *testing.T) {
	store := testStore(t)
	app := NewApp(store)
	if app.store != store {
		t.Fatalf("store not wired")
	}
	if app.now == nil {
		t.Fatalf("now func not set")
	}
	if app.location == nil {
		t.Fatalf("location not set")
	}
	if app.now().IsZero() {
		t.Fatalf("default now returned zero time")
	}
}
