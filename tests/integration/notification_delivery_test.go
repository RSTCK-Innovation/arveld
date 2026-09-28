package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/components"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerDeliversNativeWebhookLifecycle(t *testing.T) {
	if os.Getenv("ARVELD_TEST_PROMETHEUS_BINARY") == "" || os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set both pinned engine executable paths to run native delivery")
	}
	config := controllerConfig(t, "")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	initial, err := notification.NewStore(db).AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(filepath.Dir(config.DatabasePath), initial); err != nil {
		t.Fatal(err)
	}
	config.alertmanagerURL = startNotificationAlertmanager(t, filepath.Dir(config.DatabasePath))
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath), config.alertmanagerURL)
	type delivery struct {
		Status string `json:"status"`
		Alerts []struct {
			Status      string            `json:"status"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"alerts"`
	}
	received := make(chan delivery, 10)
	var requests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("notification method = %s", r.Method)
		}
		var value delivery
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		select {
		case received <- value:
		default:
			t.Error("unexpected notification flood")
		}
	}))
	t.Cleanup(destination.Close)
	address, stop := startController(t, config)
	cookie := loginController(t, address)
	owner := createControllerHTTPMonitor(t, address, cookie, uid)
	renamed, err := monitor.NewStore(db).Get(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed.Name = `API <ops> {{ "literal name" }}`
	if _, err := monitor.NewStore(db).Update(t.Context(), renamed); err != nil {
		t.Fatal(err)
	}
	post := func(path string, body any) string {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, address+path, strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		response := doControllerRequest(t, request)
		var value struct {
			ID string `json:"id"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&value)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || value.ID == "" {
			t.Fatalf("create %s: HTTP %d, %v, %v", path, response.StatusCode, decodeErr, closeErr)
		}
		return value.ID
	}
	channelID := post("/api/v1/notifications", map[string]any{"name": "Local receiver", "type": "webhook", "config": map[string]string{"url": destination.URL}})
	ruleID := post("/api/v1/monitors/"+owner.ID+"/alert-rules", map[string]any{"condition": "failed", "for_seconds": 1, "severity": "critical", "notification_ids": []string{channelID}})
	definition, err := monitor.NewStore(db).Get(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	await := func(status string) {
		t.Helper()
		timer := time.NewTimer(55 * time.Second)
		defer timer.Stop()
		for {
			select {
			case value := <-received:
				if value.Status != status {
					continue
				}
				if len(value.Alerts) != 1 || value.Alerts[0].Status != status || value.Alerts[0].Labels["arveld_rule_id"] != ruleID || value.Alerts[0].Labels["arveld_monitor_id"] != owner.ID {
					t.Fatalf("unexpected native delivery: %+v", value)
				}
				if value.Alerts[0].Annotations["resource"] != renamed.Name || value.Alerts[0].Annotations["summary"] != "Check failed" || value.Alerts[0].Annotations["description"] == "" {
					t.Fatalf("notification lacks literal resource name and explanation: %+v", value.Alerts[0].Annotations)
				}
				return
			case <-timer.C:
				t.Fatalf("no %s notification received; requests=%d", status, requests.Load())
			}
		}
	}
	emitAlertMetric(t, address, token, definition, alertMetricFixture{status: 500})
	await("firing")
	if requests.Load() < 2 {
		t.Fatal("Alertmanager did not retry the rejected notification")
	}
	stop()
	address, _ = startController(t, config)
	emitAlertMetric(t, address, token, definition, alertMetricFixture{status: 200})
	await("resolved")
}

func TestControllerResumesNativeWebhooksAfterSilence(t *testing.T) {
	if os.Getenv("ARVELD_TEST_PROMETHEUS_BINARY") == "" || os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY") == "" {
		t.Skip("set both pinned engine executable paths to run native silence delivery")
	}
	config := controllerConfig(t, "")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	initial, err := notification.NewStore(db).AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := components.WriteAlertmanagerConfig(filepath.Dir(config.DatabasePath), initial); err != nil {
		t.Fatal(err)
	}
	config.alertmanagerURL = startNotificationAlertmanager(t, filepath.Dir(config.DatabasePath))
	config.prometheusURL = startRulePrometheus(t, filepath.Dir(config.DatabasePath), config.alertmanagerURL)
	type delivery struct {
		Status string `json:"status"`
		Alerts []struct {
			Status string            `json:"status"`
			Labels map[string]string `json:"labels"`
		} `json:"alerts"`
		receivedAt time.Time
	}
	received := make(chan delivery, 10)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value delivery
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value.receivedAt = time.Now()
		select {
		case received <- value:
		default:
			t.Error("unexpected notification flood")
		}
	}))
	t.Cleanup(destination.Close)
	address, _ := startController(t, config)
	cookie := loginController(t, address)
	post := func(path string, body any) string {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, address+path, strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		response := doControllerRequest(t, request)
		var value struct {
			ID string `json:"id"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&value)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusCreated || decodeErr != nil || closeErr != nil || value.ID == "" {
			t.Fatalf("create %s: HTTP %d, %v, %v", path, response.StatusCode, decodeErr, closeErr)
		}
		return value.ID
	}
	channelID := post("/api/v1/notifications", map[string]any{"name": "Silence receiver", "type": "webhook", "config": map[string]string{"url": destination.URL}})
	owners := make(map[string]monitor.Monitor)
	ruleIDs := make(map[string]string)
	for _, name := range []string{"cancel", "expire", "control"} {
		owner := createControllerHTTPMonitor(t, address, cookie, uid)
		definition, err := monitor.NewStore(db).Get(t.Context(), owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		owners[name] = definition
		ruleIDs[owner.ID] = post("/api/v1/monitors/"+owner.ID+"/alert-rules", map[string]any{
			"condition": "failed", "for_seconds": 1, "severity": "critical", "notification_ids": []string{channelID},
		})
	}
	silenceIDs := make(map[string]string)
	for name, duration := range map[string]int{"cancel": 300, "expire": 60} {
		input := map[string]any{
			"duration_seconds": duration, "comment": "Native delivery proof",
		}
		if name == "expire" {
			input["starts_at"] = time.Now().Add(3 * time.Second).UTC()
		}
		silenceIDs[name] = post("/api/v1/monitors/"+owners[name].ID+"/silences", input)
	}
	readSilence := func(name string) notification.Silence {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			address+"/api/v1/monitors/"+owners[name].ID+"/silences", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		var value struct {
			Silences []notification.Silence `json:"silences"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&value)
		closeErr := response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || closeErr != nil || len(value.Silences) != 1 || value.Silences[0].ID != silenceIDs[name] {
			t.Fatalf("read %s silence: HTTP %d, %+v, %v, %v", name, response.StatusCode, value, decodeErr, closeErr)
		}
		return value.Silences[0]
	}
	scheduled := readSilence("expire")
	if scheduled.State != "pending" {
		t.Fatalf("future silence must remain pending: %+v", scheduled)
	}
	deadline := scheduled.StartsAt.Add(5 * time.Second)
	for readSilence("expire").State != "active" {
		if time.Now().After(deadline) {
			t.Fatal("scheduled silence did not activate")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if time.Now().Before(scheduled.StartsAt) {
		t.Fatal("scheduled silence activated before its start")
	}
	expiresAt := scheduled.EndsAt
	assertDelivery := func(value delivery) string {
		t.Helper()
		if value.Status != "firing" || len(value.Alerts) != 1 || value.Alerts[0].Status != "firing" {
			t.Fatalf("unexpected native delivery: %+v", value)
		}
		id := value.Alerts[0].Labels["arveld_monitor_id"]
		if ruleIDs[id] == "" || value.Alerts[0].Labels["arveld_rule_id"] != ruleIDs[id] {
			t.Fatalf("unexpected native delivery labels: %+v", value)
		}
		return id
	}
	started := time.Now()
	for _, owner := range owners {
		emitAlertMetric(t, address, token, owner, alertMetricFixture{status: 500})
	}
	for _, id := range ruleIDs {
		waitNativeState(t, address, cookie, id, "firing", started)
	}
	select {
	case value := <-received:
		if assertDelivery(value) != owners["control"].ID {
			t.Fatalf("silenced Monitor delivered before the control: %+v", value)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("unsilenced control did not deliver")
	}
	// Observe a full group_wait after the positive control while both silences remain active.
	if time.Until(expiresAt) < 10*time.Second {
		t.Fatal("expiration window elapsed before suppression could be verified")
	}
	select {
	case value := <-received:
		t.Fatalf("unexpected delivery while silences are active: %+v", value)
	case <-time.After(7 * time.Second):
	}
	for name := range silenceIDs {
		if value := readSilence(name); value.State != "active" {
			t.Fatalf("expected active silence for %s, got %+v", name, value)
		}
	}
	cancelledAt := time.Now()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete,
		address+"/api/v1/monitors/"+owners["cancel"].ID+"/silences/"+silenceIDs["cancel"], nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	response := doControllerRequest(t, request)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusNoContent || closeErr != nil {
		t.Fatalf("cancel silence: HTTP %d, %v", response.StatusCode, closeErr)
	}
	// Fresh failures keep evaluation firing while native grouping schedules delivery.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()
	remaining := map[string]time.Time{owners["cancel"].ID: cancelledAt, owners["expire"].ID: expiresAt}
	for len(remaining) > 0 {
		select {
		case value := <-received:
			id := assertDelivery(value)
			eligibleAt, exists := remaining[id]
			if !exists || value.receivedAt.Before(eligibleAt) {
				t.Fatalf("unexpected or premature delivery: %+v; remaining: %+v", value, remaining)
			}
			delete(remaining, id)
		case <-ticker.C:
			for _, owner := range owners {
				emitAlertMetric(t, address, token, owner, alertMetricFixture{status: 500})
			}
		case <-timer.C:
			t.Fatalf("notifications did not resume: %+v", remaining)
		}
	}
	for name := range silenceIDs {
		if value := readSilence(name); value.State != "expired" {
			t.Fatalf("expected expired silence for %s, got %+v", name, value)
		}
		waitNativeState(t, address, cookie, ruleIDs[owners[name].ID], "firing", cancelledAt)
	}
}

func startNotificationAlertmanager(t *testing.T, directory string) string {
	t.Helper()
	binary := os.Getenv("ARVELD_TEST_ALERTMANAGER_BINARY")
	logPath := filepath.Join(directory, "alertmanager-test.log")
	log, err := os.Create(logPath) //nolint:gosec // Generated path in the test directory.
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	command := exec.CommandContext(ctx, binary, //nolint:gosec // Explicit test executable and generated arguments, no shell.
		"--config.file="+filepath.Join(directory, "config", "alertmanager.yml"), "--storage.path="+filepath.Join(directory, "alertmanager-test-data"),
		"--web.listen-address=127.0.0.1:0", "--cluster.listen-address=", "--log.format=json")
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		cancel()
		_ = log.Close() //nolint:errcheck // Cleanup after the primary startup failure.
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait() //nolint:errcheck // CommandContext kills the test-owned process.
		if err := log.Close(); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(15 * time.Second)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath) //nolint:gosec // Generated path in the test directory.
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			var entry struct {
				Message string `json:"msg"`
				Address string `json:"address"`
			}
			if json.Unmarshal([]byte(line), &entry) != nil || entry.Message != "Listening on" || entry.Address == "" {
				continue
			}
			address := "http://" + entry.Address
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address+"/-/ready", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if err == nil {
				if err := response.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode == http.StatusOK {
					return address
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Alertmanager did not become ready; log: %s", logPath)
	return ""
}
