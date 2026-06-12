package rootaika

import (
	"testing"
	"time"
)

func TestCalculateUsageCountsOnlyActiveSegmentsAndCapsGaps(t *testing.T) {
	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Hour)
	events := []ActivityEvent{
		{State: StateActive, ProcessName: "steam.exe", OccurredAt: start.Add(10 * time.Minute), Sequence: 1},
		{State: StateIdle, OccurredAt: start.Add(40 * time.Minute), Sequence: 2},
		{State: StateActive, ProcessName: "browser.exe", OccurredAt: start.Add(50 * time.Minute), Sequence: 3},
		{State: StateLocked, OccurredAt: start.Add(70 * time.Minute), Sequence: 4},
		{State: StateActive, ProcessName: "steam.exe", OccurredAt: start.Add(80 * time.Minute), Sequence: 5},
	}

	report := CalculateUsage(events, start, start.Add(24*time.Hour), now, 15*time.Minute)

	if report.TotalSeconds != int64((15+15+15)*60) {
		t.Fatalf("total seconds = %d", report.TotalSeconds)
	}
	if report.ByProcess["steam.exe"] != int64(30*60) {
		t.Fatalf("steam seconds = %d", report.ByProcess["steam.exe"])
	}
	if report.ByProcess["browser.exe"] != int64(15*60) {
		t.Fatalf("browser seconds = %d", report.ByProcess["browser.exe"])
	}
}

func TestCalculateUsageUsesPreviousEventAcrossDayBoundary(t *testing.T) {
	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	events := []ActivityEvent{
		{State: StateActive, ProcessName: "game.exe", OccurredAt: start.Add(-10 * time.Minute), Sequence: 1},
		{State: StateIdle, OccurredAt: start.Add(5 * time.Minute), Sequence: 2},
	}

	report := CalculateUsage(events, start, start.Add(24*time.Hour), start.Add(time.Hour), 30*time.Minute)

	if report.TotalSeconds != int64(5*60) {
		t.Fatalf("total seconds = %d", report.TotalSeconds)
	}
}
