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
	// OTA update: the per-device version selection (nil = inherit the global
	// selection) and the version the client last reported it is running.
	SelectedVersionID   *int64
	LastClientVersion   string
	LastClientVersionAt time.Time
}

// ClientVersion is one registered, selectable client release: the OTA triple
// the Windows client needs to download and verify a build. The download origin
// (GitHub owner/repo) is fixed in the client binary, never server-controlled.
type ClientVersion struct {
	ID           int64
	Version      string
	ArtifactName string
	SHA256       string
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
	WarningSoundVersion    string
	Categories             []ProgramCategory
	// OTA update directives resolved from the device/global version selection.
	// All empty means no update is offered and the client keeps running.
	DesiredVersion string
	ArtifactName   string
	SHA256         string
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
	// SelectedVersionID is the global OTA version selection: the id of a
	// client_versions row every device inherits unless it has its own selection.
	// 0 = no version selected (no update offered).
	SelectedVersionID int
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
