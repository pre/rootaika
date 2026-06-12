package activity

import (
	"context"
	"time"
)

type Snapshot struct {
	IdleFor           time.Duration
	ForegroundProcess string
	At                time.Time
}

type Probe interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}
