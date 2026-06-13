package rootaika

import (
	"sort"
	"time"
)

// DaySpan is a single day window in UTC together with a display label.
type DaySpan struct {
	Start time.Time
	End   time.Time
	Label string
}

func CalculateUsage(events []ActivityEvent, start, end, now time.Time, maxGap time.Duration) UsageReport {
	report := UsageReport{ByProcess: map[string]int64{}}
	if len(events) == 0 || !end.After(start) {
		return report
	}
	if now.IsZero() || now.After(end) {
		now = end
	}
	if now.Before(start) {
		return report
	}

	ordered := append([]ActivityEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
	})

	for i, event := range ordered {
		if event.State != StateActive {
			continue
		}
		if !event.OccurredAt.Before(end) {
			continue
		}

		next := now
		if i+1 < len(ordered) {
			next = ordered[i+1].OccurredAt
		}
		if next.After(now) {
			next = now
		}
		if next.After(end) {
			next = end
		}
		if maxGap > 0 {
			capped := event.OccurredAt.Add(maxGap)
			if next.After(capped) {
				next = capped
			}
		}

		segmentStart := event.OccurredAt
		if segmentStart.Before(start) {
			segmentStart = start
		}
		if next.After(segmentStart) {
			seconds := int64(next.Sub(segmentStart).Seconds())
			if seconds > 0 {
				process := event.ProcessName
				if process == "" {
					process = "unknown"
				}
				report.TotalSeconds += seconds
				report.ByProcess[process] += seconds
			}
		}
	}
	return report
}
