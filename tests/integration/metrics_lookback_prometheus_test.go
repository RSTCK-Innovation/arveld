package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMetricsQueriesKeepShortDefaultLookback(t *testing.T) {
	config := controllerConfig(t, "")
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath))
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	origin, _ := startController(t, config)
	cookie := loginController(t, origin)
	owner := monitor.Monitor{ID: "slow-monitor", Protocol: "tcp", AgentInstanceUID: agent.InstanceUID{1}}
	emitAlertMetric(t, origin, token, owner, alertMetricFixture{age: 600, status: 0})
	expression := fmt.Sprintf(`tcpcheck_status_ratio{job="arveld-agent",instance=%q,arveld_monitor_id=%q}`, owner.AgentInstanceUID.String(), owner.ID)
	for _, endpoint := range []string{"query", "query_range"} {
		for _, lookback := range []string{"", "20m"} {
			t.Run(endpoint+"/lookback="+lookback, func(t *testing.T) {
				parameters := url.Values{"query": {expression}}
				if endpoint == "query_range" {
					now := time.Now().UTC()
					parameters.Set("start", now.Add(-time.Second).Format(time.RFC3339Nano))
					parameters.Set("end", now.Format(time.RFC3339Nano))
					parameters.Set("step", "1s")
				}
				if lookback != "" {
					parameters.Set("lookback_delta", lookback)
				}
				request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
					origin+"/api/v1/metrics/"+endpoint+"?"+parameters.Encode(), nil)
				if err != nil {
					t.Fatal(err)
				}
				request.AddCookie(cookie)
				response := doControllerRequest(t, request)
				var envelope struct {
					Status string `json:"status"`
					Data   struct {
						Result []json.RawMessage `json:"result"`
					} `json:"data"`
				}
				decodeErr := json.NewDecoder(response.Body).Decode(&envelope)
				closeErr := response.Body.Close()
				if response.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil || envelope.Status != "success" {
					t.Fatalf("metric query = %d, %v, %v, %+v", response.StatusCode, decodeErr, closeErr, envelope)
				}
				want := 0
				if lookback != "" {
					want = 1
				}
				if len(envelope.Data.Result) != want {
					t.Fatalf("ten-minute-old measurement: got %d series, want %d (lookback %q)", len(envelope.Data.Result), want, lookback)
				}
			})
		}
	}
}
