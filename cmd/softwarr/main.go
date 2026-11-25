package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/digitalbile/softwarr/internal/auth"
	"github.com/digitalbile/softwarr/internal/config"
	"github.com/digitalbile/softwarr/internal/db"
	"github.com/digitalbile/softwarr/internal/db/dbq"
	"github.com/digitalbile/softwarr/internal/downloadclient"
	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/health"
	"github.com/digitalbile/softwarr/internal/httpapi"
	"github.com/digitalbile/softwarr/internal/library"
	"github.com/digitalbile/softwarr/internal/logbuf"
	"github.com/digitalbile/softwarr/internal/notify"
	"github.com/digitalbile/softwarr/internal/pathmap"
	"github.com/digitalbile/softwarr/internal/requests"
	"github.com/digitalbile/softwarr/internal/settings"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if err := resetPasswordMain(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("softwarr failed", "err", err)
		os.Exit(1)
	}
}

func resetPasswordMain() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return resetPasswordCmd(context.Background(), cfg, os.Stdout)
}

// resetPasswordCmd resets the owner's password and prints the temporary one.
// It does not start the server.
func resetPasswordCmd(ctx context.Context, cfg config.Config, stdout io.Writer) error {
	database, err := db.Open(filepath.Join(cfg.DataDir, "softwarr.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	q := dbq.New(database)
	pw, err := auth.ResetOwnerPassword(ctx, database, q)
	if err != nil {
		return err
	}
	owner, err := auth.NewUsers(database, q).Owner(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "The owner password has been reset.\nUsername: %s\nTemporary password: %s\nYou'll be asked to choose a new password after signing in.\n", owner.Username, pw)
	return nil
}

func run() error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	logs := logbuf.New(1000, slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(slog.New(logs))
	startedAt := time.Now()
	slog.Info("starting softwarr", "version", version, "port", cfg.Port, "data_dir", cfg.DataDir, "url_base", cfg.URLBase)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(filepath.Join(cfg.DataDir, "softwarr.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	q := dbq.New(database)
	st := settings.NewStore(q)

	broker := events.NewBroker()
	notifier := notify.New(q, broker, st, &http.Client{Timeout: 10 * time.Second}, time.Now)
	requestSvc := requests.NewService(database, q, st, notifier, broker, time.Now)
	worker := requests.NewWorker(requestSvc, q, st, &http.Client{Timeout: 60 * time.Second}, nil, time.Now)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, 15*time.Second)
	}()
	librarySvc := library.NewService(q, st, notifier, broker, time.Now)
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		librarySvc.RunRetention(ctx)
	}()
	sessions := auth.NewSessions(q, time.Now)
	purgeDone := make(chan struct{})
	go func() {
		defer close(purgeDone)
		sessions.RunPurge(ctx, 24*time.Hour)
	}()
	checker := health.NewChecker(q, st, nil, nil, notifier, broker)
	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		checker.Loop(ctx, 5*time.Minute)
	}()
	sseDone := make(chan struct{})
	handler := httpapi.New(httpapi.Deps{
		Settings:        st,
		Users:           auth.NewUsers(database, q),
		Sessions:        sessions,
		OIDC:            auth.NewOIDC(st, q, database, time.Now),
		Broker:          broker,
		Notify:          notifier,
		Requests:        requestSvc,
		Library:         librarySvc,
		Health:          checker,
		DownloadClients: downloadclient.NewStore(q),
		PathMappings:    pathmap.NewStore(q),
		Logs:            logs,
		Done:            sseDone,
		URLBase:         cfg.URLBase,
		DataDir:         cfg.DataDir,
		StartedAt:       startedAt,
		Version:         version,
		Now:             time.Now,
	})
	srv := newHTTPServer(cfg.Port, handler)

	// Wait for Shutdown to finish draining before returning.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		close(sseDone) // end open SSE streams so Shutdown can drain
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "err", err)
		}
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		stop()
		<-workerDone
		<-retentionDone
		<-healthDone
		<-purgeDone
		return fmt.Errorf("server failed: %w", err)
	}
	<-drained
	<-purgeDone
	<-healthDone
	<-retentionDone
	<-workerDone // let an in-flight tick finish before the database closes
	return nil
}

// newHTTPServer builds the server for handler. ReadHeaderTimeout stops a
// client that sends headers slowly from holding a connection open. There is
// no write timeout: SSE streams and library downloads run for a long time.
func newHTTPServer(port int, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
