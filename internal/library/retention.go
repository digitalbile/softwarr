package library

import (
	"context"
	"log/slog"
	"time"
)

const retentionInterval = 24 * time.Hour

// RunRetention applies retention at startup and then every 24 hours until
// ctx is done. A run that has started finishes before RunRetention returns.
func (s *Service) RunRetention(ctx context.Context) {
	run := func() {
		if n, err := s.ApplyRetention(ctx); err != nil {
			slog.Error("library: retention", "err", err)
		} else if n > 0 {
			slog.Info("library: expired items", "count", n)
		}
	}
	run()
	t := time.NewTicker(retentionInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
