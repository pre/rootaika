package rootaika

import (
	"context"
	"testing"
	"time"
)

func TestParseChartRange(t *testing.T) {
	tests := []struct {
		in      string
		want    ChartRange
		wantErr bool
	}{
		{in: "day", want: RangeDay},
		{in: "week", want: RangeWeek},
		{in: "month", want: RangeMonth},
		{in: "year", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseChartRange(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseChartRange(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseChartRange(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseChartRange(%q) = %q", tt.in, got)
		}
	}
}

func TestIntradaySpansAreCumulativeFromMidnight(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) // afternoon, axis runs 07:00 -> now
	spans := intradaySpans(now, location)

	if len(spans) < 2 {
		t.Fatalf("expected several spans, got %d", len(spans))
	}
	// Every span is cumulative from midnight even though the axis starts at 07:00.
	midnight := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	for i, span := range spans {
		if !span.Start.Equal(midnight) {
			t.Fatalf("span %d start = %v, want midnight %v (all spans cumulative)", i, span.Start, midnight)
		}
	}
	if spans[0].Label != "07:00" {
		t.Fatalf("first label = %q, want axis to start at 07:00", spans[0].Label)
	}
	last := spans[len(spans)-1]
	if !last.End.Equal(now) {
		t.Fatalf("last span end = %v, want now %v", last.End, now)
	}
	if last.Label != "12:00" {
		t.Fatalf("last label = %q", last.Label)
	}
}

func TestIntradaySpansBeforeStartHourReturnsSinglePoint(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 6, 11, 3, 30, 0, 0, time.UTC) // before 07:00
	spans := intradaySpans(now, location)

	if len(spans) != 1 {
		t.Fatalf("before start hour want 1 span, got %d", len(spans))
	}
	midnight := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	if !spans[0].Start.Equal(midnight) || !spans[0].End.Equal(now) {
		t.Fatalf("single span = %+v, want midnight->now", spans[0])
	}
	if spans[0].Label != "03:30" {
		t.Fatalf("label = %q", spans[0].Label)
	}
}

func TestUsageChartCumulativeDay(t *testing.T) {
	app := testApp(t) // now = 2026-06-11 12:00 UTC
	ctx := context.Background()

	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	// Heartbeat active events every 5 min (== default maxGap) from 09:00 to
	// 09:30, then idle. Spacing within maxGap means every gap counts fully, so
	// the day total is 30 min.
	events := []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC), Sequence: 1},
		{EventUUID: "b", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 5, 0, 0, time.UTC), Sequence: 2},
		{EventUUID: "c", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 10, 0, 0, time.UTC), Sequence: 3},
		{EventUUID: "d", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 15, 0, 0, time.UTC), Sequence: 4},
		{EventUUID: "e", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 20, 0, 0, time.UTC), Sequence: 5},
		{EventUUID: "f", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 25, 0, 0, time.UTC), Sequence: 6},
		{EventUUID: "g", Type: EventTypeActivityObserved, State: StateIdle, OccurredAt: time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC), Sequence: 7},
	}
	if _, _, err := app.store.InsertEvents(ctx, device.ID, events, app.now()); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	chart, err := app.usageChart(ctx, RangeDay)
	if err != nil {
		t.Fatalf("usageChart: %v", err)
	}
	if chart.Range != "day" {
		t.Fatalf("range = %q", chart.Range)
	}
	if chart.YMaxMinutes != 720 {
		t.Fatalf("y_max = %d, want default 720", chart.YMaxMinutes)
	}
	if len(chart.Devices) != 1 {
		t.Fatalf("devices = %d", len(chart.Devices))
	}
	points := chart.Devices[0].Points
	if len(points) != len(chart.Labels) {
		t.Fatalf("points/labels mismatch: %d vs %d", len(points), len(chart.Labels))
	}
	// Cumulative series must be non-decreasing.
	for i := 1; i < len(points); i++ {
		if points[i] < points[i-1]-0.001 {
			t.Fatalf("cumulative series decreased at %d: %v -> %v", i, points[i-1], points[i])
		}
	}
	// The final cumulative point must equal the whole-day usage.
	dayStart := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	wantSeconds := CalculateUsage(toActivityEvents(events), dayStart, app.now(), app.now(), 300*time.Second).TotalSeconds
	final := points[len(points)-1]
	if final != secondsToMinutes(wantSeconds) {
		t.Fatalf("final cumulative = %v min, want %v min (whole-day usage)", final, secondsToMinutes(wantSeconds))
	}
	if final < 29.9 || final > 30.1 {
		t.Fatalf("final cumulative minutes = %v, want ~30", final)
	}
}

func toActivityEvents(inputs []EventInput) []ActivityEvent {
	events := make([]ActivityEvent, len(inputs))
	for i, in := range inputs {
		events[i] = ActivityEvent{
			EventUUID:   in.EventUUID,
			Type:        in.Type,
			State:       in.State,
			OccurredAt:  in.OccurredAt,
			ProcessName: in.ProcessName,
			Sequence:    in.Sequence,
		}
	}
	return events
}

