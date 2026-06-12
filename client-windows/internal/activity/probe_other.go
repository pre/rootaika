//go:build !windows

package activity

import (
	"context"
	"time"
)

type stubProbe struct{}

func NewProbe() Probe {
	return stubProbe{}
}

func (stubProbe) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{
		IdleFor:           0,
		ForegroundProcess: "nonwindows-stub",
		At:                time.Now().UTC(),
	}, nil
}
