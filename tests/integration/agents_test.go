package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

func TestListAgentsReturnsEmptyArray(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"agents\":[]}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestListAgentsReturnsStoredAgent(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	store := agent.NewStore(db)
	uid := agent.InstanceUID{}
	uid[0] = 0x12
	uid[15] = 0xab
	if err := store.Upsert(context.Background(), agent.UpsertParams{
		InstanceUID: uid,
		Connected:   true,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents",
		nil,
	)
	response := httptest.NewRecorder()
	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Body.String(), "{\"agents\":[{\"instance_uid\":\"12000000-0000-0000-0000-0000000000ab\",\"hostname\":null,\"version\":null,\"connected\":true,\"last_seen_at\":null}]}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	hostname, version := "collector-01", "0.159.0"
	lastSeenAt := time.Date(2026, time.September, 1, 14, 30, 0, 123_000_000, time.UTC)
	if err := store.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: uid, Hostname: &hostname, Version: &version, Connected: false, LastSeenAt: &lastSeenAt,
	}); err != nil {
		t.Fatalf("store agent metadata: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request.Clone(t.Context()))
	want := `{"agents":[{"instance_uid":"12000000-0000-0000-0000-0000000000ab","hostname":"collector-01","version":"0.159.0","connected":false,"last_seen_at":"2026-09-01T14:30:00.123Z"}]}` + "\n"
	if response.Code != http.StatusOK || response.Body.String() != want {
		t.Errorf("agent metadata response = %d %q, want 200 %q", response.Code, response.Body.String(), want)
	}
}

func TestListAgentsReturnsInternalServerError(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents",
		nil,
	)
	// Keep authentication available while making the Agent inventory unreadable.
	if _, err := db.ExecContext(t.Context(), "DROP TABLE agents"); err != nil {
		t.Fatalf("make Agent storage unavailable: %v", err)
	}
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusInternalServerError,
		)
	}
	if got, want := response.Body.String(), "Internal Server Error\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
