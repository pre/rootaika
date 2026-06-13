package rootaika

import (
	"context"
	"errors"
	"math"
	"time"
)

// ChartRange selects the time window and bucketing for a chart.
type ChartRange string

const (
	RangeDay   ChartRange = "day"
	RangeWeek  ChartRange = "week"
	RangeMonth ChartRange = "month"

	// intradayBinMinutes is the spacing between points on the cumulative
	// "today" chart (x = time of day).
	intradayBinMinutes = 15

	// intradayStartHour is the first hour drawn on the cumulative "today"
	// chart. The empty night hours before it are skipped to give the daytime
	// data more horizontal room; values stay cumulative from midnight, so any
	// usage before this hour still shows up in the first plotted point.
	intradayStartHour = 7

	weekDayCount  = 7
	monthDayCount = 30
)

// chartSpan is a single x-axis point: usage is computed for [Start, End) and
// plotted under Label. For the cumulative day range every span shares the same
// Start (local midnight); for week/month each span is one day window.
type chartSpan struct {
	Start time.Time
	End   time.Time
	Label string
}

// UsageChart is the payload for the all-devices usage chart.
type UsageChart struct {
	Range       string         `json:"range"`
	Labels      []string       `json:"labels"`
	YMaxMinutes int            `json:"y_max_minutes"`
	Now         string         `json:"now"`
	Devices     []DeviceSeries `json:"devices"`
}

type DeviceSeries struct {
	DeviceID int64     `json:"device_id"`
	Name     string    `json:"name"`
	Points   []float64 `json:"points"`
}

// ProgramChart is the payload for one device's per-program charts: a time
// series per program plus total minutes per program for the bar chart.
type ProgramChart struct {
	Range       string          `json:"range"`
	DeviceID    int64           `json:"device_id"`
	Labels      []string        `json:"labels"`
	YMaxMinutes int             `json:"y_max_minutes"`
	Now         string          `json:"now"`
	Series      []ProgramSeries `json:"series"`
	Totals      []ProgramTotal  `json:"totals"`
}

type ProgramSeries struct {
	Program string    `json:"program"`
	Points  []float64 `json:"points"`
}

type ProgramTotal struct {
	Program string  `json:"program"`
	Minutes float64 `json:"minutes"`
}

func parseChartRange(value string) (ChartRange, error) {
	switch ChartRange(value) {
	case RangeDay:
		return RangeDay, nil
	case RangeWeek:
		return RangeWeek, nil
	case RangeMonth:
		return RangeMonth, nil
	default:
		return "", errors.New("range must be day, week or month")
	}
}

// chartSpans builds the x-axis spans for a range. The day range is cumulative
// (each span starts at local midnight), week/month reuse dailySpans.
func chartSpans(now time.Time, location *time.Location, r ChartRange) []chartSpan {
	switch r {
	case RangeWeek:
		return spansFromDays(dailySpans(now, location, weekDayCount))
	case RangeMonth:
		return spansFromDays(dailySpans(now, location, monthDayCount))
	default:
		return intradaySpans(now, location)
	}
}

func spansFromDays(days []DaySpan) []chartSpan {
	spans := make([]chartSpan, 0, len(days))
	for _, day := range days {
		spans = append(spans, chartSpan{Start: day.Start, End: day.End, Label: day.Label})
	}
	return spans
}

