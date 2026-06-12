//go:build windows

package servicehost

import (
	"context"
	"log"
	"time"

	"golang.org/x/sys/windows/svc"
)

type handler struct {
	run func(context.Context) error
}

func Run(ctx context.Context, name string, run func(context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return run(ctx)
	}
	return svc.Run(name, &handler{run: run})
}

func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	status <- svc.Status{State: svc.StartPending}
	go func() {
		done <- h.run(ctx)
	}()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						log.Printf("service stopped with error: %v", err)
						return false, 1
					}
				case <-time.After(20 * time.Second):
					log.Printf("service stop timed out")
				}
				return false, 0
			default:
				log.Printf("unexpected service control request: %v", req.Cmd)
			}
		case err := <-done:
			cancel()
			if err != nil {
				log.Printf("service exited with error: %v", err)
				return false, 1
			}
			return false, 0
		}
	}
}
