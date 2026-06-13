//go:build !windows

package lock

import "context"

type Controller struct {
	locked bool
}

func NewController() *Controller {
	return &Controller{}
}

func (c *Controller) SetLocked(context.Context, bool, string) error {
	return nil
}

func (c *Controller) Close() error {
	return nil
}