func TestProgramChartTotalsAndSeries(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()

	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	// game.exe runs 09:00->09:15 (heartbeats within maxGap = 15 min), then
	// browser.exe 09:15->09:20 (5 min), then idle.
	events := []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC), Sequence: 1},
		{EventUUID: "b", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 5, 0, 0, time.UTC), Sequence: 2},
		{EventUUID: "c", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 10, 0, 0, time.UTC), Sequence: 3},
		{EventUUID: "d", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "browser.exe", OccurredAt: time.Date(2026, 6, 11, 9, 15, 0, 0, time.UTC), Sequence: 4},
		{EventUUID: "e", Type: EventTypeActivityObserved, State: StateIdle, OccurredAt: time.Date(2026, 6, 11, 9, 20, 0, 0, time.UTC), Sequence: 5},
	}
	if _, _, err := app.store.InsertEvents(ctx, device.ID, events, app.now()); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	chart, err := app.programChart(ctx, device.ID, RangeDay)
	if err != nil {
		t.Fatalf("programChart: %v", err)
	}
	if chart.DeviceID != device.ID {
		t.Fatalf("device id = %d", chart.DeviceID)
	}
	if len(chart.Totals) != 2 {
		t.Fatalf("totals = %d, want 2 programs", len(chart.Totals))
	}
	// Largest first: game.exe (20 min) before browser.exe (5 min).
	if chart.Totals[0].Program != "game.exe" {
		t.Fatalf("totals[0] = %q, want game.exe first", chart.Totals[0].Program)
	}
	if chart.Totals[0].Minutes < 14.9 || chart.Totals[0].Minutes > 15.1 {
		t.Fatalf("game.exe minutes = %v, want ~15", chart.Totals[0].Minutes)
	}
	if len(chart.Series) != 2 {
		t.Fatalf("series = %d", len(chart.Series))
	}
	for _, s := range chart.Series {
		if len(s.Points) != len(chart.Labels) {
			t.Fatalf("series %q points/labels mismatch", s.Program)
		}
	}
}

func TestFirstNonEmptyIndex(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		sets   [][]float64
		want   int
	}{
		{
			name:   "leading empties trimmed to first data column",
			labels: []string{"a", "b", "c", "d"},
			sets:   [][]float64{{0, 0, 5, 0}, {0, 0, 0, 3}},
			want:   2,
		},
		{
			name:   "data in first column keeps everything",
			labels: []string{"a", "b"},
			sets:   [][]float64{{1, 0}},
			want:   0,
		},
		{
			name:   "no data anywhere keeps everything",
			labels: []string{"a", "b", "c"},
			sets:   [][]float64{{0, 0, 0}},
			want:   0,
		},
		{
			name:   "no series keeps everything",
			labels: []string{"a", "b"},
			sets:   nil,
			want:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstNonEmptyIndex(tt.labels, tt.sets); got != tt.want {
				t.Fatalf("firstNonEmptyIndex = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUsageChartTrimsLeadingEmptyDays(t *testing.T) {
	app := testApp(t) // now = 2026-06-11 12:00 UTC
	ctx := context.Background()
	device, err := app.store.EnsureDevice(ctx, "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	// Only data today (the last day of the week range).
	events := []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC), Sequence: 1},
		{EventUUID: "b", Type: EventTypeActivityObserved, State: StateIdle, OccurredAt: time.Date(2026, 6, 11, 9, 10, 0, 0, time.UTC), Sequence: 2},
	}
	if _, _, err := app.store.InsertEvents(ctx, device.ID, events, app.now()); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	chart, err := app.usageChart(ctx, RangeWeek)
	if err != nil {
		t.Fatalf("usageChart: %v", err)
	}
	// The 7-day week should be trimmed to start at the only day with data.
	if len(chart.Labels) != 1 {
		t.Fatalf("week labels = %d (%v), want 1 after trimming", len(chart.Labels), chart.Labels)
	}
	if len(chart.Devices[0].Points) != len(chart.Labels) {
		t.Fatalf("points/labels mismatch after trim")
	}
}

func TestUsageChartRangesProduceDailyBuckets(t *testing.T) {
	app := testApp(t)
	ctx := context.Background()
	if _, err := app.store.EnsureDevice(ctx, "client-1", app.now()); err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	week, err := app.usageChart(ctx, RangeWeek)
	if err != nil {
		t.Fatalf("week chart: %v", err)
	}
	if len(week.Labels) != weekDayCount {
		t.Fatalf("week labels = %d, want %d", len(week.Labels), weekDayCount)
	}
	month, err := app.usageChart(ctx, RangeMonth)
	if err != nil {
		t.Fatalf("month chart: %v", err)
	}
	if len(month.Labels) != monthDayCount {
		t.Fatalf("month labels = %d, want %d", len(month.Labels), monthDayCount)
	}
}