// intradaySpans returns cumulative spans for the "today" chart. Every span
// starts at local midnight so the plotted value is cumulative active time so
// far that day, but the x-axis only begins at intradayStartHour: the empty
// night hours are skipped to give the daytime data more room. Usage before the
// start hour is not lost, it shows up folded into the first plotted point.
func intradaySpans(now time.Time, location *time.Location) []chartSpan {
	localNow := now.In(location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	startUTC := dayStart.UTC()
	axisStart := dayStart.Add(time.Duration(intradayStartHour) * time.Hour)
	step := time.Duration(intradayBinMinutes) * time.Minute

	// Before the start hour there is only a single point at now.
	if !localNow.After(axisStart) {
		return []chartSpan{{Start: startUTC, End: localNow.UTC(), Label: localNow.Format("15:04")}}
	}

	var spans []chartSpan
	for t := axisStart; t.Before(localNow); t = t.Add(step) {
		spans = append(spans, chartSpan{Start: startUTC, End: t.UTC(), Label: t.Format("15:04")})
	}
	spans = append(spans, chartSpan{Start: startUTC, End: localNow.UTC(), Label: localNow.Format("15:04")})
	return spans
}

func (a *App) usageChart(ctx context.Context, r ChartRange) (UsageChart, error) {
	now := a.now()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		return UsageChart{}, err
	}
	devices, err := a.store.Devices(ctx)
	if err != nil {
		return UsageChart{}, err
	}

	spans := chartSpans(now, a.location, r)
	maxGap := time.Duration(settings.MaxCountableGapSeconds) * time.Second
	rangeStart := spans[0].Start
	rangeEnd := spans[len(spans)-1].End

	series := make([]DeviceSeries, 0, len(devices))
	for _, device := range devices {
		events, err := a.store.ReportEvents(ctx, device.ID, rangeStart, rangeEnd)
		if err != nil {
			return UsageChart{}, err
		}
		points := make([]float64, len(spans))
		for i, span := range spans {
			seconds := CalculateUsage(events, span.Start, span.End, now, maxGap).TotalSeconds
			points[i] = secondsToMinutes(seconds)
		}
		series = append(series, DeviceSeries{DeviceID: device.ID, Name: device.DisplayName, Points: points})
	}

	labels := spanLabels(spans)
	if r != RangeDay {
		sets := make([][]float64, len(series))
		for i := range series {
			sets[i] = series[i].Points
		}
		if start := firstNonEmptyIndex(labels, sets); start > 0 {
			labels = labels[start:]
			for i := range series {
				series[i].Points = series[i].Points[start:]
			}
		}
	}

	return UsageChart{
		Range:       string(r),
		Labels:      labels,
		YMaxMinutes: settings.ChartYMaxMinutes,
		Now:         now.In(a.location).Format("2006-01-02 15:04:05"),
		Devices:     series,
	}, nil
}

func (a *App) programChart(ctx context.Context, deviceID int64, r ChartRange) (ProgramChart, error) {
	now := a.now()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		return ProgramChart{}, err
	}

	spans := chartSpans(now, a.location, r)
	maxGap := time.Duration(settings.MaxCountableGapSeconds) * time.Second
	rangeStart := spans[0].Start
	rangeEnd := spans[len(spans)-1].End

	events, err := a.store.ReportEvents(ctx, deviceID, rangeStart, rangeEnd)
	if err != nil {
		return ProgramChart{}, err
	}

	// Totals over the whole range define which programs to show and their
	// order (largest first), reusing the same ordering as the dashboard.
	totalsReport := CalculateUsage(events, rangeStart, rangeEnd, now, maxGap)
	ordered := processViews(totalsReport.ByProcess)
	totals := make([]ProgramTotal, 0, len(ordered))
	for _, p := range ordered {
		totals = append(totals, ProgramTotal{Program: p.Name, Minutes: secondsToMinutes(p.Seconds)})
	}

	// One time series per program, in the same order as totals.
	series := make([]ProgramSeries, len(ordered))
	for i, p := range ordered {
		series[i] = ProgramSeries{Program: p.Name, Points: make([]float64, len(spans))}
	}
	indexOf := make(map[string]int, len(ordered))
	for i, p := range ordered {
		indexOf[p.Name] = i
	}
	for spanIdx, span := range spans {
		byProcess := CalculateUsage(events, span.Start, span.End, now, maxGap).ByProcess
		for name, seconds := range byProcess {
			if i, ok := indexOf[name]; ok {
				series[i].Points[spanIdx] = secondsToMinutes(seconds)
			}
		}
	}

	labels := spanLabels(spans)
	if r != RangeDay {
		sets := make([][]float64, len(series))
		for i := range series {
			sets[i] = series[i].Points
		}
		if start := firstNonEmptyIndex(labels, sets); start > 0 {
			labels = labels[start:]
			for i := range series {
				series[i].Points = series[i].Points[start:]
			}
		}
	}

	return ProgramChart{
		Range:       string(r),
		DeviceID:    deviceID,
		Labels:      labels,
		YMaxMinutes: settings.ChartYMaxMinutes,
		Now:         now.In(a.location).Format("2006-01-02 15:04:05"),
		Series:      series,
		Totals:      totals,
	}, nil
}

// firstNonEmptyIndex returns the index of the first column where any series has
// data, so leading empty days can be dropped from the left of a chart. Trailing
// empty days (future) are kept. Returns 0 when there is no data at all (or only
// one column), so the chart is never reduced to nothing.
func firstNonEmptyIndex(labels []string, sets [][]float64) int {
	for col := range labels {
		for _, points := range sets {
			if col < len(points) && points[col] > 0 {
				return col
			}
		}
	}
	return 0
}

func spanLabels(spans []chartSpan) []string {
	labels := make([]string, len(spans))
	for i, span := range spans {
		labels[i] = span.Label
	}
	return labels
}

// secondsToMinutes converts seconds to minutes rounded to one decimal so the
// chart lines move smoothly without exposing sub-second noise.
func secondsToMinutes(seconds int64) float64 {
	return math.Round(float64(seconds)/60*10) / 10
}
