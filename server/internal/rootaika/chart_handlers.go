package rootaika

import (
	"net/http"
	"strconv"
	"strings"
)

func (a *App) handleUsageChart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	chartRange, err := parseChartRange(rangeParam(r))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	chart, err := a.usageChart(r.Context(), chartRange)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "usage chart failed")
		return
	}
	writeJSON(w, http.StatusOK, chart)
}

func (a *App) handleProgramChart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := a.requireRole(w, r, RoleAdmin, RoleClient); !ok {
		return
	}

	deviceID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("device_id")), 10, 64)
	if err != nil || deviceID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "device_id must be a positive integer")
		return
	}
	chartRange, err := parseChartRange(rangeParam(r))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	chart, err := a.programChart(r.Context(), deviceID, chartRange)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "program chart failed")
		return
	}
	writeJSON(w, http.StatusOK, chart)
}

// rangeParam defaults to the day range when the caller omits it.
func rangeParam(r *http.Request) string {
	value := strings.TrimSpace(r.URL.Query().Get("range"))
	if value == "" {
		return string(RangeDay)
	}
	return value
}
