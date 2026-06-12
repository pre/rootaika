package model

import "time"

const EventTypeActivityObserved = "activity_observed"

type ActivityState string

const (
	StateActive ActivityState = "active"
	StateIdle   ActivityState = "idle"
	StateLocked ActivityState = "locked"
)

func (s ActivityState) Valid() bool {
	switch s {
	case StateActive, StateIdle, StateLocked:
		return true
	default:
		return false
	}
}

type Event struct {
	EventID     string        `json:"event_id,omitempty"`
	Type        string        `json:"type,omitempty"`
	OccurredAt  time.Time     `json:"occurred_at"`
	State       ActivityState `json:"state"`
	ProcessName string        `json:"process_name,omitempty"`
	Sequence    int64         `json:"sequence,omitempty"`
}

type EventBatch struct {
	ClientID string  `json:"client_id"`
	Events   []Event `json:"events"`
}

type ClientConfig struct {
	IdleThresholdSeconds   int `json:"idle_threshold_seconds"`
	UploadIntervalSeconds  int `json:"upload_interval_seconds"`
	PollIntervalSeconds    int `json:"poll_interval_seconds"`
	ObserveIntervalSeconds int `json:"observe_interval_seconds,omitempty"`
	MaxCountableGapSeconds int `json:"max_countable_gap_seconds,omitempty"`
}

type CommandType string

const (
	CommandLock   CommandType = "lock"
	CommandUnlock CommandType = "unlock"
)

type Command struct {
	CommandID string      `json:"command_id,omitempty"`
	ID        string      `json:"id,omitempty"`
	Type      CommandType `json:"type"`
}

func (c Command) Identifier() string {
	if c.CommandID != "" {
		return c.CommandID
	}
	return c.ID
}

type CommandsResponse struct {
	Commands []Command `json:"commands"`
}
