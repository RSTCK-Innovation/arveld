package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONPreservesStrictBodyLimitsWithoutContentLength(t *testing.T) {
	const input = `{"email":"camille@example.com","password":"test"}`
	atLimit := input + strings.Repeat(" ", 4096-len(input))
	cases := []struct {
		name, body, media string
		status            int
	}{
		{"exact limit", atLimit, "application/json; charset=utf-8", http.StatusNoContent},
		{"excess whitespace", atLimit + " ", "application/json", http.StatusRequestEntityTooLarge},
		{"unknown field", `{"email":"e","unexpected":true}`, "application/json", http.StatusBadRequest},
		{"second value", input + ` {}`, "application/json", http.StatusBadRequest},
		{"invalid utf8", `{"email":"` + string([]byte{255}) + `"}`, "application/json", http.StatusBadRequest},
		{"unsupported type", input, "text/plain", http.StatusUnsupportedMediaType},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input loginRequest
				if status := decodeJSONRequest(w, r, &input); status != 0 {
					fail(w, status)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			send := func(body, media string) *httptest.ResponseRecorder {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", strings.NewReader(body))
				request.ContentLength = -1
				request.Header.Set("Content-Type", media)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			}
			if response := send(test.body, test.media); response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
