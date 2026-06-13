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

	CommandLock   = "lock"
	CommandUnlock = "unlock"

	CommandStatusPending = "pending"
	CommandStatusAcked   = "acked"
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
	Categories             []ProgramCategory
}

type Settings struct {
	IdleThresholdSeconds   int
	UploadIntervalSeconds  int
	PollIntervalSeconds    int
	MaxCountableGapSeconds int
	DebugMode              bool
	DebugUnassignedClients bool
}

type Command struct {
	ID        int64
	DeviceID  int64
	Device    string
	Type      string
	Status    string
	Message   string
	CreatedAt time.Time
	AckAt     *time.Time
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

func validCommand(command string) bool {
	return command == CommandLock || command == CommandUnlock
}
