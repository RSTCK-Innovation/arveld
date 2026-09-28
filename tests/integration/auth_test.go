package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

func TestHTTPServerReportsAdministratorSetupState(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	authStore := auth.NewStore(db)
	server := newAccountHandler(db)

	checkSetup := func(wantStatus int, wantBody, wantContentType string) {
		t.Helper()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/auth/setup", nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)

		if response.Code != wantStatus {
			t.Fatalf("setup status = %d, want %d", response.Code, wantStatus)
		}
		if got := response.Body.String(); got != wantBody {
			t.Errorf("setup body = %q, want %q", got, wantBody)
		}
		if got := response.Header().Get("Content-Type"); got != wantContentType {
			t.Errorf("Content-Type = %q, want %q", got, wantContentType)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}

	checkSetup(http.StatusOK, "{\"required\":true}\n", "application/json")
	if err := authStore.CreateAdministrator(ctx, auth.CreateAdministratorParams{
		Name:     "Camille",
		Email:    "camille@example.com",
		Password: "a long password for the local administrator",
	}); err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	checkSetup(http.StatusOK, "{\"required\":false}\n", "application/json")

	if err := db.Close(); err != nil {
		t.Fatalf("close database before failure check: %v", err)
	}
	checkSetup(http.StatusInternalServerError, "Internal Server Error\n", "text/plain; charset=utf-8")
}
