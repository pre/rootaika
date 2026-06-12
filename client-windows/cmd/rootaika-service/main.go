package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"rootaika/client-windows/internal/config"
	"rootaika/client-windows/internal/serviceapp"
	"rootaika/client-windows/internal/servicehost"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", config.DefaultPath(), "path to client config JSON")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := servicehost.Run(ctx, "rootaika-service", func(runCtx context.Context) error {
		return serviceapp.Run(runCtx, cfgPath)
	}); err != nil {
		log.Fatal(err)
	}
}
