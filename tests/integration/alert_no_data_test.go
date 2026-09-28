package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerDistinguishesMissingDataFromMonitorFailure(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	origin, stop := startController(t, config)
	cookie := loginController(t, origin)
	owner := createControllerHTTPMonitor(t, origin, cookie, uid)
	type ruleResponse struct {
		ID         string `json:"id"`
		MonitorID  string `json:"monitor_id"`
		Condition  string `json:"condition"`
		ForSeconds int    `json:"for_seconds"`
		Severity   string `json:"severity"`
	}
	rules := make(map[string]ruleResponse)
	for _, condition := range []string{"no_data", "failed"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
			origin+"/api/v1/monitors/"+owner.ID+"/alert-rules",
			strings.NewReader(fmt.Sprintf(`{"condition":%q,"for_seconds":10,"severity":"warning"}`, condition)))
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		response := doControllerRequest(t, request)
		var created ruleResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&created)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || created.ID == "" ||
			created.MonitorID != owner.ID || created.Condition != condition || created.ForSeconds != 10 || created.Severity != "warning" {
			t.Fatalf("create %s rule = %d, %+v, %v, %v", condition, response.StatusCode, created, decodeErr, closeErr)
		}
		rules[condition] = created
	}
	if rules["no_data"].ID == rules["failed"].ID {
		t.Fatal("missing data and failure must have independent rule identities")
	}
	// No measurements have ever been ingested: only the no-data rule activates.
	waitNativeState(t, origin, cookie, rules["no_data"].ID, "pending", time.Now())
	waitNativeState(t, origin, cookie, rules["failed"].ID, "inactive", time.Now())
	waitNativeState(t, origin, cookie, rules["no_data"].ID, "firing", time.Now())
	stop()
	origin, _ = startController(t, config)
	for _, want := range rules {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin+"/api/v1/alert-rules/"+want.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		var got ruleResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&got)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil || got != want {
			t.Fatalf("persisted rule after restart = %d, %+v, %v, %v; want %+v", response.StatusCode, got, decodeErr, closeErr, want)
		}
	}
	waitNativeState(t, origin, cookie, rules["no_data"].ID, "firing", time.Now())
	fixtureOwner := monitor.Monitor{ID: owner.ID, Protocol: "http", AgentInstanceUID: uid}
	emitAlertMetric(t, origin, token, fixtureOwner, alertMetricFixture{status: 500})
	waitNativeState(t, origin, cookie, rules["no_data"].ID, "inactive", time.Now())
	waitNativeState(t, origin, cookie, rules["failed"].ID, "firing", time.Now())
	emitAlertMetric(t, origin, token, fixtureOwner, alertMetricFixture{status: 200})
	evaluatedAfter := time.Now()
	waitNativeState(t, origin, cookie, rules["no_data"].ID, "inactive", evaluatedAfter)
	waitNativeState(t, origin, cookie, rules["failed"].ID, "inactive", evaluatedAfter)
}
