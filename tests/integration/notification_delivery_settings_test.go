package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNotificationDeliverySettingsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	const body = `{"name":"Operations","type":"webhook","config":{"url":"https://hooks.example.com/events"},"delivery":{"group_by":"resource","group_wait_seconds":0,"group_interval_seconds":60,"repeat_interval_seconds":3600}}`
	created := request(http.MethodPost, "/api/v1/notifications", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create delivery settings = %d %s", created.Code, created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"delivery":{"group_by":"resource","group_wait_seconds":0,"group_interval_seconds":60,"repeat_interval_seconds":3600}`) {
		t.Fatalf("missing normalized settings: %s", created.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	read := request(http.MethodGet, created.Header().Get("Location"), "")
	if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
		t.Fatalf("settings changed after reopening: %d %s", read.Code, read.Body.String())
	}
	for _, invalid := range []string{
		strings.Replace(body, `"resource"`, `"all"`, 1),
		strings.Replace(body, `"group_wait_seconds":0`, `"group_wait_seconds":-1`, 1),
		strings.Replace(body, `"group_wait_seconds":0`, `"group_wait_seconds":3601`, 1),
		strings.Replace(body, `"group_interval_seconds":60`, `"group_interval_seconds":0`, 1),
		strings.Replace(body, `"group_interval_seconds":60`, `"group_interval_seconds":86401`, 1),
		strings.Replace(body, `"repeat_interval_seconds":3600`, `"repeat_interval_seconds":30`, 1),
		strings.Replace(body, `"repeat_interval_seconds":3600`, `"repeat_interval_seconds":61`, 1),
		strings.Replace(body, `"repeat_interval_seconds":3600`, `"repeat_interval_seconds":432060`, 1),
	} {
		if response := request(http.MethodPut, created.Header().Get("Location"), invalid); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid delivery accepted: %d %s", response.Code, response.Body.String())
		}
	}
	if read := request(http.MethodGet, created.Header().Get("Location"), ""); read.Body.String() != created.Body.String() {
		t.Fatalf("invalid updates changed settings: %s", read.Body.String())
	}
	const defaults = `{"name":"Operations","type":"webhook","config":{"url":"https://hooks.example.com/events"}}`
	updated := request(http.MethodPut, created.Header().Get("Location"), defaults)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"delivery":{"group_by":"rule","group_wait_seconds":5,"group_interval_seconds":30,"repeat_interval_seconds":14400}`) {
		t.Fatalf("omitted delivery did not restore defaults: %d %s", updated.Code, updated.Body.String())
	}
}
