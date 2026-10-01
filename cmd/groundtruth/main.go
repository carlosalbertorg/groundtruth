// Command groundtruth runs the groundtruth server: a self-hosted drift
// detector for Terraform and OpenTofu.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/alerting"
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
	// A check killed outright (SIGKILL, OOM) never ran its deferred cleanup,
	// and its scratch directory can hold an unredacted plan file.
	removed, err := executor.SweepStale()
	if err != nil {
		logger.Warn("could not remove every stale check directory; remove them manually", "dir", cfg.CheckTmpDir(), "error", err)
	}
	if removed > 0 {
		logger.Info("removed check directories left behind by an earlier run", "count", removed)
	}
	checkService := checks.NewService(queries, executor)
	checkService.SetNotifier(alerting.NewDispatcher(queries, logger, cfg.BaseURL))

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
		APITokens:     auth.NewAPITokenManager(queries),
		SecureCookies: cfg.SecureCookies(),
	})

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
		// Every request's context derives from ctx, so SIGTERM cancels a
		// check running inside a "check now" request just as it cancels one
		// started by the scheduler. Without it such a check would keep
		// running through the shutdown grace period and then be cut off
		// mid-flight, skipping its deferred cleanup of the scratch
		// directory (which can hold an unredacted plan file).
		BaseContext:       func(net.Listener) context.Context { return ctx },
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
