// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Command town-os-ingress is the shared Host-router ingress sidecar: it
// supervises a caddy child and serves a gRPC management API (on a Unix socket)
// the systemcontroller uses to program routes. Caddy terminates TLS per-SNI on
// :443 with each package/page leaf and Host-routes plain HTTP on :80, reverse-
// proxying to backends on the ingress network.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"google.golang.org/grpc"

	"gitea.com/town-os/town-os/src/caddysup"
	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

func run() error {
	socket := flag.String("socket", "/data/ingress.sock", "unix socket path for the gRPC management API")
	port := flag.Int("port", 443, "TCP port the HTTPS vhosts bind (443 in production; ephemeral in tests)")
	httpPort := flag.Int("http-port", 80, "TCP port the HTTP vhosts bind (80 in production; ephemeral in tests)")
	defaultBackend := flag.String("default-backend", "", "container:port to reverse-proxy for hosts not matched by a route on :80 (the Town OS UI)")
	caddyBin := flag.String("caddy", caddysup.DefaultCaddyBinary, "path to the caddy binary")
	caddyCfg := flag.String("caddy-config", caddysup.DefaultCaddyConfigPath, "path to the rendered Caddyfile")
	flag.Parse()

	// Ensure the socket directory exists and clear a stale socket from a
	// previous run (the file persists on the bind-mounted data dir).
	if dir := filepath.Dir(*socket); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // socket dir must be reachable by the systemcontroller
			return fmt.Errorf("create socket dir: %w", err)
		}
	}
	if err := os.Remove(*socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %s: %w", *socket, err)
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "unix", *socket)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", *socket, err)
	}

	sup := caddysup.NewSupervisor(*caddyBin, *caddyCfg)
	srv := ingress.NewServer(sup, *port, *httpPort, *defaultBackend)
	// Start caddy with the initial (empty) config so the supervisor is live
	// before the first route arrives.
	if err := srv.Bootstrap(); err != nil {
		return fmt.Errorf("bootstrap caddy: %w", err)
	}

	grpcServer := grpc.NewServer()
	ingresspb.RegisterIngressServer(grpcServer, srv)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		if shErr := sup.Shutdown(); shErr != nil {
			slog.Debug(fmt.Sprintf("caddy shutdown: %v", shErr))
		}
	}()

	slog.Info(fmt.Sprintf("ingress: serving gRPC on %s, https vhosts on :%d, http vhosts on :%d", *socket, *port, *httpPort))
	return grpcServer.Serve(lis)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "town-os-ingress: %v\n", err)
		os.Exit(1)
	}
}
