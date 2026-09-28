package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/alert"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerRecordsNativeIncidentEpisodes(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	owner := monitor.Monitor{ID: "incident-monitor", Name: "Checkout API", Protocol: "http", AgentInstanceUID: uid, Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5}
	if err := monitor.NewStore(db).Create(t.Context(), owner); err != nil {
		t.Fatal(err)
	}
	if err := alert.NewStore(db).Create(t.Context(), alert.Rule{ID: "incident-rule", MonitorID: owner.ID, Condition: "failed", ForSeconds: 1, Severity: "critical"}); err != nil {
		t.Fatal(err)
	}
	url, stop := startController(t, config)
	cookie := loginController(t, url)
	request := func(method, path string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, url+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(cookie)
		response := doControllerRequest(t, req)
		body, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read incident response: %v %v", err, closeErr)
		}
		return response.StatusCode, body
	}
	type incident struct {
		ID             string `json:"id"`
		OwnerID        string `json:"owner_id"`
		OwnerName      string `json:"owner_name"`
		Status         string `json:"status"`
		CloseReason    string `json:"close_reason"`
		AcknowledgedAt string `json:"acknowledged_at"`
	}
	list := func() []incident {
		t.Helper()
		status, body := request(http.MethodGet, "/api/v1/incidents")
		if status != http.StatusOK {
			t.Fatalf("list incidents = %d %s", status, body)
		}
		var data struct {
			Incidents []incident `json:"incidents"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatal(err)
		}
		return data.Incidents
	}
	wait := func(count int, status, reason string) []incident {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			items := list()
			if len(items) == count && items[0].Status == status && items[0].CloseReason == reason {
				return items
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("incident history did not reach %d %s %s: %+v", count, status, reason, list())
		return nil
	}
	emitAlertMetric(t, url, token, owner, alertMetricFixture{status: 500})
	waitNativeState(t, url, cookie, "incident-rule", "firing", time.Time{})
	opened := wait(1, "open", "")[0]
	if opened.OwnerID != owner.ID || opened.OwnerName != owner.Name {
		t.Fatalf("missing incident owner: %+v", opened)
	}
	status, ack := request(http.MethodPost, "/api/v1/incidents/"+opened.ID+"/acknowledgment")
	if status != http.StatusOK || !strings.Contains(string(ack), `"acknowledged_at"`) {
		t.Fatalf("acknowledge = %d %s", status, ack)
	}
	status, repeated := request(http.MethodPost, "/api/v1/incidents/"+opened.ID+"/acknowledgment")
	if status != http.StatusOK || string(repeated) != string(ack) {
		t.Fatalf("acknowledgment must be idempotent: %d %s", status, repeated)
	}
	stop()
	url, _ = startController(t, config)
	waitNativeState(t, url, cookie, "incident-rule", "firing", time.Now())
	items := wait(1, "open", "")
	if items[0].ID != opened.ID || items[0].AcknowledgedAt == "" {
		t.Fatalf("restart duplicated or changed acknowledgment: %+v", items)
	}
	emitAlertMetric(t, url, token, owner, alertMetricFixture{status: 200})
	wait(1, "closed", "condition_ended")
	emitAlertMetric(t, url, token, owner, alertMetricFixture{status: 500})
	newest := wait(2, "open", "")[0]
	if newest.ID == opened.ID || newest.AcknowledgedAt != "" {
		t.Fatalf("new episode reused the previous incident: %+v", newest)
	}
	status, body := request(http.MethodDelete, "/api/v1/monitors/"+owner.ID)
	if status != http.StatusNoContent {
		t.Fatalf("delete owner = %d %s", status, body)
	}
	wait(2, "closed", "rule_removed")
	status, body = request(http.MethodGet, "/api/v1/incidents/"+opened.ID)
	if status != http.StatusOK || !strings.Contains(string(body), owner.Name) {
		t.Fatalf("deleted resource lost history = %d %s", status, body)
	}
	status, body = request(http.MethodGet, "/api/v1/incidents?limit=1")
	if status != http.StatusOK || !strings.Contains(string(body), `"next_before":"`+newest.ID+`"`) {
		t.Fatalf("history pagination = %d %s", status, body)
	}
	status, body = request(http.MethodGet, "/api/v1/incidents?limit=1&before="+newest.ID)
	if status != http.StatusOK || !strings.Contains(string(body), `"id":"`+opened.ID+`"`) {
		t.Fatalf("older history = %d %s", status, body)
	}
}
