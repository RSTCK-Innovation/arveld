package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNotificationChannelLifecycle(t *testing.T) {
	for _, channelType := range []string{"webhook", "discord", "slack", "msteams"} {
		t.Run(channelType, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arveld.db")
			db := testutil.OpenDatabase(t, path)
			createAdministrator(t, db)
			handler := newAccountHandler(db)
			cookie := login(t, handler, "a long password for testing")
			request := func(method, path, body string, status int) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
				req.AddCookie(cookie)
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s %s = %d %s, want uncached %d", method, path, response.Code, response.Body.String(), status)
				}
				return response
			}
			const collection = "/api/v1/notifications"
			body := `{"name":"Operations","type":"` + channelType + `","config":{"url":"https://hooks.example.com/events"}}`
			if empty := request(http.MethodGet, collection, "", 200); strings.TrimSpace(empty.Body.String()) != `{"notifications":[]}` {
				t.Fatal(empty.Body.String())
			}
			first := request(http.MethodPost, collection, body, 201)
			second := request(http.MethodPost, collection, body, 201)
			location := first.Header().Get("Location")
			if !strings.HasPrefix(location, collection+"/") || location == second.Header().Get("Location") {
				t.Fatal("expected distinct server-owned channel URLs")
			}
			updated := request(http.MethodPut, location, strings.Replace(body, "/events", "/updated", 1), 200)
			if !strings.Contains(updated.Body.String(), `"url":"https://hooks.example.com/updated"`) {
				t.Fatal(updated.Body.String())
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = testutil.OpenDatabase(t, path)
			handler = newAccountHandler(db)
			if got := request(http.MethodGet, location, "", 200); got.Body.String() != updated.Body.String() {
				t.Fatal("update did not survive database reopening")
			}
			var listed struct {
				Notifications []struct {
					ID string `json:"id"`
				} `json:"notifications"`
			}
			if err := json.Unmarshal(request(http.MethodGet, collection, "", 200).Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if len(listed.Notifications) != 2 || listed.Notifications[0].ID >= listed.Notifications[1].ID {
				t.Fatalf("unexpected channel list: %+v", listed)
			}
			for _, path := range []string{collection, location} {
				head := request(http.MethodHead, path, "", 200)
				if head.Body.Len() != 0 || head.Header().Get("Content-Type") != "application/json" {
					t.Fatal("HEAD must return JSON headers without a body")
				}
			}
			request(http.MethodDelete, location, "", 204)
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
				request(method, location, body, 404)
			}
			if got := request(http.MethodGet, second.Header().Get("Location"), "", 200); got.Body.String() != second.Body.String() {
				t.Fatal("other channel changed")
			}
		})
	}
}

func TestNotificationChannelValidationAndAccess(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writer, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body, token, origin, contentType string, session *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if session != nil {
			req.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	const collection = "/api/v1/notifications"
	const valid = `{"name":"Operations","type":"webhook","config":{"url":"https://hooks.example.com/events"}}`
	created := request(http.MethodPost, collection,
		`{"name":"  Équipe  ","type":"webhook","config":{"url":"http://127.0.0.1:9999/events?token=test-only"}}`, writer, "", "application/json", nil)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"name":"Équipe"`) {
		t.Fatalf("write-key creation = %d %s, want 201 with a trimmed name", created.Code, created.Body.String())
	}
	location := created.Header().Get("Location")
	for _, invalid := range []struct {
		name, body string
		status     int
	}{
		{"short name", strings.Replace(valid, "Operations", "A", 1), 422},
		{"NUL name", strings.Replace(valid, "Operations", `Ops\u0000`, 1), 422},
		{"unsupported channel", strings.Replace(valid, "webhook", "unknown", 1), 422},
		{"unknown config field", strings.Replace(valid, `"url":`, `"token":"secret","url":`, 1), 422},
		{"relative URL", strings.Replace(valid, "https://hooks.example.com/events", "/events", 1), 422},
		{"unsupported URL scheme", strings.Replace(valid, "https://", "file://", 1), 422},
		{"URL credentials", strings.Replace(valid, "hooks.example.com", "user:secret@hooks.example.com", 1), 422},
		{"URL fragment", strings.Replace(valid, "/events", "/events#secret", 1), 422},
		{"zero port", strings.Replace(valid, "hooks.example.com", "hooks.example.com:0", 1), 422},
		{"out of range port", strings.Replace(valid, "hooks.example.com", "hooks.example.com:65536", 1), 422},
		{"malformed JSON", `{`, 400},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			response := request(http.MethodPut, location, invalid.body, "", "", "application/json", cookie)
			if response.Code != invalid.status || response.Body.String() != http.StatusText(invalid.status)+"\n" {
				t.Fatalf("invalid notification %s = %d %q, want generic %d", http.MethodPut, response.Code, response.Body.String(), invalid.status)
			}
			if response.Header().Get("Location") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("rejected write must not publish a Location or allow caching")
			}
			read := request(http.MethodGet, location, "", reader, "", "", nil)
			if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
				t.Fatalf("rejected update changed the channel: %d %s", read.Code, read.Body.String())
			}
		})
	}
	for _, access := range []struct {
		name, token, origin string
		session             *http.Cookie
		status              int
	}{
		{"anonymous", "", "", nil, 401},
		{"reader", reader, "", nil, 403},
		{"Agent credential", agentKey, "", nil, 401},
		{"invalid bearer overrides session", "invalid", "", cookie, 401},
		{"foreign origin", "", "https://foreign.example", cookie, 403},
	} {
		t.Run(access.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
				path := collection
				if method != http.MethodPost {
					path = location
				}
				response := request(method, path, valid, access.token, access.origin, "application/json", access.session)
				if response.Code != access.status || response.Header().Get("Location") != "" {
					t.Fatalf("unauthorized channel %s = %d, want %d without Location", method, response.Code, access.status)
				}
			}
		})
	}
	read := request(http.MethodGet, location, "", reader, "", "", nil)
	if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
		t.Fatalf("read-only key must retrieve saved settings: %d %s", read.Code, read.Body.String())
	}
	updated := request(http.MethodPut, location, valid, writer, "", "application/json", nil)
	if updated.Code != http.StatusOK {
		t.Fatalf("write-key update = %d %s, want 200", updated.Code, updated.Body.String())
	}
	read = request(http.MethodGet, location, "", reader, "", "", nil)
	if read.Code != http.StatusOK || read.Body.String() != updated.Body.String() {
		t.Fatalf("read-only key must retrieve updated settings: %d %s", read.Code, read.Body.String())
	}
	if got := request(http.MethodPut, collection+"/missing", valid, writer, "", "application/json", nil).Code; got != http.StatusNotFound {
		t.Fatalf("update missing channel = %d, want 404", got)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete} {
		for _, token := range []string{"", agentKey} {
			if got := request(method, location, "", token, "", "", nil).Code; got != http.StatusUnauthorized {
				t.Fatalf("non-management channel access = %d, want 401", got)
			}
		}
		if got := request(method, collection+"/missing", "", writer, "", "", nil).Code; got != http.StatusNotFound {
			t.Fatalf("missing channel = %d, want 404", got)
		}
	}
}
