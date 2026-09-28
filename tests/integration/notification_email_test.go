package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestEmailNotificationSettingsSurviveReopen(t *testing.T) {
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
	const body = `{"name":"Operations email","type":"email","config":{"smarthost":"smtp.example.test:587","from":"arveld@example.test","to":"operations@example.test","auth_username":"arveld","auth_password":" test-only password "}}`
	created := request(http.MethodPost, "/api/v1/notifications", body, http.StatusCreated)
	var channel struct {
		Type   string `json:"type"`
		Config struct {
			SMTPHost string `json:"smarthost"`
			TLSMode  string `json:"tls_mode"`
			Password string `json:"auth_password"`
		} `json:"config"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &channel); err != nil {
		t.Fatal(err)
	}
	if channel.Type != "email" || channel.Config.SMTPHost != "smtp.example.test:587" || channel.Config.TLSMode != "starttls" || channel.Config.Password != " test-only password " {
		t.Fatalf("email settings or defaults changed: %+v", channel)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	read := request(http.MethodGet, created.Header().Get("Location"), "", http.StatusOK)
	if read.Body.String() != created.Body.String() {
		t.Fatal("email settings changed after database reopen")
	}
	for _, change := range []struct{ name, old, value string }{
		{"invalid recipient", "operations@example.test", "not-an-address"},
		{"multiple recipients", "operations@example.test", "one@example.test,two@example.test"},
		{"display name", "operations@example.test", "Ops <operations@example.test>"},
		{"template recipient", "operations@example.test", "{{.Receiver}}@example.test"},
		{"template sender", "arveld@example.test", "{{.Receiver}}@example.test"},
		{"missing port", "smtp.example.test:587", "smtp.example.test"},
		{"missing host", "smtp.example.test:587", ":587"},
		{"invalid port", "smtp.example.test:587", "smtp.example.test:65536"},
		{"URL instead of host", "smtp.example.test:587", "smtp://smtp.example.test:587"},
		{"unknown option", `"smarthost":`, `"tls_config":{},"smarthost":`},
		{"unknown encryption", `"smarthost":`, `"tls_mode":"auto","smarthost":`},
		{"credentials without TLS", `"smarthost":`, `"tls_mode":"none","smarthost":`},
		{"missing username", `"auth_username":"arveld"`, `"auth_username":""`},
		{"missing password", `"auth_password":" test-only password "`, `"auth_password":""`},
	} {
		t.Run(change.name, func(t *testing.T) {
			response := request(http.MethodPut, created.Header().Get("Location"), strings.Replace(body, change.old, change.value, 1), http.StatusUnprocessableEntity)
			if response.Body.String() != "Unprocessable Entity\n" {
				t.Fatal("validation must not echo email settings")
			}
		})
	}
	if got := request(http.MethodGet, created.Header().Get("Location"), "", http.StatusOK); got.Body.String() != created.Body.String() {
		t.Fatal("invalid update changed email settings")
	}
	updated := request(http.MethodPut, created.Header().Get("Location"), `{"name":"Local relay","type":"email","config":{"smarthost":"[::1]:2525","from":"arveld@example.test","to":"operations@example.test","tls_mode":"none"}}`, http.StatusOK)
	if !strings.Contains(updated.Body.String(), `"auth_password":""`) || !strings.Contains(updated.Body.String(), `"smarthost":"[::1]:2525"`) {
		t.Fatal("replacement must preserve IPv6 and clear omitted credentials")
	}
	request(http.MethodDelete, created.Header().Get("Location"), "", http.StatusNoContent)
	request(http.MethodGet, created.Header().Get("Location"), "", http.StatusNotFound)
}
