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

// LockTransition is a single change of a device's client-reported lock state,
// derived from the append-only events stream: a device entering state=locked is
// a lock, leaving it is an unlock.
type LockTransition struct {
	DeviceID   int64
	DeviceName string
	OccurredAt time.Time
	Locked     bool
}

// Lock audit sources record where an admin/board lock action originated.
const (
	LockSourceAdmin       = "admin"
	LockSourceBoardButton = "board-button"
	LockSourceBoardUnlock = "board-unlock"
)

// LockAuditEntry is a recorded admin or board lock action. Device-wide board
// actions have DeviceID == nil (DeviceName is then empty); per-device admin
// actions name the affected device.
type LockAuditEntry struct {
	ID         int64
	OccurredAt time.Time
	DeviceID   *int64
	DeviceName string
	Locked     bool
	Source     string
	Affected   int
}

func validState(state string) bool {
	return state == StateActive || state == StateIdle || state == StateLocked
}
