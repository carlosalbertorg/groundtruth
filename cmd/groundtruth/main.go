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

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/buildinfo"
	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/config"
	"github.com/carlosalbertorg/groundtruth/internal/httpapi"
	"github.com/carlosalbertorg/groundtruth/internal/scheduler"
	"github.com/carlosalbertorg/groundtruth/internal/store"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
	"github.com/carlosalbertorg/groundtruth/internal/webassets"
)

// schedulerPollInterval is how often the scheduler checks for due
// workspaces - not how often any single workspace gets checked (that's
// each workspace's own check_interval_minutes).
const schedulerPollInterval = 30 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("closing database", "error", err)
		}
	}()

	if err := store.Migrate(db); err != nil {
		return err
	}

	queries := sqlc.New(db)

	executor, err := terraform.NewExecutor(cfg.CheckTmpDir(), cfg.PluginCacheDir())
	if err != nil {
		return err
	}
	checkService := checks.NewService(queries, executor)

	spa, err := webassets.Dist()
	if err != nil {
		return err
	}

	handler := httpapi.NewRouter(httpapi.Deps{
		SPA:           spa,
		Logger:        logger,
		Queries:       queries,
		CheckService:  checkService,
		Sessions:      auth.NewSessionManager(queries),
		SetupGate:     auth.NewSetupGate(queries),
		SecureCookies: cfg.SecureCookies(),
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	sched := scheduler.New(queries, checkService, logger, schedulerPollInterval, cfg.MaxConcurrentChecks)
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		sched.Run(ctx)
	}()

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

	// sched.Run already observed ctx.Done() above and is unwinding its
	// own in-flight checks (killed via that same cancelled context) -
	// wait for it too, bounded by the same shutdown grace period, so a
	// check's goroutine doesn't touch the database after it's closed.
	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		logger.Warn("scheduler did not stop within the shutdown grace period")
	}

	return <-serveErr
}
