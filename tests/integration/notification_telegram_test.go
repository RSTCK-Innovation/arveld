package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestTelegramNotificationSettingsSurviveReopen(t *testing.T) {
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
	const body = `{"name":"Operations Telegram","type":"telegram","config":{"bot_token":"123456:Test_only-token","chat_id":-1001234567890,"message_thread_id":42}}`
	created := request(http.MethodPost, "/api/v1/notifications", body, http.StatusCreated)
	location := created.Header().Get("Location")
	for _, expected := range []string{`"api_url":"https://api.telegram.org"`, `"chat_id":-1001234567890`, `"message_thread_id":42`, `"bot_token":"123456:Test_only-token"`} {
		if !strings.Contains(created.Body.String(), expected) {
			t.Fatalf("missing normalized setting %s", expected)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	if got := request(http.MethodGet, location, "", http.StatusOK); got.Body.String() != created.Body.String() {
		t.Fatal("Telegram settings changed after reopen")
	}
	for _, change := range []struct{ name, old, value string }{
		{"missing bot token", "123456:Test_only-token", ""},
		{"token URL injection", "123456:Test_only-token", "123:abc/../other"},
		{"zero chat", "-1001234567890", "0"},
		{"fractional chat", "-1001234567890", "-100.5"},
		{"inexact browser integer", "-1001234567890", "9007199254740992"},
		{"negative thread", `"message_thread_id":42`, `"message_thread_id":-1`},
		{"oversized thread", `"message_thread_id":42`, `"message_thread_id":2147483648`},
		{"unknown field", `"chat_id":`, `"parse_mode":"HTML","chat_id":`},
		{"invalid endpoint", `"chat_id":`, `"api_url":"file:///tmp/bot","chat_id":`},
		{"query endpoint", `"chat_id":`, `"api_url":"https://example.test?token=x","chat_id":`},
	} {
		t.Run(change.name, func(t *testing.T) {
			response := request(http.MethodPut, location, strings.Replace(body, change.old, change.value, 1), http.StatusUnprocessableEntity)
			if response.Body.String() != "Unprocessable Entity\n" {
				t.Fatal("validation echoed channel settings")
			}
		})
	}
	if got := request(http.MethodGet, location, "", http.StatusOK); got.Body.String() != created.Body.String() {
		t.Fatal("rejected update changed Telegram settings")
	}
	updated := request(http.MethodPut, location, `{"name":"Private chat","type":"telegram","config":{"api_url":"https://bot.example.test/","bot_token":"456:New_token","chat_id":123456}}`, http.StatusOK)
	if !strings.Contains(updated.Body.String(), `"message_thread_id":0`) || !strings.Contains(updated.Body.String(), `"api_url":"https://bot.example.test"`) {
		t.Fatal("replacement must clear the thread and normalize the API base")
	}
	request(http.MethodDelete, location, "", http.StatusNoContent)
	request(http.MethodGet, location, "", http.StatusNotFound)
}
