package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/upnp"
)

func run() error {
	statePath := flag.String("state", "", "path to per-package network state JSON file (required)")
	flag.Parse()

	if *statePath == "" {
		return fmt.Errorf("--state is required")
	}

	// Attempt to discover UPnP device; log warning if unavailable.
	var upnpMgr upnp.Manager
	client, err := upnp.NewIGDClient()
	if err != nil {
		slog.Warn(fmt.Sprintf("UPnP unavailable: %v", err))
	} else {
		upnpMgr = client
	}

	ctrl := networkcontroller.NewController(upnpMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		cancel()
	}()

	return ctrl.Run(ctx, *statePath)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "town-os-networkcontroller: %v\n", err)
		os.Exit(1)
	}
}
