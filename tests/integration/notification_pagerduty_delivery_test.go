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

func TestNativePagerDutyNotificationLifecycle(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable to run native PagerDuty delivery")
	}
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	type event struct {
		RoutingKey string `json:"routing_key"`
		Action     string `json:"event_action"`
		DedupKey   string `json:"dedup_key"`
		Payload    struct {
			Severity string          `json:"severity"`
			Summary  string          `json:"summary"`
			Source   string          `json:"source"`
			Details  json.RawMessage `json:"custom_details"`
		} `json:"payload"`
	}
	received := make(chan event, 16)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value event
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v2/enqueue" || value.RoutingKey != "0123456789abcdef0123456789abcdef" {
			t.Error("incorrect PagerDuty endpoint or routing key")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case received <- value:
		default:
			t.Error("unexpected notification flood")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if _, err := w.Write([]byte(`{"status":"success","message":"Event processed"}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(destination.Close)
	config, err := json.Marshal(map[string]string{"url": destination.URL + "/v2/enqueue", "routing_key": "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	store := notification.NewStore(db)
	if _, err := store.Create(t.Context(), notification.Channel{
		ID: "pagerduty", Name: "Operations PagerDuty", Type: "pagerduty", Config: config,
		Delivery: &notification.DeliverySettings{GroupBy: "resource", GroupWaitSeconds: 0, GroupIntervalSeconds: 1, RepeatIntervalSeconds: 60},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
 INSERT INTO alert_rules (id,agent_instance_uid,condition,threshold,for_seconds,severity) VALUES
 ('cpu',zeroblob(16),'cpu',80,1,'warning'),('memory',zeroblob(16),'memory',80,1,'critical');
 INSERT INTO alert_rule_notifications (rule_id,notification_id) VALUES ('cpu','pagerduty'),('memory','pagerduty');
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
	emit := func(ids []string, end time.Time) {
		t.Helper()
		var alerts []map[string]any
		for _, id := range ids {
			severity := "warning"
			if id == "memory" {
				severity = "critical"
			}
			alerts = append(alerts, map[string]any{"labels": map[string]string{"alertname": "ArveldAgent" + id, "arveld_rule_id": id, "arveld_rule_revision": "revision", "arveld_agent_id": "00000000-0000-0000-0000-000000000000", "severity": severity}, "annotations": map[string]string{"resource": "Homelab", "summary": "High " + id + " usage", "description": "Usage is above 80%."}, "startsAt": start, "endsAt": end.UTC()})
		}
		body, err := json.Marshal(alerts)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, engine+"/api/v2/alerts", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("native ingestion = %d", response.StatusCode)
		}
	}
	await := func(action, severity string, names ...string) event {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case value := <-received:
				if value.Action != action {
					continue
				}
				if value.DedupKey == "" || value.Payload.Severity != severity || !strings.HasPrefix(value.Payload.Summary, "Arveld · ") || value.Payload.Source != "Homelab" {
					t.Fatalf("incorrect PagerDuty event: %+v", value)
				}
				if strings.Contains(string(value.Payload.Details), "arveld_rule_id") {
					t.Fatal("PagerDuty details expose routing internals")
				}
				for _, name := range names {
					if !strings.Contains(string(value.Payload.Details), name) {
						t.Fatalf("missing %s in grouped PagerDuty details", name)
					}
				}
				return value
			case <-timer.C:
				t.Fatalf("missing PagerDuty %s event", action)
			}
		}
	}
	emit([]string{"cpu"}, time.Now().Add(time.Hour))
	warning := await("trigger", "warning", "High cpu usage")
	emit([]string{"cpu", "memory"}, time.Now().Add(time.Hour))
	critical := await("trigger", "critical", "High cpu usage", "High memory usage")
	emit([]string{"cpu", "memory"}, time.Now().Add(-time.Second))
	resolved := await("resolve", "critical", "High cpu usage", "High memory usage")
	if warning.DedupKey != critical.DedupKey || critical.DedupKey != resolved.DedupKey {
		t.Fatal("group update and resolution must target the original PagerDuty event")
	}
}
