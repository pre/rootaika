package rootaika

import (
	"net/http"
	"time"

	_ "time/tzdata"
)

type App struct {
	store    *Store
	now      func() time.Time
	location *time.Location
}

func NewApp(store *Store) *App {
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		location = time.Local
	}
	return &App{
		store:    store,
		now:      func() time.Time { return time.Now().UTC() },
		location: location,
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		a.handleDashboard(w, r)
	case r.URL.Path == "/week" && r.Method == http.MethodGet:
		a.handleWeek(w, r)
	case r.URL.Path == "/month" && r.Method == http.MethodGet:
		a.handleMonth(w, r)
	case r.URL.Path == "/board" && r.Method == http.MethodGet:
		a.handleBoard(w, r)
	case r.URL.Path == "/history" && r.Method == http.MethodGet:
		a.handleHistory(w, r)
	case r.URL.Path == "/settings" && r.Method == http.MethodGet:
		a.handleSettings(w, r)
	case r.URL.Path == "/api/v1/charts/usage" && r.Method == http.MethodGet:
		a.handleUsageChart(w, r)
	case r.URL.Path == "/api/v1/charts/programs" && r.Method == http.MethodGet:
		a.handleProgramChart(w, r)
	case r.URL.Path == "/api/v1/board/today" && r.Method == http.MethodGet:
		a.handleBoardToday(w, r)
	case r.URL.Path == "/api/v1/events/batch":
		a.handleEventsBatch(w, r)
	case r.URL.Path == "/api/v1/client/config":
		a.handleClientConfig(w, r)
	case r.URL.Path == "/api/v1/lock" && r.Method == http.MethodGet:
		a.handleLockStatus(w, r)
	case r.URL.Path == "/api/v1/lock":
		a.handleBoardButton(w, r)
	case r.URL.Path == "/api/v1/unlock":
		a.handleBoardUnlock(w, r)
	case stringsHasPrefix(r.URL.Path, "/admin/"):
		a.handleAdmin(w, r)
	default:
		http.NotFound(w, r)
	}
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}
