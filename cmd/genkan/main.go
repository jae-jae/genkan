// Command genkan is a lightweight self-hosted homelab navigation page.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the scratch image has no zoneinfo; plugins (world-clock) use IANA time zones

	"github.com/jae-jae/genkan/internal/auth"
	"github.com/jae-jae/genkan/internal/favicon"
	"github.com/jae-jae/genkan/internal/health"
	"github.com/jae-jae/genkan/internal/server"
	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/web"
)

var version = "dev"

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "version", "-v", "--version":
			fmt.Println("genkan", version)
			return
		}
	}
	if err := run(); err != nil {
		log.Fatalf("genkan: %v", err)
	}
}

// healthcheck probes the local /healthz; used by Docker HEALTHCHECK in a scratch image.
func healthcheck() int {
	addr := env("GENKAN_ADDR", ":8080")
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = strings.TrimPrefix(addr, ":")
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func run() error {
	// Small, mostly idle service: keep the heap tight.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(128 << 20)
	}

	addr := env("GENKAN_ADDR", ":8080")
	dataDir := env("GENKAN_DATA", "./data")

	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open data: %w", err)
	}
	if pw := os.Getenv("GENKAN_PASSWORD"); pw != "" && st.SetupRequired() {
		if msg := auth.ValidatePassword(pw); msg != "" {
			return fmt.Errorf("GENKAN_PASSWORD: %s", msg)
		}
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		if _, err := st.SetInitialPassword(hash); err != nil {
			return err
		}
		log.Printf("admin password initialized from GENKAN_PASSWORD")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var srv *server.Server
	hc := health.New(st, func(m map[string]health.Result) {
		if srv != nil {
			srv.PublishHealth(m)
		}
	})
	srv = server.New(server.Config{
		Store:       st,
		Health:      hc,
		Favicon:     favicon.NewFetcher(filepath.Join(dataDir, "uploads", "icons")),
		DataDir:     dataDir,
		Web:         web.FS(),
		Version:     version,
		GitHubToken: env("GENKAN_GITHUB_TOKEN", ""),
	})

	hcDone := make(chan struct{})
	go func() { defer close(hcDone); hc.Run(ctx) }()

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second, // SSE and uploads lift this per request
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	// Shutdown waits for handlers to return; end SSE streams first.
	httpSrv.RegisterOnShutdown(srv.Broker().Close)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()
	log.Printf("genkan %s listening on %s (data: %s)", version, ln.Addr(), dataDir)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Printf("shutting down...")
	}

	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Broker().Close()
	if err := httpSrv.Shutdown(sctx); err != nil {
		log.Printf("http shutdown: %v", err)
		_ = httpSrv.Close()
	}
	stop()
	srv.Close()
	<-hcDone
	if err := st.Close(); err != nil {
		return fmt.Errorf("flush data: %w", err)
	}
	log.Printf("bye")
	return nil
}
