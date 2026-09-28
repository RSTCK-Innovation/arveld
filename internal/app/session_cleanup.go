package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

const (
	sessionCleanupInterval = 5 * time.Minute
	sessionCleanupTimeout  = 5 * time.Second
)

// startSessionCleanup returns a function that cancels cleanup and waits for it
// to stop, so the caller can safely close the database afterwards.
func startSessionCleanup(ctx context.Context, store *auth.SessionStore, logger *slog.Logger) func() {
	cleanupCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()

		for {
			if cleanupCtx.Err() != nil {
				return
			}
			sweepCtx, stopSweep := context.WithTimeout(cleanupCtx, sessionCleanupTimeout)
			removed, err := store.DeleteExpired(sweepCtx)
			stopSweep()
			if err != nil && cleanupCtx.Err() == nil {
				logger.ErrorContext(cleanupCtx, "remove expired sessions", "error", err)
			} else if removed > 0 {
				logger.InfoContext(cleanupCtx, "expired sessions removed", "count", removed)
			}

			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return func() {
		cancel()
		<-stopped
	}
}
