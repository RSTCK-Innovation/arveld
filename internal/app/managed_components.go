package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

type managedComponentsRunner func(context.Context, chan<- struct{}) error

const (
	managedComponentsStarting int32 = iota
	managedComponentsReady
	managedComponentsStopped
)

type managedComponentsState struct {
	status atomic.Int32
}

func (state *managedComponentsState) markReady() bool {
	return state.status.CompareAndSwap(
		managedComponentsStarting,
		managedComponentsReady,
	)
}

func (state *managedComponentsState) markStarting() {
	state.status.Store(managedComponentsStarting)
}

func (state *managedComponentsState) markStopped() {
	state.status.Store(managedComponentsStopped)
}

func (state *managedComponentsState) ready() bool {
	return state.status.Load() == managedComponentsReady
}

func startManagedComponents(
	ctx context.Context,
	logger *slog.Logger,
	run managedComponentsRunner,
	state *managedComponentsState,
) func() {
	managedCtx, stop := context.WithCancel(ctx)
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		defer state.markStopped()

		var restartDelay time.Duration
		for {
			state.markStarting()
			ready := make(chan struct{})
			attemptStopped := make(chan struct{})
			readinessFinished := make(chan struct{})
			go func() {
				defer close(readinessFinished)
				select {
				case <-ready:
					if state.markReady() {
						logger.InfoContext(managedCtx, "managed components ready")
					}
				case <-attemptStopped:
				case <-managedCtx.Done():
				}
			}()

			runErr := run(managedCtx, ready)
			close(attemptStopped)
			<-readinessFinished
			if managedCtx.Err() != nil {
				return
			}

			state.markStarting()
			logger.ErrorContext(
				managedCtx,
				"managed components stopped",
				"error",
				runErr,
			)
			logger.InfoContext(
				managedCtx,
				"restarting managed components",
				"delay",
				restartDelay,
			)
			if err := waitForManagedComponentsRestart(managedCtx, restartDelay); err != nil {
				return
			}
			restartDelay = nextManagedComponentsRestartDelay(restartDelay)
		}
	}()

	return func() {
		stop()
		<-stopped
	}
}

func waitForManagedComponentsRestart(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for managed component restart: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func nextManagedComponentsRestartDelay(current time.Duration) time.Duration {
	switch current { //nolint:exhaustive // duration is a bounded state, not an enum
	case 0:
		return time.Second
	case time.Second:
		return 2 * time.Second
	case 2 * time.Second:
		return 5 * time.Second
	case 5 * time.Second:
		return 10 * time.Second
	default:
		return 30 * time.Second
	}
}
