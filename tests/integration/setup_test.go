package integration

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
)

func TestHTTPServerCreatesFirstAdministrator(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	store := auth.NewStore(db)
	server := newAccountHandler(db)

	post := func(body, contentType, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost,
			"http://arveld.example/api/v1/auth/setup", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	const body = `{"name":"  Camille  ","email":"  CAMILLE@EXAMPLE.COM  ","password":"  a very long password 🔑  "}`
	invalid := []struct {
		name        string
		body        string
		contentType string
		origin      string
		status      int
	}{
		{"malformed JSON", `{`, "application/json", "", http.StatusBadRequest},
		{"unknown field", strings.Replace(body, `"name":`, `"role":"admin","name":`, 1), "application/json", "", http.StatusBadRequest},
		{"empty object", `{}`, "application/json", "", http.StatusUnprocessableEntity},
		{"null object", `null`, "application/json", "", http.StatusUnprocessableEntity},
		{"oversized body", body + strings.Repeat(" ", 4096), "application/json", "", http.StatusRequestEntityTooLarge},
		{"cross origin", body, "application/json", "https://another.example", http.StatusForbidden},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			response := post(test.body, test.contentType, test.origin)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			required, err := store.NeedsSetup(ctx)
			if err != nil || !required {
				t.Fatalf("NeedsSetup() = %v, %v after rejected request, want true, nil", required, err)
			}
		})
	}
	if t.Failed() {
		return
	}

	// Occupy the sole SQL connection so the first creation stays in progress.
	connection, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close reserved connection: %v", err)
		}
	})
	waitCount := db.Stats().WaitCount
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- post(body, "application/json; charset=utf-8", "http://arveld.example")
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for db.Stats().WaitCount == waitCount {
		select {
		case <-ctx.Done():
			t.Fatal("first creation did not reach the database")
		case <-ticker.C:
		}
	}
	busy := post(body, "application/json", "http://arveld.example")
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") != "1" {
		t.Errorf("concurrent creation = %d, Retry-After %q, want 429 and 1", busy.Code, busy.Header().Get("Retry-After"))
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("release reserved connection: %v", err)
	}
	response := <-first
	if response.Code != http.StatusCreated {
		t.Fatalf("creation status = %d, want 201", response.Code)
	}
	if response.Body.Len() != 0 || len(response.Header().Values("Set-Cookie")) != 0 {
		t.Error("creation must return no credential data or session cookie yet")
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := post(body, "application/json", "").Code; got != http.StatusConflict {
		t.Errorf("second creation status = %d, want 409", got)
	}
	administrator, err := store.Administrator(ctx)
	if err != nil {
		t.Fatalf("read administrator: %v", err)
	}
	if administrator.Name != "Camille" || administrator.Email != "camille@example.com" {
		t.Error("stored profile is not normalized")
	}
	match, err := auth.VerifyPassword("  a very long password 🔑  ", administrator.PasswordHash)
	if err != nil || !match {
		t.Fatalf("password verification = %v, %v, want true, nil", match, err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close database before failure check: %v", err)
	}
	if got := post(body, "application/json", "").Code; got != http.StatusInternalServerError {
		t.Errorf("unavailable database status = %d, want 500", got)
	}
}
