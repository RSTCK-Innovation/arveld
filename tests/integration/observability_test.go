package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/internal/prometheus"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHTTPServerRoutesLargeMetricsBodies(t *testing.T) {
	expression := `sum(up{instance=~"` + strings.Repeat("agent|", 3000) + `last"})`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.RequestURI) > 8192 {
			http.Error(w, "request line too long", http.StatusRequestURITooLong)
			return
		}
		if r.Method != http.MethodPost || r.FormValue("query") != expression {
			t.Error("large expression was not forwarded in a POST form")
		}
		if r.FormValue("lookback_delta") != "7260" {
			t.Error("lookback was not forwarded")
		}
		if strings.HasSuffix(r.URL.Path, "query_range") &&
			(r.FormValue("start") != "100" || r.FormValue("end") != "200" || r.FormValue("step") != "15") {
			t.Error("range parameters were not forwarded")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	config := controllerConfig(t, upstream.URL)
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	origin, _ := startController(t, config)
	cookie := loginController(t, origin)
	for _, path := range []string{"query", "query_range"} {
		parameters := url.Values{"query": {expression}, "lookback_delta": {"7260"}}
		if path == "query_range" {
			parameters.Set("start", "100")
			parameters.Set("end", "200")
			parameters.Set("step", "15")
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			origin+"/api/v1/metrics/"+path, strings.NewReader(parameters.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, response.StatusCode)
		}
	}
}

func TestHTTPMetricsClientCancellationDoesNotLogErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	for _, target := range []string{
		"/api/v1/metrics/query?query=up",
		"/api/v1/metrics/query_range?query=up&start=100&end=200&step=15",
	} {
		for _, phase := range []string{"query", "response body"} {
			t.Run(target+"/"+phase, func(t *testing.T) {
				ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
				defer stop()
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				upstreamDone := make(chan struct{})
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(upstreamDone)
					// A POST query is parsed before evaluation. Consume its body so the
					// HTTP/1 server can observe the client's connection cancellation.
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Errorf("read query body: %v", err)
						return
					}
					if phase == "query" {
						cancel()
					} else {
						if _, err := io.WriteString(w, `{"status":"success","data":`); err != nil {
							t.Errorf("write upstream response: %v", err)
							return
						}
						if err := http.NewResponseController(w).Flush(); err != nil {
							t.Errorf("flush upstream response: %v", err)
							return
						}
					}
					<-r.Context().Done()
				}))
				t.Cleanup(upstream.Close)
				client, err := prometheus.NewClient(upstream.URL, 30*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				var logs bytes.Buffer
				dependencies := accountDependencies(db)
				dependencies.Prometheus = client
				dependencies.Logger = slog.New(slog.NewTextHandler(&logs, nil))
				handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, dependencies)
				request := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
				request.AddCookie(cookie)
				response := httptest.NewRecorder()
				if phase == "response body" {
					handler.ServeHTTP(cancelMetricsResponseWriter{response, cancel}, request)
				} else {
					handler.ServeHTTP(response, request)
				}
				if ctx.Err() != context.Canceled {
					t.Fatalf("request context error = %v, want client cancellation", ctx.Err())
				}
				select {
				case <-upstreamDone:
				case <-time.After(5 * time.Second):
					t.Fatal("client cancellation did not stop the upstream request")
				}
				if strings.Contains(logs.String(), "level=ERROR") {
					t.Errorf("client cancellation logged as a server error: %s", logs.String())
				}
			})
		}
	}
}

// cancelMetricsResponseWriter cancels only once the API starts sending the body.
type cancelMetricsResponseWriter struct {
	http.ResponseWriter
	cancel context.CancelFunc
}

func (w cancelMetricsResponseWriter) Write(body []byte) (int, error) {
	n, err := w.ResponseWriter.Write(body)
	w.cancel()
	if err != nil {
		return n, fmt.Errorf("write metrics response: %w", err)
	}
	return n, nil
}

