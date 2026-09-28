package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
	"go.yaml.in/yaml/v3"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerReconcilesPersistedHTTPMonitorsOnReconnect(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	uid, otherUID := agent.InstanceUID{0x12, 15: 0xab}, agent.InstanceUID{2}
	url, stop := startController(t, config)
	message := &protobufs.AgentToServer{
		InstanceUid: uid[:],
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
			uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig),
	}
	base := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	if base == nil {
		t.Fatal("new Agent was not offered its base configuration")
	}
	stop()
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: otherUID}); err != nil {
		t.Fatal(err)
	}
	// Product changes made while the Agent is offline must survive controller restart.
	// Insert in reverse ID order to exercise stable input ordering.
	for _, value := range []monitor.Monitor{
		{Protocol: "http", ID: "status", Name: "Status", AgentInstanceUID: uid, Endpoint: "http://example.net/status", Method: http.MethodHead, IntervalSeconds: 15, TimeoutSeconds: 3},
		{Protocol: "http", ID: "foreign", Name: "Other Agent", AgentInstanceUID: otherUID, Endpoint: "https://other.example.com", Method: http.MethodGet, IntervalSeconds: 30, TimeoutSeconds: 5},
		{Protocol: "http", ID: "homepage", Name: "Homepage", AgentInstanceUID: uid, Endpoint: "https://example.com/health?literal=${env:TOKEN}&price=$$", Method: http.MethodGet, IntervalSeconds: 60, TimeoutSeconds: 5},
	} {
		if err := monitor.NewStore(db).Create(t.Context(), value); err != nil {
			t.Fatal(err)
		}
	}
	url, stop = startController(t, config)
	message.RemoteConfigStatus = &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: base.GetConfigHash(), Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}
	updated := sendAgentMessage(t, url, token, message).GetRemoteConfig()
	if updated == nil {
		t.Fatal("reconnected Agent was not offered its persisted HTTP Monitors")
	}
	content := updated.GetConfig().GetConfigMap()[""].GetBody()
	assertPersistedHTTPMonitorYAML(t, content)
	hash := sha256.Sum256(content)
	if !bytes.Equal(updated.GetConfigHash(), hash[:]) {
		t.Fatal("offered hash does not identify the published YAML")
	}
	store := remoteconfig.NewStore(db)
	desired, err := store.Desired(t.Context(), uid)
	if err != nil || desired.Number != 2 || !bytes.Equal(desired.Content, content) {
		t.Fatalf("desired configuration = %+v, %v; want the offered artifact at revision 2", desired, err)
	}
	var specification configuration.Specification
	if err := json.Unmarshal(desired.Specification, &specification); err != nil {
		t.Fatal(err)
	}
	want := configuration.NewSpecification(uid)
	want.HTTPMonitors = []configuration.HTTPMonitor{
		{ID: "homepage", Endpoint: "https://example.com/health?literal=${env:TOKEN}&price=$$", Method: http.MethodGet, IntervalSeconds: 60, TimeoutSeconds: 5},
		{ID: "status", Endpoint: "http://example.net/status", Method: http.MethodHead, IntervalSeconds: 15, TimeoutSeconds: 3},
	}
	if !reflect.DeepEqual(specification, want) {
		t.Fatalf("published inputs = %+v, want %+v", specification, want)
	}
	// A lost offer is recovered from the same durable artifact after restart.
	stop()
	url, _ = startController(t, config)
	if retry := sendAgentMessage(t, url, token, message).GetRemoteConfig(); !bytes.Equal(retry.GetConfigHash(), hash[:]) {
		t.Fatal("restart did not recover the committed Monitor configuration")
	}
	message.RemoteConfigStatus.LastRemoteConfigHash = hash[:]
	for range 2 {
		if sendAgentMessage(t, url, token, message).GetRemoteConfig() != nil {
			t.Fatal("unchanged acknowledged Monitor inputs caused another offer")
		}
	}
	history, err := store.ListRevisions(t.Context(), uid)
	if err != nil || len(history) != 2 {
		t.Fatalf("configuration history = %+v, %v; want exactly base and Monitor revisions", history, err)
	}
	original, err := store.RevisionContent(t.Context(), uid, 1)
	if err != nil || !bytes.Equal(original, base.GetConfig().GetConfigMap()[""].GetBody()) {
		t.Fatal("Monitor publication changed historical base bytes")
	}
}

func assertPersistedHTTPMonitorYAML(t *testing.T, content []byte) {
	t.Helper()
	decode := func(content []byte) map[string]any {
		t.Helper()
		var document map[string]any
		if err := yaml.Unmarshal(content, &document); err != nil {
			t.Fatal(err)
		}
		return document
	}
	object := func(value any) map[string]any {
		t.Helper()
		result, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("Collector section = %T, want a mapping", value)
		}
		return result
	}
	got := decode(content)
	contributions, err := os.ReadFile("../../internal/configuration/testdata/http-monitors.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := decode(contributions)
	for _, section := range []string{"receivers", "processors"} {
		actual := object(got[section])
		for name, expected := range object(want[section]) {
			if !reflect.DeepEqual(actual[name], expected) {
				t.Fatalf("%s.%s = %#v, want %#v", section, name, actual[name], expected)
			}
			delete(actual, name)
		}
	}
	pipelines := object(object(got["service"])["pipelines"])
	for name, expected := range object(object(want["service"])["pipelines"]) {
		if !reflect.DeepEqual(pipelines[name], expected) {
			t.Fatalf("pipeline %s = %#v, want %#v", name, pipelines[name], expected)
		}
		delete(pipelines, name)
	}
	base, err := os.ReadFile("../../internal/configuration/testdata/base-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, decode(base)) {
		t.Fatal("persisted Monitor contributions changed the base or included another Agent's Monitor")
	}
}
