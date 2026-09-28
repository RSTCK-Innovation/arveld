package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestHealthzReturnsOK(t *testing.T) {
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/healthz",
		nil,
	)
	response := httptest.NewRecorder()

	newTestHandlerWithReadiness(DependencyReadiness{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}

	if got, want := response.Body.String(), "{\"status\":\"healthy\"}"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	server := httptest.NewServer(newTestHandlerWithReadiness(DependencyReadiness{}))
	t.Cleanup(server.Close)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodHead, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	head, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(head.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := head.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if head.StatusCode != http.StatusOK || len(body) != 0 || head.ContentLength != int64(response.Body.Len()) || head.Header.Get("Content-Type") != response.Header().Get("Content-Type") {
		t.Fatalf("HEAD response = %d, length %d, headers %v, body %q", head.StatusCode, head.ContentLength, head.Header, body)
	}
}

func TestRoutingDispatchesMethods(t *testing.T) {
	handler := NewHandler(Config{}, Dependencies{OpAMP: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})})
	for _, method := range []string{"CONNECT", "DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT", "TRACE"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/v1/opamp", nil))
		if response.Code != http.StatusNoContent {
			t.Errorf("OpAMP %s = %d, want 204", method, response.Code)
		}
	}
	for _, test := range []struct {
		method, path, allow, body string
		status                    int
	}{
		{"POST", "/healthz", "GET, HEAD", "", 405},
		{"QUERY", "/v1/opamp", "CONNECT, DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT, TRACE", "", 405},
		{"CUSTOM", "/v1/opamp", "", "", 405},
		{"CUSTOM", "/missing", "", "", 405},
		{"GET", "/missing", "", "404 page not found\n", 404},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil))
			allowed := response.Header().Values("Allow")
			slices.Sort(allowed)
			if response.Code != test.status || strings.Join(allowed, ", ") != test.allow || response.Body.String() != test.body {
				t.Fatalf("fallback = %d, Allow %q, body %q", response.Code, allowed, response.Body.String())
			}
		})
	}
}

func TestReadyzIdentifiesUnavailablePrometheus(t *testing.T) {
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/readyz",
		nil,
	)
	response := httptest.NewRecorder()
	handler := newTestHandlerWithReadiness(DependencyReadiness{
		Prometheus:   false,
		Alertmanager: true,
	})

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusServiceUnavailable,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"version\":\"v0.1.0-rc.2\",\"ready\":false,\"components\":{\"arveld\":{\"ready\":true},\"prometheus\":{\"ready\":false},\"alertmanager\":{\"ready\":true}}}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func newTestHandlerWithReadiness(readiness DependencyReadiness) http.Handler {
	return NewHandler(Config{Version: "v0.1.0-rc.2", SecureCookie: true}, Dependencies{Logger: slog.New(slog.DiscardHandler), CheckReadiness: func(context.Context) DependencyReadiness { return readiness }})
}
