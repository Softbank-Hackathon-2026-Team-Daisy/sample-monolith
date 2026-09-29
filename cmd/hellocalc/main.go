// Command hellocalc runs the HelloCalc HTTP server.
//
// Usage:
//
//	hellocalc              start the server (configured via environment)
//	hellocalc healthcheck  probe /health on the local server; exit 0 if healthy
//	hellocalc version      print build metadata as JSON
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Seungjun1127/HelloCalc/internal/buildinfo"
	"github.com/Seungjun1127/HelloCalc/internal/config"
	"github.com/Seungjun1127/HelloCalc/internal/httpserver"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hellocalc:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	switch {
	case len(args) == 0:
		return serve(cfg)
	case len(args) == 1 && args[0] == "healthcheck":
		return healthcheck(cfg)
	case len(args) == 1 && args[0] == "version":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(buildinfo.Get())
	default:
		return fmt.Errorf("unknown arguments %q (usage: hellocalc [healthcheck|version])", args)
	}
}

func serve(cfg config.Config) error {
	logger := httpserver.NewLogger(os.Stdout, cfg.LogLevel)
	info := buildinfo.Get()
	logger.Info("starting",
		slog.String("name", info.Name),
		slog.String("version", info.Version),
		slog.String("commit", info.Commit),
		slog.String("build_time", info.BuildTime),
		slog.String("go_version", info.GoVersion),
		slog.String("addr", cfg.Addr()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		logger.Error("listen failed", slog.String("addr", cfg.Addr()), slog.String("error", err.Error()))
		return err
	}

	// Once the first signal arrives, restore default signal handling so a
	// second signal terminates immediately.
	context.AfterFunc(ctx, stop)

	srv := httpserver.New(logger, info)
	if err := srv.Serve(ctx, ln, cfg.ShutdownTimeout); err != nil {
		logger.Error("server error", slog.String("error", err.Error()))
		return err
	}
	return nil
}

// healthcheck lets minimal images without a shell or curl run a container
// health check against the local server.
func healthcheck(cfg config.Config) error {
	host := cfg.Host
	switch host {
	case "0.0.0.0", "":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port)) + "/health"

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d", url, resp.StatusCode)
	}
	return nil
}
