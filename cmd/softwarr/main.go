package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/digitalbile/softwarr/internal/httpapi"
	"github.com/digitalbile/softwarr/internal/settings"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("softwarr failed", "err", err)
		os.Exit(1)
	}
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

	handler := httpapi.New(httpapi.Deps{
		DB:       database,
		Q:        q,
		Settings: settings.NewStore(q),
		Users:    auth.NewUsers(database, q),
		Sessions: auth.NewSessions(q, time.Now),
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
