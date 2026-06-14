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

// boardButtonMessage is the lock overlay text shown when the physical board
// button locks all devices.
const boardButtonMessage = "Nappi painettu"

// boardButtonWarningSeconds is the lock-warning countdown applied when the
// physical board button locks all devices: clients play the warning sound and
// show a click-through overlay for this long before the screen actually locks.
// Matches the per-device admin lock form default.
const boardButtonWarningSeconds = 60

// handleBoardButton toggles the lock state of all registered devices from a
// single physical button press on the board. It uses the client role so the Pi
// can call it with the same client/client credentials it already uses for
// GET /api/v1/board/today.
func (a *App) handleBoardButton(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	locked, affected, err := a.store.ToggleAllLocks(r.Context(), boardButtonMessage, boardButtonWarningSeconds, a.now())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "toggle locks failed")
		return
	}
	a.notifier.notify()
	writeJSON(w, http.StatusOK, map[string]any{
		"locked":   locked,
		"affected": affected,
	})
}

// handleLockStatus reports the system-wide lock state so the board button can
// query what the system currently is before deciding its next action. Lock is
// per-device, but the button operates on the global state: locked means at least
// one registered device is locked, matching what a toggle would flip.
func (a *App) handleLockStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	locked, lockedCount, totalCount, err := a.store.GlobalLockState(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "lock status failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"locked":       locked,
		"locked_count": lockedCount,
		"total_count":  totalCount,
	})
}

// handleBoardUnlock unconditionally releases the lock on all registered devices.
// Unlike handleBoardButton it never locks, so the board can use it as a
// dedicated release control when the current lock state is unknown.
func (a *App) handleBoardUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	affected, err := a.store.UnlockAllLocks(r.Context(), a.now())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unlock failed")
		return
	}
	a.notifier.notify()
	writeJSON(w, http.StatusOK, map[string]any{
		"locked":   false,
		"affected": affected,
	})
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