func TestHTTPMetricsUpstreamFailuresRemainVisible(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	cookie := login(t, newAccountHandler(db), "a long password for testing")
	for _, target := range []string{
		"/api/v1/metrics/query?query=up",
		"/api/v1/metrics/query_range?query=up&start=100&end=200&step=15",
	} {
		for _, test := range []struct {
			name, message, failure string
			status                 int
		}{
			{"connection refused", "query Prometheus", "connection refused", http.StatusBadGateway},
			{"query timeout", "query Prometheus", "deadline exceeded", http.StatusBadGateway},
			{"truncated response", "write Prometheus query response", "unexpected EOF", http.StatusOK},
		} {
			t.Run(target+"/"+test.name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Errorf("read query body: %v", err)
						return
					}
					if test.name == "query timeout" {
						<-r.Context().Done()
						return
					}
					w.Header().Set("Content-Length", "100")
					if _, err := io.WriteString(w, `{"status":"success","data":`); err != nil {
						t.Errorf("write upstream response: %v", err)
					}
				}))
				t.Cleanup(upstream.Close)
				if test.name == "connection refused" {
					upstream.Close()
				}
				client, err := prometheus.NewClient(upstream.URL, 500*time.Millisecond)
				if err != nil {
					t.Fatal(err)
				}
				var logs bytes.Buffer
				dependencies := accountDependencies(db)
				dependencies.Prometheus = client
				dependencies.Logger = slog.New(slog.NewTextHandler(&logs, nil))
				handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, dependencies)
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
				request.AddCookie(cookie)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != test.status {
					t.Errorf("upstream failure status = %d, want %d", response.Code, test.status)
				}
				if !strings.Contains(logs.String(), "level=ERROR") ||
					!strings.Contains(logs.String(), test.message) ||
					!strings.Contains(logs.String(), test.failure) {
					t.Errorf("upstream failure missing from error logs: %s", logs.String())
				}
			})
		}
	}
}

func TestHTTPServerRoutesInstantMetricsQueryToPrometheus(t *testing.T) {
	prometheusResponse := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	prometheusServer := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if got, want := request.FormValue("query"), "up"; got != want {
				t.Errorf("query = %q, want %q", got, want)
			}
			if got := request.FormValue("lookback_delta"); got != "7260" {
				t.Errorf("lookback_delta = %q, want 7260", got)
			}
			response.Header().Set("Content-Type", "application/json")
			if _, err := response.Write([]byte(prometheusResponse)); err != nil {
				t.Errorf("write Prometheus response: %v", err)
			}
		},
	))
	t.Cleanup(prometheusServer.Close)

	config := controllerConfig(t, prometheusServer.URL)
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		url+"/api/v1/metrics/query?query=up&lookback_delta=7260",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := response.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got := string(body); got != prometheusResponse {
		t.Errorf("body = %q, want %q", got, prometheusResponse)
	}
}

func TestHTTPServerRoutesRangeMetricsQueryToPrometheus(t *testing.T) {
	prometheusResponse := `{"status":"success","data":{"resultType":"matrix","result":[]}}`
	prometheusServer := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			parameters := request.PostForm
			if got, want := parameters.Get("query"), "up"; got != want {
				t.Errorf("query = %q, want %q", got, want)
			}
			if got, want := parameters.Get("start"), "100"; got != want {
				t.Errorf("start = %q, want %q", got, want)
			}
			if got, want := parameters.Get("end"), "200"; got != want {
				t.Errorf("end = %q, want %q", got, want)
			}
			if got, want := parameters.Get("step"), "15"; got != want {
				t.Errorf("step = %q, want %q", got, want)
			}
			if got := request.FormValue("lookback_delta"); got != "7260" {
				t.Errorf("lookback_delta = %q, want 7260", got)
			}
			response.Header().Set("Content-Type", "application/json")
			if _, err := response.Write([]byte(prometheusResponse)); err != nil {
				t.Errorf("write Prometheus response: %v", err)
			}
		},
	))
	t.Cleanup(prometheusServer.Close)

	config := controllerConfig(t, prometheusServer.URL)
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	url, _ := startController(t, config)
	cookie := loginController(t, url)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		url+"/api/v1/metrics/query_range?query=up&start=100&end=200&step=15&lookback_delta=7260",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := string(body); got != prometheusResponse {
		t.Errorf("body = %q, want %q", got, prometheusResponse)
	}
}
