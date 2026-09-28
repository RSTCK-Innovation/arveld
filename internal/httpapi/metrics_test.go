package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsQueryRejectsInvalidBodies(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"wrong media type", "application/json", `{}`, http.StatusUnsupportedMediaType},
		{"invalid form", "application/x-www-form-urlencoded", "query=%ZZ", http.StatusBadRequest},
		{"empty expression", "application/x-www-form-urlencoded", "query=", http.StatusBadRequest},
		{"oversized form", "application/x-www-form-urlencoded", "query=" + strings.Repeat("x", 1024*1024), http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, handler := range []http.HandlerFunc{(&api{}).queryMetrics, (&api{}).queryRangeMetrics} {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/metrics/query", strings.NewReader(test.body))
				request.Header.Set("Content-Type", test.contentType)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != test.status {
					t.Errorf("invalid body response = %d, want %d", response.Code, test.status)
				}
			}
		})
	}
}

func TestMetricsQueryRejectsMissingParameters(t *testing.T) {
	for _, test := range []struct {
		name, target string
		handler      http.HandlerFunc
	}{
		{"instant expression", "/metrics/query", (&api{}).queryMetrics},
		{"range expression", "/metrics/query_range?start=100&end=200&step=15", (&api{}).queryRangeMetrics},
		{"range start", "/metrics/query_range?query=up&end=200&step=15", (&api{}).queryRangeMetrics},
		{"range end", "/metrics/query_range?query=up&start=100&step=15", (&api{}).queryRangeMetrics},
		{"range step", "/metrics/query_range?query=up&start=100&end=200", (&api{}).queryRangeMetrics},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Invalid HTTP input must be rejected without a configured upstream.
			handler := test.handler
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.target, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != "Bad Request\n" {
				t.Fatalf("invalid query response = %d %q, want generic 400", response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("error Content-Type = %q", response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestOTLPMetricsRejectsMalformedAuthorizationBeforeForwarding(t *testing.T) {
	for _, test := range []struct {
		name          string
		authorization []string
	}{
		{"missing header", nil},
		{"empty header", []string{""}},
		{"missing token", []string{"Bearer"}},
		{"wrong scheme", []string{"Basic secret-token"}},
		{"extra field", []string{"Bearer secret-token extra"}},
		{"duplicate headers", []string{"Bearer secret-token", "Bearer other-token"}},
		{"combined headers", []string{"Bearer secret-token, Bearer other-token"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			forwarded := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				forwarded = true
			})
			handler := (&api{otlp: next}).receiveMetrics
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/otlp/v1/metrics", nil)
			for _, authorization := range test.authorization {
				request.Header.Add("Authorization", authorization)
			}
			response := httptest.NewRecorder()

			http.HandlerFunc(handler).ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized || response.Body.String() != "Unauthorized\n" {
				t.Errorf("response = %d %q, want generic 401", response.Code, response.Body.String())
			}
			if got, want := response.Header().Get("WWW-Authenticate"), "Bearer"; got != want {
				t.Errorf("WWW-Authenticate = %q, want %q", got, want)
			}
			if forwarded {
				t.Error("request was forwarded with malformed authorization")
			}
		})
	}
}
