// Command groundtruth runs the groundtruth server: a self-hosted drift
// detector for Terraform and OpenTofu.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/buildinfo"
	"github.com/carlosalbertorg/groundtruth/internal/config"
	"github.com/carlosalbertorg/groundtruth/internal/httpapi"
	"github.com/carlosalbertorg/groundtruth/internal/webassets"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()

	spa, err := webassets.Dist()
	if err != nil {
		return err
	}

	handler := httpapi.NewRouter(spa, logger)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("groundtruth starting",
			"addr", cfg.Addr,
			"version", buildinfo.Version,
			"commit", buildinfo.Commit,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return <-serveErr
}
