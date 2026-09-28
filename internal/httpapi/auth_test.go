package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestUpdateProfileRejectsInvalidHTTPInput(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"validation rejection", `{"name":"Robin","email":"invalid"}`, http.StatusUnprocessableEntity},
		{"unknown field", `{"name":"Robin","email":"robin@example.com","password":"secret"}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := noStore(http.HandlerFunc((&api{}).updateProfile))
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/profile", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != http.StatusText(test.status)+"\n" {
				t.Fatalf("profile response = %d %q, want generic %d", response.Code, response.Body.String(), test.status)
			}
			if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("profile response headers = %v", response.Header())
			}
		})
	}
}

func TestAdmissionBoundsConcurrencyBeyondTheRateInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		gate := newAdmission(time.Second)
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if !gate.acquire() {
				tooManyRequests(w)
				return
			}
			defer gate.release()
			<-release
			w.WriteHeader(http.StatusNoContent)
		})
		send := func() *httptest.ResponseRecorder {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil))
			return response
		}
		first := make(chan *httptest.ResponseRecorder, 1)
		go func() { first <- send() }()
		synctest.Wait()
		time.Sleep(2 * time.Second)
		if response := send(); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "1" {
			t.Fatalf("busy response = %d, %v", response.Code, response.Header())
		}
		close(release)
		if response := <-first; response.Code != http.StatusNoContent {
			t.Fatalf("first request = %d", response.Code)
		}
		if response := send(); response.Code != http.StatusNoContent {
			t.Fatalf("busy rejection consumed the next slot: %d", response.Code)
		}
		if response := send(); response.Code != http.StatusTooManyRequests {
			t.Fatalf("immediate next request = %d", response.Code)
		}
		time.Sleep(time.Second)
		if response := send(); response.Code != http.StatusNoContent {
			t.Fatalf("request at rate boundary = %d", response.Code)
		}
	})
}
