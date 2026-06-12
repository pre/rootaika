//go:build !windows

package servicehost

import "context"

func Run(ctx context.Context, _ string, run func(context.Context) error) error {
	return run(ctx)
}
