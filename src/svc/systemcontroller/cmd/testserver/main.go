package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func withStaticFiles(api http.Handler, dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Serve index.html for root
			if r.URL.Path == "/" {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
			// Serve static file if it exists
			clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
			if !strings.Contains(clean, "..") {
				if _, err := os.Stat(filepath.Join(dir, clean)); err == nil {
					fileServer.ServeHTTP(w, r)
					return
				}
			}
		}
		// Fall through to API handler
		api.ServeHTTP(w, r)
	})
}

func run() (err error) {
	var staticDir string
	flag.StringVar(&staticDir, "static", "", "serve static files from directory")
	flag.Parse()

	dir, err := os.MkdirTemp("", "testserver-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil && err == nil {
			err = rerr
		}
	}()

	db, err := account.OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	acctMgr, err := account.InitManager(db)
	if err != nil {
		return fmt.Errorf("init account manager: %w", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, acctMgr, signingKey)
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		return fmt.Errorf("init audit manager: %w", err)
	}

	repoData, err := json.Marshal([]packages.Repository{})
	if err != nil {
		return fmt.Errorf("marshal empty repo list: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), repoData, 0644); err != nil {
		return fmt.Errorf("write repositories file: %w", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		return fmt.Errorf("init repository root: %w", err)
	}

	inst := packages.NewInstallManager(dir)

	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: "nginx.service", Description: "The NGINX HTTP Server", LoadState: "loaded", ActiveState: "active", SubState: "running"},
		{Name: "redis.service", Description: "Redis", LoadState: "loaded", ActiveState: "inactive", SubState: "dead"},
	}
	sd.Entries = []systemd.JournalEntry{
		{Message: "Starting nginx...", RealtimeTimestamp: time.Now().Add(-2 * time.Second), SystemdUnit: "nginx.service", Priority: "6"},
		{Message: "Started nginx.", RealtimeTimestamp: time.Now().Add(-time.Second), SystemdUnit: "nginx.service", Priority: "6"},
	}

	var handler http.Handler = systemcontroller.NewHandler(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		AccountMgr:     acctMgr,
		SessionMgr:     sessMgr,
		AuditMgr:       auditMgr,
	})

	if staticDir != "" {
		handler = withStaticFiles(handler, staticDir)
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		cancel()
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	fmt.Fprintln(os.Stderr, "listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}

	_ = ctx
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "testserver: %v\n", err)
		os.Exit(1)
	}
}
