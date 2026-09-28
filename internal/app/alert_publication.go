package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

// startAlertPublication retries desired configuration while the managed
// components are ready until it is loaded.
// It never evaluates Monitor measurements or advances an alert timer.
func startAlertPublication(
	ctx context.Context,
	publisher *alert.Publisher,
	store *alert.Store,
	client *prometheus.Client,
	managedState *managedComponentsState,
	logger *slog.Logger,
) func() {
	publicationCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if publicationCtx.Err() != nil {
				return
			}
			if managedState.ready() {
				attemptCtx, stopAttempt := context.WithTimeout(publicationCtx, 30*time.Second)
				err := publisher.Sync(attemptCtx)
				if err == nil {
					err = alert.ObserveIncidents(attemptCtx, store, client)
				}
				stopAttempt()
				if err != nil && publicationCtx.Err() == nil {
					logger.ErrorContext(publicationCtx, "reconcile alert rules and incidents", "error", err)
				}
			}
			select {
			case <-publicationCtx.Done():
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
