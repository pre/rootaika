package rootaika

import (
	"context"
	"net/http"
	"time"
)

// BoardToday is the compact JSON payload for the external e-ink board: one entry
// per device with today's total active minutes, plus the admin-configured refresh
// interval so the display can pace itself without its own configuration.
type BoardToday struct {
	Now            string             `json:"now"`
	RefreshSeconds int                `json:"refresh_seconds"`
	Devices        []BoardDeviceUsage `json:"devices"`
}

type BoardDeviceUsage struct {
	Name    string `json:"name"`
	Minutes int    `json:"minutes"`
}

func (a *App) handleBoardToday(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	board, err := a.boardToday(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "board summary failed")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

func (a *App) boardToday(ctx context.Context) (BoardToday, error) {
	now := a.now()
	localNow := now.In(a.location)
	startLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, a.location)
	start := startLocal.UTC()
	end := now.UTC()

	settings, err := a.store.Settings(ctx)
	if err != nil {
		return BoardToday{}, err
	}
	devices, err := a.store.Devices(ctx)
	if err != nil {
		return BoardToday{}, err
	}

	maxGap := time.Duration(settings.MaxCountableGapSeconds) * time.Second
	usages := make([]BoardDeviceUsage, 0, len(devices))
	for _, device := range devices {
		events, err := a.store.ReportEvents(ctx, device.ID, start, end)
		if err != nil {
			return BoardToday{}, err
		}
		report := CalculateUsage(events, start, end, now, maxGap)
		usages = append(usages, BoardDeviceUsage{
			Name:    device.DisplayName,
			Minutes: secondsToWholeMinutes(report.TotalSeconds),
		})
	}

	return BoardToday{
		Now:            localNow.Format("2006-01-02 15:04:05"),
		RefreshSeconds: settings.BoardRefreshSeconds,
		Devices:        usages,
	}, nil
}

// secondsToWholeMinutes rounds to the nearest whole minute for the board, which
// has no room for sub-minute precision.
func secondsToWholeMinutes(seconds int64) int {
	return int((seconds + 30) / 60)
}
