package components

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForReadyRetriesUntilEndpointIsReady(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			if requests.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			response.WriteHeader(http.StatusOK)
		},
	))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	probe := NewReadinessProbe()
	if err := probe.waitReady(ctx, server.URL+"/-/ready"); err != nil {
		t.Fatalf("wait for readiness: %v", err)
	}
	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("readiness requests = %d, want %d", got, want)
	}
}

func TestReadinessProbeReportsCurrentEndpointState(t *testing.T) {
	var statusCode atomic.Int32
	statusCode.Store(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(int(statusCode.Load()))
		},
	))
	t.Cleanup(server.Close)

	probe := NewReadinessProbe()
	if probe.Ready(t.Context(), server.URL+"/-/ready") {
		t.Error("Ready() = true for a 503 response, want false")
	}

	statusCode.Store(http.StatusOK)
	if !probe.Ready(t.Context(), server.URL+"/-/ready") {
		t.Error("Ready() = false for a 200 response, want true")
	}
}
