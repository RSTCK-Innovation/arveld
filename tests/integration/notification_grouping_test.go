package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestNativeNotificationGroupingPerChannel(t *testing.T) {
	if os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set the pinned Alertmanager executable to run native grouping")
	}
	directory := t.TempDir()
	db := testutil.OpenDatabase(t, filepath.Join(directory, "arveld.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{ID: id, Name: id, Protocol: "tcp", AgentInstanceUID: uid, Endpoint: "example.com:443", IntervalSeconds: 30, TimeoutSeconds: 5}); err != nil {
			t.Fatal(err)
		}
	}
	type message struct {
		channel string
		ids     []string
	}
	received := make(chan message, 16)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Alerts []struct {
				Labels map[string]string `json:"labels"`
			} `json:"alerts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value := message{channel: r.URL.Path}
		for _, item := range body.Alerts {
			value.ids = append(value.ids, item.Labels["arveld_rule_id"])
		}
		slices.Sort(value.ids)
		select {
		case received <- value:
		default:
			t.Error("unexpected notification flood")
		}
	}))
	t.Cleanup(destination.Close)
	store := notification.NewStore(db)
	for _, id := range []string{"grouped", "separate"} {
		var delivery *notification.DeliverySettings
		if id == "grouped" {
			delivery = &notification.DeliverySettings{GroupBy: "resource", GroupWaitSeconds: 1, GroupIntervalSeconds: 2, RepeatIntervalSeconds: 60}
		}
		config, err := json.Marshal(map[string]string{"url": destination.URL + "/" + id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(t.Context(), notification.Channel{ID: id, Name: id, Type: "webhook", Config: config, Delivery: delivery}); err != nil {
			t.Fatal(err)
		}
	}
	threshold := 80.0
	for _, rule := range []alert.Rule{
		{ID: "cpu", AgentInstanceUID: &uid, Condition: "cpu", Threshold: &threshold, ForSeconds: 1, Severity: "warning", NotificationIDs: []string{"grouped", "separate"}},
		{ID: "memory", AgentInstanceUID: &uid, Condition: "memory", Threshold: &threshold, ForSeconds: 1, Severity: "warning", NotificationIDs: []string{"grouped", "separate"}},
		{ID: "one.a", MonitorID: "one", Condition: "failed", ForSeconds: 1, Severity: "warning", NotificationIDs: []string{"grouped", "separate"}},
		{ID: "one+b", MonitorID: "one", Condition: "no_data", ForSeconds: 1, Severity: "warning", NotificationIDs: []string{"grouped", "separate"}},
		{ID: "two", MonitorID: "two", Condition: "failed", ForSeconds: 1, Severity: "warning", NotificationIDs: []string{"grouped", "separate"}},
	} {
		if err := alert.NewStore(db).Create(t.Context(), rule); err != nil {
			t.Fatal(err)
		}
	}
	content, err := store.AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(directory, content); err != nil {
		t.Fatal(err)
	}
	engine := startNotificationAlertmanager(t, directory)
	var alerts []map[string]any
	for _, entry := range []struct{ id, owner string }{{"cpu", ""}, {"memory", ""}, {"one.a", "one"}, {"one+b", "one"}, {"two", "two"}, {"oneXa", "one"}} {
		alerts = append(alerts, map[string]any{"labels": map[string]string{"alertname": "RoutingFixture", "arveld_rule_id": entry.id, "arveld_rule_revision": "revision", "arveld_agent_id": uid.String(), "arveld_monitor_id": entry.owner}, "startsAt": time.Now().UTC(), "endsAt": time.Now().Add(time.Hour).UTC()})
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
	expected := map[string]bool{"/grouped:cpu,memory": false, "/separate:cpu": false, "/separate:memory": false, "/grouped:one+b,one.a": false, "/grouped:two": false, "/separate:one.a": false, "/separate:one+b": false, "/separate:two": false}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for remaining := len(expected); remaining > 0; {
		select {
		case value := <-received:
			key := value.channel + ":" + strings.Join(value.ids, ",")
			seen, exists := expected[key]
			if !exists {
				t.Fatalf("wrong native notification group: %s", key)
			}
			if !seen {
				expected[key] = true
				remaining--
			}
		case <-timer.C:
			t.Fatalf("missing native notification groups: %v", expected)
		}
	}
}
