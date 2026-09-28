package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/notification"
)

// startNotificationPublication retries while the managed components are ready
// until the engine loads the channels.
func startNotificationPublication(
	ctx context.Context,
	publisher *notification.Publisher,
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
				stopAttempt()
				if err != nil && publicationCtx.Err() == nil {
					logger.ErrorContext(publicationCtx, "publish notification configuration", "error", err)
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
