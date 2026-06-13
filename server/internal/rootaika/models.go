package rootaika

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleClient Role = "client"

	EventTypeActivityObserved = "activity_observed"

	StateActive = "active"
	StateIdle   = "idle"
	StateLocked = "locked"
)

type Credential struct {
	Role     Role
	Username string
	Password string
}

type User struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

type Device struct {
	ID                 int64
	ClientUUID         string
	DisplayName        string
	UserID             *int64
	UserName           string
	RegistrationStatus string
	CreatedAt          time.Time
	LastSeenAt         time.Time
	Locked             bool
	WarningSeconds     int
	LastStatus         string
	LastStatusAt       time.Time
}

type ActivityEvent struct {
	ID          int64
	EventUUID   string
	DeviceID    int64
	Type        string
	State       string
	OccurredAt  time.Time
	ProcessName string
	Sequence    int64
}

type EventInput struct {
	EventUUID   string
	Type        string
	OccurredAt  time.Time
	State       string
	ProcessName string
	Sequence    int64
}

type ClientConfig struct {
	DeviceID               int64
	IdleThresholdSeconds   int
	UploadIntervalSeconds  int
	PollIntervalSeconds    int
	MaxCountableGapSeconds int
	DebugMode              bool
	Locked                 bool
	LockMessage            string
	WarningSeconds         int
	Categories             []ProgramCategory
}

type Settings struct {
	IdleThresholdSeconds   int
	UploadIntervalSeconds  int
	PollIntervalSeconds    int
	MaxCountableGapSeconds int
	ChartYMaxMinutes       int
	BoardRefreshSeconds    int
	DebugMode              bool
	DebugUnassignedClients bool
}

type ProgramCategory struct {
	ID        int64
	MatchType string
	Pattern   string
	Category  string
	CreatedAt time.Time
}

type UsageReport struct {
	TotalSeconds int64
	ByProcess    map[string]int64
}

func validState(state string) bool {
	return state == StateActive || state == StateIdle || state == StateLocked
}
