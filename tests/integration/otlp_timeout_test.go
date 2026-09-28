package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestControllerBoundsOTLPMetricsTransfer(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "waiting for headers"
		if streaming {
			name = "streaming body"
		}
		t.Run(name, func(t *testing.T) {
			stopped := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				defer close(stopped)
				if streaming {
					response.WriteHeader(http.StatusOK)
					if err := http.NewResponseController(response).Flush(); err != nil {
						t.Errorf("flush upstream headers: %v", err)
						return
					}
				}
				<-request.Context().Done()
			}))
			t.Cleanup(upstream.Close)
			config := controllerConfig(t, upstream.URL)
			config.OTLPMetricsTimeout = 250 * time.Millisecond
			token := createControllerAgentKey(t, config.DatabasePath)
			url, _ := startController(t, config)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/otlp/v1/metrics", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+token)
			response := doControllerRequest(t, request)
			defer func() {
				if err := response.Body.Close(); err != nil {
					t.Errorf("close controller response: %v", err)
				}
			}()
			body, readErr := io.ReadAll(response.Body)
			if streaming {
				if response.StatusCode != http.StatusOK || !errors.Is(readErr, io.ErrUnexpectedEOF) {
					t.Fatalf("stalled response body = %d, %v; want interrupted stream", response.StatusCode, readErr)
				}
			} else if response.StatusCode != http.StatusBadGateway || readErr != nil || string(body) != "Bad Gateway\n" {
				t.Fatalf("stalled upstream headers = %d %q, %v; want generic 502", response.StatusCode, body, readErr)
			}
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("timed-out transfer did not stop at the upstream")
			}
		})
	}
}

func TestControllerCancelsOTLPTransferWhenCallerDisconnects(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		defer close(stopped)
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	config := controllerConfig(t, upstream.URL)
	config.OTLPMetricsTimeout = time.Minute
	token := createControllerAgentKey(t, config.DatabasePath)
	url, _ := startController(t, config)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/otlp/v1/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	finished := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Do(request)
		if response != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Errorf("close canceled response: %v", closeErr)
			}
		}
		finished <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("caller request = %v, want cancellation", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("caller request did not stop")
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("OTLP request did not reach upstream")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation did not stop the upstream transfer")
	}
}
