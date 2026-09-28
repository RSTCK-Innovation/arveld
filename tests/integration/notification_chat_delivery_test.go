package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

// The real engine sends service-specific formats to isolated local receivers.
// No provider account is contacted by this test.
func TestNativeChatNotificationLifecycle(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable to run native chat delivery")
	}
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	type message struct{ channel, title, text string }
	received := make(chan message, 16)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID          string `json:"chat_id"`
			MessageThreadID string `json:"message_thread_id"`
			ParseMode       string `json:"parse_mode"`
			Text            string `json:"text"`
			Attachments     []struct {
				Title       string `json:"title"`
				Text        string `json:"text"`
				ContentType string `json:"contentType"`
				Content     struct {
					Type string `json:"type"`
					Body []struct {
						Text string `json:"text"`
					} `json:"body"`
				} `json:"content"`
			} `json:"attachments"`
			Embeds []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"embeds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value := message{channel: r.URL.Path}
		switch r.URL.Path {
		case "/bot123456:Test_only-token/sendMessage":
			if body.ChatID != "-1001234567890" || body.MessageThreadID != "42" || body.ParseMode != "" {
				t.Errorf("incorrect Telegram destination or parse mode: chat=%s thread=%s mode=%s", body.ChatID, body.MessageThreadID, body.ParseMode)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			value.channel, value.text = "/telegram", body.Text
		case "/msteams":
			if len(body.Attachments) != 1 || body.Attachments[0].ContentType != "application/vnd.microsoft.card.adaptive" || body.Attachments[0].Content.Type != "AdaptiveCard" || len(body.Attachments[0].Content.Body) != 2 {
				t.Error("missing Teams adaptive card")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			value.title, value.text = body.Attachments[0].Content.Body[0].Text, body.Attachments[0].Content.Body[1].Text
		case "/slack":
			if len(body.Attachments) != 1 {
				t.Error("missing Slack attachment")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			value.title, value.text = body.Attachments[0].Title, body.Attachments[0].Text
		case "/discord":
			if len(body.Embeds) != 1 {
				t.Error("missing Discord embed")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			value.title, value.text = body.Embeds[0].Title, body.Embeds[0].Description
		default:
			t.Errorf("unexpected destination %s", r.URL.Path)
		}
		select {
		case received <- value:
		default:
			t.Error("unexpected notification flood")
		}
		switch value.channel {
		case "/telegram":
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1,"chat":{"id":-1001234567890,"type":"supergroup"},"text":"accepted"}}`)); err != nil {
				t.Error(err)
			}
		case "/discord":
			w.WriteHeader(http.StatusNoContent)
		default:
			if _, err := w.Write([]byte("ok")); err != nil {
				t.Error(err)
			}
		}
	}))
	t.Cleanup(destination.Close)
	store := notification.NewStore(db)
	for _, channelType := range []string{"discord", "slack", "msteams", "telegram"} {
		settings := map[string]any{"url": destination.URL + "/" + channelType}
		if channelType == "telegram" {
			settings = map[string]any{"api_url": destination.URL, "bot_token": "123456:Test_only-token", "chat_id": int64(-1001234567890), "message_thread_id": 42}
		}
		config, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(t.Context(), notification.Channel{
			ID: channelType, Name: channelType, Type: channelType, Config: config,
			Delivery: &notification.DeliverySettings{GroupBy: "resource", GroupWaitSeconds: 1, GroupIntervalSeconds: 1, RepeatIntervalSeconds: 3},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
 INSERT INTO alert_rules (id,agent_instance_uid,condition,threshold,for_seconds,severity) VALUES
 ('cpu',zeroblob(16),'cpu',80,1,'warning'),('memory',zeroblob(16),'memory',80,1,'warning');
 INSERT INTO alert_rule_notifications (rule_id,notification_id) VALUES
 ('cpu','discord'),('cpu','slack'),('memory','discord'),('memory','slack'),('cpu','msteams'),('memory','msteams'),('cpu','telegram'),('memory','telegram');
 `); err != nil {
		t.Fatal(err)
	}
	content, err := store.AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(directory, content); err != nil {
		t.Fatal(err)
	}
	engine := startNotificationAlertmanager(t, directory)
	start := time.Now().Add(-time.Minute).UTC()
	emit := func(end time.Time) {
		t.Helper()
		var alerts []map[string]any
		for _, id := range []string{"cpu", "memory"} {
			alerts = append(alerts, map[string]any{"labels": map[string]string{"alertname": "ArveldAgent" + id, "arveld_rule_id": id, "arveld_rule_revision": "revision", "arveld_agent_id": "00000000-0000-0000-0000-000000000000", "severity": "warning"}, "annotations": map[string]string{"resource": "Homelab", "summary": "High " + id + " usage", "description": "Usage is above 80%."}, "startsAt": start, "endsAt": end.UTC()})
		}
		body, err := json.Marshal(alerts)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, engine+"/api/v2/alerts", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("native alert ingestion = %d", response.StatusCode)
		}
	}
	await := func(status string) {
		t.Helper()
		expected := map[string]bool{"/discord": false, "/slack": false, "/msteams": false, "/telegram": false}
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for remaining := len(expected); remaining > 0; {
			select {
			case value := <-received:
				title := "Arveld · " + status + " · Homelab"
				if value.channel == "/telegram" {
					if !strings.HasPrefix(value.text, title) {
						t.Fatalf("unreadable Telegram title: %q", value.text)
					}
				} else if value.title != title {
					t.Fatalf("unreadable %s title: %q", value.channel, value.title)
				}
				if !strings.Contains(value.text, "High cpu usage") || !strings.Contains(value.text, "High memory usage") {
					t.Fatalf("missing grouped alert details in %s: %+v", value.channel, value)
				}
				for _, technical := range []string{"arveld_rule_id", "revision", "ArveldAgent", "00000000-0000-0000-0000-000000000000"} {
					if strings.Contains(value.text+value.title, technical) {
						t.Fatalf("internal detail %q in %s", technical, value.channel)
					}
				}
				if seen, exists := expected[value.channel]; exists && !seen {
					expected[value.channel] = true
					remaining--
				}
			case <-timer.C:
				t.Fatalf("missing %s native chat deliveries: %v", status, expected)
			}
		}
	}
	emit(time.Now().Add(time.Hour))
	await("2 alerts active")
	await("2 alerts active") // The configured 3-second reminder, not the default 4 hours.
	emit(time.Now().Add(-time.Second))
	await("Recovered")
}
