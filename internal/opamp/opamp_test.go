package opamp_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/opamp"
)

func TestServerRejectsMalformedAuthorization(t *testing.T) {
	server, err := opamp.NewServer(nil, nil, slog.New(slog.DiscardHandler), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close OpAMP server: %v", err)
		}
	})
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
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, opamp.Path, nil)
			for _, header := range test.authorization {
				request.Header.Add("Authorization", header)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || response.Body.Len() != 0 {
				t.Errorf("response = %d %q, want empty 401", response.Code, response.Body.String())
			}
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", got)
			}
		})
	}
}
