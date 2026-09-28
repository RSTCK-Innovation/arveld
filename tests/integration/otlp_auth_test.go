package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/httpapi"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestOTLPMetricsAuthenticatesEachRequestAgainstPersistedKeys(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		response.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)
	config := controllerConfig(t, upstream.URL)
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	store := agentauth.NewStore(db)
	primary, primaryToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Primary agent"})
	if err != nil {
		t.Fatal(err)
	}
	_, secondaryToken, err := store.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Secondary agent"})
	if err != nil {
		t.Fatal(err)
	}
	url, _ := startController(t, config)

	send := func(t *testing.T, authorization []string, wantStatus int) {
		t.Helper()
		before := forwarded.Load()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/otlp/v1/metrics", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, header := range authorization {
			request.Header.Add("Authorization", header)
		}
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
		if response.StatusCode != wantStatus {
			t.Fatalf("status = %d, want %d", response.StatusCode, wantStatus)
		}
		if wantStatus == http.StatusAccepted {
			if forwarded.Load() != before+1 {
				t.Error("accepted request did not reach Prometheus exactly once")
			}
			return
		}
		if string(body) != "Unauthorized\n" || response.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("rejected response = %q, want generic 401 with Bearer challenge", body)
		}
		if forwarded.Load() != before {
			t.Error("rejected request reached Prometheus")
		}
	}

	send(t, []string{"Bearer " + primaryToken}, http.StatusAccepted)
	send(t, []string{"bearer " + secondaryToken}, http.StatusAccepted)
	// Revoke through another database connection while the controller stays running.
	if err := store.RevokeKey(t.Context(), primary.ID); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		authorization []string
	}{
		{"revoked key", []string{"Bearer " + primaryToken}},
		{"unknown key", []string{"Bearer unknown-token"}},
		{"legacy static token", []string{"Bearer secret-token"}},
		{"duplicate headers with active key first", []string{"Bearer " + secondaryToken, "Bearer unknown-token"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			send(t, test.authorization, http.StatusUnauthorized)
		})
	}
	send(t, []string{"Bearer " + secondaryToken}, http.StatusAccepted)
}

func TestOTLPMetricsRejectsRequestsWhenKeyStorageIsUnavailable(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	_, token, err := agentauth.NewStore(db).CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "Metrics agent"})
	if err != nil {
		t.Fatal(err)
	}
	deps := accountDependencies(db)
	forwarded := false
	deps.OTLPMetrics = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		forwarded = true
		response.WriteHeader(http.StatusAccepted)
	})
	handler := httpapi.NewHandler(httpapi.Config{SecureCookie: true}, deps)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/otlp/v1/metrics", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Errorf("response = %d %q, want generic 500", response.Code, response.Body.String())
	}
	if response.Header().Get("WWW-Authenticate") != "" {
		t.Error("storage failure must not be reported as invalid credentials")
	}
	if forwarded {
		t.Error("request reached Prometheus while key authentication was unavailable")
	}
}
