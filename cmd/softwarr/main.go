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
	_ "github.com/digitalbile/softwarr/internal/downloadclient/qbittorrent"
	"github.com/digitalbile/softwarr/internal/events"
	"github.com/digitalbile/softwarr/internal/httpapi"
	"github.com/digitalbile/softwarr/internal/notify"
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
	sseDone := make(chan struct{})
	handler := httpapi.New(httpapi.Deps{
		DB:       database,
		Q:        q,
		Settings: st,
		Users:    auth.NewUsers(database, q),
		Sessions: auth.NewSessions(q, time.Now),
		OIDC:     auth.NewOIDC(st, q, database, time.Now),
		Broker:   broker,
		Notify:   notifier,
		Requests: requests.NewService(database, q, st, notifier, broker, time.Now),
		Done:     sseDone,
		URLBase:  cfg.URLBase,
		Version:  version,
		Now:      time.Now,
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Port), Handler: handler}

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
		return fmt.Errorf("server failed: %w", err)
	}
	<-drained
	return nil
}
