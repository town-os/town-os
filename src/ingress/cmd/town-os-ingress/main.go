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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"gitea.com/town-os/town-os/src/caddysup"
	"gitea.com/town-os/town-os/src/i18n"
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
	metricsPort := flag.Int("metrics-port", ingress.DefaultMetricsPort,
		"TCP port to serve the Prometheus endpoint on (0 disables it)")
	adminPort := flag.Int("caddy-admin-port", ingress.DefaultAdminPort,
		"TCP port for caddy's loopback admin API, which `caddy reload` and the metrics passthrough use (2019 in production, where the ingress has its own netns; ephemeral when caddy shares a namespace with another caddy)")
	locale := flag.String("locale", i18n.DefaultLocale,
		"language the retry page falls back to when a client asks for one Town OS has no catalog for (the box's `locale` setting; a code with no catalog is ignored)")
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
	srv := ingress.NewServer(sup, *port, *httpPort, *defaultBackend,
		ingress.WithCaddyAdminPort(*adminPort), ingress.WithDefaultLocale(*locale))
	// Start caddy with the initial (empty) config so the supervisor is live
	// before the first route arrives.
	if err := srv.Bootstrap(); err != nil {
		return fmt.Errorf("bootstrap caddy: %w", err)
	}

	grpcServer := grpc.NewServer()
	ingresspb.RegisterIngressServer(grpcServer, srv)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	metricsSrv := startMetrics(ctx, srv, *metricsPort)

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		if metricsSrv != nil {
			// Its own context: ctx is already cancelled by the time this runs,
			// and a cancelled one makes Shutdown return immediately without
			// draining the scrape that may be in flight.
			shutCtx, shutCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer shutCancel()
			if mErr := metricsSrv.Shutdown(shutCtx); mErr != nil {
				slog.Debug(fmt.Sprintf("metrics shutdown: %v", mErr))
			}
		}
		if shErr := sup.Shutdown(); shErr != nil {
			slog.Debug(fmt.Sprintf("caddy shutdown: %v", shErr))
		}
	}()

	slog.Info(fmt.Sprintf("ingress: serving gRPC on %s, https vhosts on :%d, http vhosts on :%d", *socket, *port, *httpPort))
	return grpcServer.Serve(lis)
}

// startMetrics brings up the Prometheus endpoint on its own listener, returning
// nil when it is disabled (port 0) or could not bind.
//
// A failure to bind is logged and survived rather than returned. The ingress's
// job is to route traffic; refusing to start it because nothing can scrape it
// would turn a monitoring gap into an outage — and the port is published to the
// host loopback, where a leftover container from a previous run is exactly the
// kind of transient collision that must not take the box's router down.
func startMetrics(ctx context.Context, srv *ingress.Server, port int) *http.Server {
	if port == 0 {
		slog.Info("ingress: metrics endpoint disabled (--metrics-port=0)")
		return nil
	}
	addr := net.JoinHostPort("", strconv.Itoa(port))
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		slog.Error(fmt.Sprintf("ingress: metrics endpoint not serving: %v", err))
		return nil
	}
	// ReadHeaderTimeout is set because the default is none: a client that opens
	// a connection and never finishes its headers would otherwise hold it
	// forever.
	metricsSrv := &http.Server{Handler: srv.MetricsHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if serveErr := metricsSrv.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error(fmt.Sprintf("ingress: metrics endpoint stopped: %v", serveErr))
		}
	}()
	slog.Info(fmt.Sprintf("ingress: serving %s on :%d", ingress.MetricsPath, port))
	return metricsSrv
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "town-os-ingress: %v\n", err)
		os.Exit(1)
	}
}
