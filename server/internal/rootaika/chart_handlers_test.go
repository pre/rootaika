package rootaika

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestUsageChartEndpoint(t *testing.T) {
	app := testApp(t)
	if _, err := app.store.EnsureDevice(context.Background(), "client-1", app.now()); err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/usage?range=day", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var chart UsageChart
	if err := json.Unmarshal(recorder.Body.Bytes(), &chart); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if chart.Range != "day" {
		t.Fatalf("range = %q", chart.Range)
	}
	if chart.YMaxMinutes <= 0 {
		t.Fatalf("y_max = %d", chart.YMaxMinutes)
	}
	if len(chart.Devices) != 1 {
		t.Fatalf("devices = %d", len(chart.Devices))
	}
}

func TestUsageChartDefaultsToDay(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/usage", nil)
	request.SetBasicAuth("client", "client")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var chart UsageChart
	if err := json.Unmarshal(recorder.Body.Bytes(), &chart); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if chart.Range != "day" {
		t.Fatalf("default range = %q, want day", chart.Range)
	}
}

func TestUsageChartRejectsBadRange(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/usage?range=year", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUsageChartRequiresAuth(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/usage", nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestProgramChartEndpoint(t *testing.T) {
	app := testApp(t)
	device, err := app.store.EnsureDevice(context.Background(), "client-1", app.now())
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/programs?range=day&device_id="+strconv.FormatInt(device.ID, 10), nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var chart ProgramChart
	if err := json.Unmarshal(recorder.Body.Bytes(), &chart); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if chart.DeviceID != device.ID {
		t.Fatalf("device id = %d", chart.DeviceID)
	}
}

func TestProgramChartRejectsMissingDevice(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/charts/programs?range=day", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestBoardViewRenders(t *testing.T) {
	app := testApp(t)
	request := httptest.NewRequest(http.MethodGet, "/board", nil)
	request.SetBasicAuth("admin", "admin")
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"renderUsageChart", "renderProgramCharts", "setInterval", "/api/v1/charts/usage"} {
		if !strings.Contains(body, want) {
			t.Fatalf("board missing %q", want)
		}
	}
}
