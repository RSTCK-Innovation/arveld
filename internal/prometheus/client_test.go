package prometheus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestClientKeepsBaseQueryParametersInURL(t *testing.T) {
	for _, path := range []string{instantQueryPath, rangeQueryPath} {
		t.Run(path, func(t *testing.T) {
			const baseQuery = "tenant=x&tenant=y&route=a%2Fb+space"
			wantBody := url.Values{"query": {"up"}, "lookback_delta": {"65"}}
			if path == rangeQueryPath {
				wantBody.Set("start", "0")
				wantBody.Set("end", "60")
				wantBody.Set("step", "15")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/prometheus"+path {
					t.Errorf("endpoint = %s %s, want POST /prometheus%s", r.Method, r.URL.Path, path)
				}
				if r.URL.RawQuery != baseQuery {
					t.Errorf("URL query = %q, want %q", r.URL.RawQuery, baseQuery)
				}
				if err := r.ParseForm(); err != nil {
					t.Errorf("parse form: %v", err)
					return
				}
				if !reflect.DeepEqual(r.PostForm, wantBody) {
					t.Errorf("POST form = %v, want only API parameters %v", r.PostForm, wantBody)
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)
			client, err := NewClient(server.URL+"/prometheus?"+baseQuery, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var response *http.Response
			if path == rangeQueryPath {
				response, err = client.QueryRange(t.Context(), RangeQuery{
					Expression: "up", Start: "0", End: "60", Step: "15", LookbackDelta: "65",
				})
			} else {
				response, err = client.Query(t.Context(), "up", "65")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type stalledTransport struct{}

func (stalledTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	return nil, fmt.Errorf("stalled query: %w", request.Context().Err())
}

func TestClientBoundsStalledQueries(t *testing.T) {
	for _, timeout := range []time.Duration{30 * time.Second, 7 * time.Second, 2 * time.Minute} {
		for _, rangeQuery := range []bool{false, true} {
			synctest.Test(t, func(t *testing.T) {
				client, err := NewClient("http://prometheus.example", timeout)
				if err != nil {
					t.Fatal(err)
				}
				httpClient := *client.httpClient
				httpClient.Transport = stalledTransport{}
				client.httpClient = &httpClient
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				start := time.Now()
				var response *http.Response
				if rangeQuery {
					response, err = client.QueryRange(ctx, RangeQuery{Expression: "up", Start: "0", End: "1", Step: "1"})
				} else {
					response, err = client.Query(ctx, "up", "")
				}
				if response != nil {
					if closeErr := response.Body.Close(); closeErr != nil {
						t.Error(closeErr)
					}
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("query error = %v, want deadline exceeded", err)
				}
				if elapsed := time.Since(start); elapsed != min(timeout, time.Minute) {
					t.Errorf("query duration = %s, want %s", elapsed, min(timeout, time.Minute))
				}
			})
		}
	}
}

func TestClientQuerySendsInstantPromQLRequest(t *testing.T) {
	expression := `rate(http_requests_total{method="GET"}[5m])`
	prometheusResponse := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	prometheus := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost {
				t.Errorf("method = %q, want %q", request.Method, http.MethodPost)
			}
			if request.URL.Path != "/api/v1/query" {
				t.Errorf("path = %q, want %q", request.URL.Path, "/api/v1/query")
			}
			if got := request.FormValue("lookback_delta"); got != "5m" {
				t.Errorf("default lookback = %q, want 5m", got)
			}
			if got := request.FormValue("query"); got != expression {
				t.Errorf("query = %q, want %q", got, expression)
			}

			response.Header().Set("Content-Type", "application/json")
			if _, err := response.Write([]byte(prometheusResponse)); err != nil {
				t.Errorf("write Prometheus response: %v", err)
			}
		},
	))
	t.Cleanup(prometheus.Close)

	client, err := NewClient(prometheus.URL, 30*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v, want nil", err)
	}
	response, err := client.Query(t.Context(), expression, "")
	if err != nil {
		t.Fatalf("Query() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close query response: %v", err)
		}
	})

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := response.Header.Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read query response: %v", err)
	}
	if got := string(body); got != prometheusResponse {
		t.Errorf("body = %q, want %q", got, prometheusResponse)
	}
}

func TestClientQueryRangeSendsPromQLRangeRequest(t *testing.T) {
	rangeQuery := RangeQuery{
		Expression: `rate(http_requests_total{method="GET"}[5m])`,
		Start:      "2026-09-04T10:00:00Z",
		End:        "2026-09-04T11:00:00Z",
		Step:       "15s",
	}
	prometheus := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost {
				t.Errorf("method = %q, want %q", request.Method, http.MethodPost)
			}
			if request.URL.Path != "/api/v1/query_range" {
				t.Errorf(
					"path = %q, want %q",
					request.URL.Path,
					"/api/v1/query_range",
				)
			}
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			parameters := request.PostForm
			if got := parameters.Get("lookback_delta"); got != "5m" {
				t.Errorf("default lookback = %q, want 5m", got)
			}
			if got := parameters.Get("query"); got != rangeQuery.Expression {
				t.Errorf("query = %q, want %q", got, rangeQuery.Expression)
			}
			if got := parameters.Get("start"); got != rangeQuery.Start {
				t.Errorf("start = %q, want %q", got, rangeQuery.Start)
			}
			if got := parameters.Get("end"); got != rangeQuery.End {
				t.Errorf("end = %q, want %q", got, rangeQuery.End)
			}
			if got := parameters.Get("step"); got != rangeQuery.Step {
				t.Errorf("step = %q, want %q", got, rangeQuery.Step)
			}

			response.WriteHeader(http.StatusOK)
		},
	))
	t.Cleanup(prometheus.Close)

	client, err := NewClient(prometheus.URL, 30*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v, want nil", err)
	}
	response, err := client.QueryRange(t.Context(), rangeQuery)
	if err != nil {
		t.Fatalf("QueryRange() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close range query response: %v", err)
		}
	})

	if response.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
}
