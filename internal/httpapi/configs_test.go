package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPutAgentConfigRejectsInvalidHTTPInput(t *testing.T) {
	for _, test := range []struct {
		name, body, media string
		status            int
	}{
		{"larger than one MiB", strings.Repeat("a", 1024*1024+1), "application/x-yaml", http.StatusRequestEntityTooLarge},
		{"unsupported media type", "{}", "application/json", http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc((&api{}).putAgentConfig)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/agents/0198f1ad-6f2a-7a4b-8c3d-123456789abc/config", strings.NewReader(test.body))
			request.SetPathValue("instance_uid", "0198f1ad-6f2a-7a4b-8c3d-123456789abc")
			request.Header.Set("Content-Type", test.media)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("configuration response = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("error Content-Type = %q", response.Header().Get("Content-Type"))
			}
		})
	}
}
