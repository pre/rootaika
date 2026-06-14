package agentapp

import (
	"testing"
	"time"

	"rootaika/client-windows/internal/activity"
	"rootaika/client-windows/internal/model"
)

func TestShouldEmit(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	base := model.Event{State: model.StateActive, ProcessName: "steam.exe"}

	if !shouldEmit(nil, base, time.Time{}, now, heartbeat) {
		t.Fatalf("nil last must emit")
	}

	last := base
	if shouldEmit(&last, base, now, now, heartbeat) {
		t.Fatalf("same state/process before heartbeat must not emit")
	}

	stateChanged := base
	stateChanged.State = model.StateIdle
	if !shouldEmit(&last, stateChanged, now, now, heartbeat) {
		t.Fatalf("state change must emit")
	}

	procChanged := base
	procChanged.ProcessName = "chrome.exe"
	if !shouldEmit(&last, procChanged, now, now, heartbeat) {
		t.Fatalf("process change must emit")
	}

	elapsed := now.Add(heartbeat + time.Second)
	if !shouldEmit(&last, base, now, elapsed, heartbeat) {
		t.Fatalf("elapsed heartbeat must emit")
	}

	if !shouldEmit(&last, base, now, now, 0) {
		t.Fatalf("non-positive heartbeat must emit")
	}
}

func TestEventFromSnapshot(t *testing.T) {
	at := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	t.Run("locked overrides", func(t *testing.T) {
		snap := activity.Snapshot{At: at, ForegroundProcess: `C:\app.exe`, IdleFor: time.Hour}
		ev := eventFromSnapshot(snap, serviceState{Locked: true, IdleThresholdSeconds: 60}, true)
		if ev.State != model.StateLocked {
			t.Fatalf("locked state expected, got %s", ev.State)
		}
		if ev.ProcessName != "" {
			t.Fatalf("locked event should carry no process, got %q", ev.ProcessName)
		}
	})

	t.Run("warning countdown still counts as active", func(t *testing.T) {
		// Server intends a lock (state.Locked) but the screen is not yet locked
		// because the warning countdown is running, so play is still counted.
		snap := activity.Snapshot{At: at, ForegroundProcess: `C:\Games\Steam.EXE`, IdleFor: time.Second}
		ev := eventFromSnapshot(snap, serviceState{Locked: true, LockWarningSeconds: 60, IdleThresholdSeconds: 60}, false)
		if ev.State != model.StateActive {
			t.Fatalf("active expected during warning, got %s", ev.State)
		}
		if ev.ProcessName != "steam.exe" {
			t.Fatalf("process not normalized during warning, got %q", ev.ProcessName)
		}
	})

	t.Run("idle when threshold reached", func(t *testing.T) {
		snap := activity.Snapshot{At: at, ForegroundProcess: "x", IdleFor: 90 * time.Second}
		ev := eventFromSnapshot(snap, serviceState{IdleThresholdSeconds: 60}, false)
		if ev.State != model.StateIdle {
			t.Fatalf("idle expected, got %s", ev.State)
		}
	})

	t.Run("active sets normalized process", func(t *testing.T) {
		snap := activity.Snapshot{At: at, ForegroundProcess: `C:\Games\Steam.EXE`, IdleFor: time.Second}
		ev := eventFromSnapshot(snap, serviceState{IdleThresholdSeconds: 60}, false)
		if ev.State != model.StateActive {
			t.Fatalf("active expected, got %s", ev.State)
		}
		if ev.ProcessName != "steam.exe" {
			t.Fatalf("process not normalized, got %q", ev.ProcessName)
		}
	})

	t.Run("zero threshold falls back to 60s", func(t *testing.T) {
		snap := activity.Snapshot{At: at, ForegroundProcess: "x", IdleFor: 59 * time.Second}
		ev := eventFromSnapshot(snap, serviceState{IdleThresholdSeconds: 0}, false)
		if ev.State != model.StateActive {
			t.Fatalf("59s idle below 60s fallback should be active, got %s", ev.State)
		}
		snap.IdleFor = 60 * time.Second
		ev = eventFromSnapshot(snap, serviceState{IdleThresholdSeconds: 0}, false)
		if ev.State != model.StateIdle {
			t.Fatalf("60s idle at 60s fallback should be idle, got %s", ev.State)
		}
	})

	t.Run("zero At gets filled", func(t *testing.T) {
		ev := eventFromSnapshot(activity.Snapshot{ForegroundProcess: "x"}, serviceState{IdleThresholdSeconds: 60}, false)
		if ev.OccurredAt.IsZero() {
			t.Fatalf("OccurredAt should be filled when snapshot has zero time")
		}
	})
}

func TestNormalizeProcessName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"Steam.EXE", "steam.exe"},
		{`C:\Games\Steam.exe`, "steam.exe"},
		{"  /usr/bin/Code  ", "code"},
		{`folder\Sub\App.Exe`, "app.exe"},
	}
	for _, tc := range cases {
		if got := normalizeProcessName(tc.in); got != tc.want {
			t.Fatalf("normalizeProcessName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLocalClientEndpoint(t *testing.T) {
	cases := []struct {
		address, want string
	}{
		{"127.0.0.1:48611", "http://127.0.0.1:48611/agent/state"},
		{"http://host:8080", "http://host:8080/agent/state"},
		{"https://host:8443/", "https://host:8443/agent/state"},
		{"http://host/", "http://host/agent/state"},
	}
	for _, tc := range cases {
		c := localClient{address: tc.address}
		if got := c.endpoint("/agent/state"); got != tc.want {
			t.Fatalf("endpoint(%q) = %q, want %q", tc.address, got, tc.want)
		}
	}
}
