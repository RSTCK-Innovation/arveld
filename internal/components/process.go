package components

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

const managedProcessShutdownTimeout = 5 * time.Second

type processSpec struct {
	component  componentName
	executable string
	arguments  []string
}

type processRunner func(context.Context, processSpec, *slog.Logger) error

type restartWaiter func(context.Context, time.Duration) error

func runProcessOnce(
	ctx context.Context,
	spec processSpec,
	logger *slog.Logger,
) error {
	//nolint:gosec // executable path comes from the verified component installer
	cmd := exec.CommandContext(ctx, spec.executable, spec.arguments...)
	configureProcess(cmd)
	cmd.WaitDelay = managedProcessShutdownTimeout
	componentLogger := logger.With("component", spec.component)
	stdout := newProcessLogWriter(ctx, componentLogger.With("stream", "stdout"))
	stderr := newProcessLogWriter(ctx, componentLogger.With("stream", "stderr"))
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	defer stdout.flush(false)
	defer stderr.flush(false)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wait for process: %w", err)
	}

	return nil
}

// superviseProcess restarts one executable after any exit until ctx is canceled.
// Installation and whole-group startup retries belong to the application.
func superviseProcess(
	ctx context.Context,
	spec processSpec,
	run processRunner,
	wait restartWaiter,
	logger *slog.Logger,
) error {
	var restartDelay time.Duration

	for {
		runErr := run(ctx, spec, logger)

		if ctx.Err() != nil {
			return nil //nolint:nilerr // process termination is expected during shutdown
		}
		logger.ErrorContext(
			ctx,
			"process exited",
			"component",
			spec.component,
			"error",
			runErr,
		)
		logger.InfoContext(
			ctx,
			"restarting",
			"component",
			spec.component,
			"delay",
			restartDelay,
		)

		if err := wait(ctx, restartDelay); err != nil {
			if ctx.Err() != nil {
				return nil //nolint:nilerr // cancellation during backoff is a clean shutdown
			}

			return fmt.Errorf("wait for restart: %w", err)
		}

		switch restartDelay { //nolint:exhaustive // duration is a bounded state, not an enum
		case 0:
			restartDelay = time.Second
		case time.Second:
			restartDelay = 2 * time.Second
		case 2 * time.Second:
			restartDelay = 5 * time.Second
		case 5 * time.Second:
			restartDelay = 10 * time.Second
		default:
			restartDelay = 30 * time.Second
		}
	}
}

func waitForRestart(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("restart wait interrupted: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
