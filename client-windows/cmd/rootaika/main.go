// Command rootaika is the single combined Windows client binary. One on-disk
// exe runs both the durable service and the user-session agent (and the
// transient OTA apply-update helper), dispatched on the first argument, so a
// single file swap updates every process.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"rootaika/client-windows/internal/agentapp"
	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/serviceapp"
	"rootaika/client-windows/internal/servicehost"
	"rootaika/client-windows/internal/updater"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rootaika <service|agent|apply-update> [flags]")
		os.Exit(2)
	}
	cmd := os.Args[1]
	rest := os.Args[2:]

	switch cmd {
	case "service":
		runService(rest)
	case "agent":
		runAgent(rest)
	case "apply-update":
		runApplyUpdate(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q (want service|agent|apply-update)\n", cmd)
		os.Exit(2)
	}
}

func runService(args []string) {
	cfgPath := parseConfigFlag("service", args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := servicehost.Run(ctx, "rootaika-service", func(runCtx context.Context) error {
		return serviceapp.Run(runCtx, cfgPath)
	}); err != nil {
		log.Fatal(err)
	}
}

func runAgent(args []string) {
	cfgPath := parseConfigFlag("agent", args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agentapp.Run(ctx, cfgPath); err != nil {
		log.Fatal(err)
	}
}

func runApplyUpdate(args []string) {
	applyArgs, err := updater.ParseApplyArgs(args)
	if err != nil {
		log.Fatal(err)
	}
	if err := updater.ApplyUpdate(applyArgs); err != nil {
		log.Fatal(err)
	}
}

func parseConfigFlag(name string, args []string) string {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "path to client config JSON")
	_ = fs.Parse(args)
	return *cfgPath
}
