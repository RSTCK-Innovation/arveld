package prometheus_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
)

func TestRuleOperationsHonorTimeoutAndCancellation(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		<-r.Context().Done()
	}))
	t.Cleanup(engine.Close)
	client, err := prometheus.NewClient(engine.URL, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"validate", func(ctx context.Context) error { return client.ValidateExpression(ctx, "vector(0)") }},
		{"reload", client.Reload},
		{"read", func(ctx context.Context) error {
			if _, err := client.AlertRules(ctx, "arveld-monitor-alerts"); err != nil {
				return fmt.Errorf("read native rules: %w", err)
			}
			return nil
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("operation did not preserve its timeout: %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := operation.run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation did not preserve caller cancellation: %v", err)
			}
		})
	}
}
