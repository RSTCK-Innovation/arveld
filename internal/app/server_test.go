package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/opamp"
)

func TestServeFailureWaitsForActiveRequests(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	var finishOnce sync.Once
	release := func() { finishOnce.Do(func() { close(finish) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-finish:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	defer release()
	protocol, err := opamp.NewServer(nil, nil, testLogger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := &applicationServer{http: upstream.Config, opamp: protocol}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	requestDone := make(chan error, 1)
	go func() {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
		if requestErr != nil {
			requestDone <- requestErr
			return
		}
		response, requestErr := upstream.Client().Do(request)
		if requestErr == nil {
			requestErr = response.Body.Close()
		}
		requestDone <- requestErr
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("request did not start")
	}
	serveErr := errors.New("listener failed")
	serveErrors := make(chan error, 1)
	serveErrors <- serveErr
	stopped := make(chan error, 1)
	go func() { stopped <- server.wait(ctx, serveErrors, testLogger()) }()
	select {
	case err := <-stopped:
		t.Fatalf("server returned before its active request finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-stopped:
		if !errors.Is(err, serveErr) {
			t.Fatalf("server error = %v, want original listener error", err)
		}
	case <-ctx.Done():
		t.Fatal("server did not finish shutdown")
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("active request failed: %v", err)
	}
}
