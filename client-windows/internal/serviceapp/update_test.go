package serviceapp

import (
	"testing"
	"time"
)

func TestUpdateCooldown(t *testing.T) {
	s := &stateStore{}
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)

	if s.updateOnCooldown("v2", now) {
		t.Fatalf("no prior failure must not be on cooldown")
	}

	s.recordFailedUpdate("v2", now)
	if !s.updateOnCooldown("v2", now.Add(time.Minute)) {
		t.Fatalf("same version within cooldown must be suppressed")
	}
	if s.updateOnCooldown("v3", now.Add(time.Minute)) {
		t.Fatalf("a different version must never be on cooldown")
	}
	if s.updateOnCooldown("v2", now.Add(updateRetryCooldown+time.Second)) {
		t.Fatalf("cooldown must expire after the window elapses")
	}
}
