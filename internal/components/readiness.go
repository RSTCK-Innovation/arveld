package components

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	readinessPollInterval   = 100 * time.Millisecond
	readinessRequestTimeout = time.Second
)

// ReadinessProbe checks the current readiness of a component endpoint.
type ReadinessProbe struct {
	client *http.Client
}

// NewReadinessProbe creates a readiness probe with a bounded request timeout.
func NewReadinessProbe() *ReadinessProbe {
	return &ReadinessProbe{
		client: &http.Client{Timeout: readinessRequestTimeout},
	}
}

// Ready reports whether endpoint currently accepts traffic.
func (probe *ReadinessProbe) Ready(
	ctx context.Context,
	endpoint string,
) bool {
	return probe.check(ctx, endpoint) == nil
}

func (probe *ReadinessProbe) waitReady(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(readinessPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		lastErr = probe.check(ctx, endpoint)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for readiness at %q: %w",
				endpoint,
				errors.Join(
					ctx.Err(),
					fmt.Errorf("last probe: %w", lastErr),
				),
			)
		case <-ticker.C:
		}
	}
}

func (probe *ReadinessProbe) check(
	ctx context.Context,
	endpoint string,
) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create readiness request: %w", err)
	}

	response, err := probe.client.Do(request)
	if err != nil {
		return fmt.Errorf("perform readiness request: %w", err)
	}
	defer func() {
		_ = response.Body.Close() //nolint:errcheck // readiness depends on the HTTP status
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness endpoint returned %s", response.Status)
	}

	return nil
}
