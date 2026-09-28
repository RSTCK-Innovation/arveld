package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestPagerDutyNotificationSettingsSurviveReopen(t *testing.T) {
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
		if response.Code != status {
			t.Fatalf("%s %s = %d %s, want %d", method, path, response.Code, response.Body.String(), status)
		}
		return response
	}
	const key = "0123456789abcdef0123456789abcdef"
	const body = `{"name":"Operations PagerDuty","type":"pagerduty","config":{"routing_key":"` + key + `"}}`
	created := request(http.MethodPost, "/api/v1/notifications", body, http.StatusCreated)
	location := created.Header().Get("Location")
	if !strings.Contains(created.Body.String(), `"url":"https://events.pagerduty.com/v2/enqueue"`) || !strings.Contains(created.Body.String(), key) {
		t.Fatal("missing PagerDuty endpoint or key")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	if got := request(http.MethodGet, location, "", http.StatusOK); got.Body.String() != created.Body.String() {
		t.Fatal("PagerDuty settings changed after reopen")
	}
	for _, change := range []struct{ name, old, value string }{
		{"empty key", key, ""},
		{"whitespace key", key, "key with spaces"},
		{"template key", key, "{{.Receiver}}"},
		{"oversized key", key, strings.Repeat("a", 513)},
		{"unsupported v1 key", `"routing_key":`, `"service_key":"secret","routing_key":`},
		{"custom severity", `"routing_key":`, `"severity":"info","routing_key":`},
		{"bad endpoint", `"routing_key":`, `"url":"file:///tmp/events","routing_key":`},
	} {
		t.Run(change.name, func(t *testing.T) {
			response := request(http.MethodPut, location, strings.Replace(body, change.old, change.value, 1), http.StatusUnprocessableEntity)
			if response.Body.String() != "Unprocessable Entity\n" {
				t.Fatal("validation echoed channel settings")
			}
		})
	}
	if got := request(http.MethodGet, location, "", http.StatusOK); got.Body.String() != created.Body.String() {
		t.Fatal("rejected update changed PagerDuty settings")
	}
	updated := request(http.MethodPut, location, `{"name":"EU PagerDuty","type":"pagerduty","config":{"routing_key":"replacement-key","url":"https://events.eu.pagerduty.com/v2/enqueue"}}`, http.StatusOK)
	if !strings.Contains(updated.Body.String(), `"routing_key":"replacement-key"`) || !strings.Contains(updated.Body.String(), "events.eu.pagerduty.com") {
		t.Fatal("replacement did not preserve the new endpoint and key")
	}
	request(http.MethodDelete, location, "", http.StatusNoContent)
	request(http.MethodGet, location, "", http.StatusNotFound)
}
