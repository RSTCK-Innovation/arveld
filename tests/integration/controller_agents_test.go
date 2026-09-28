package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	opampclient "github.com/open-telemetry/opamp-go/client"
	clienttypes "github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
)

func TestAgentAppearsInAPIAfterOpAMPMessage(t *testing.T) {
	ctx := t.Context()
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, stopController := startController(t, config)
	client := opampclient.NewWebSocket(nil)
	description := &protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			{
				Key: "service.name",
				Value: &protobufs.AnyValue{
					Value: &protobufs.AnyValue_StringValue{
						StringValue: "otelcol",
					},
				},
			},
			{
				Key: "service.version",
				Value: &protobufs.AnyValue{
					Value: &protobufs.AnyValue_StringValue{
						StringValue: "0.159.0",
					},
				},
			},
		},
		NonIdentifyingAttributes: []*protobufs.KeyValue{
			{
				Key: "host.name",
				Value: &protobufs.AnyValue{
					Value: &protobufs.AnyValue_StringValue{
						StringValue: "collector-01",
					},
				},
			},
		},
	}
	if err := client.SetAgentDescription(description); err != nil {
		t.Fatalf("set agent description: %v", err)
	}

	var instanceUID clienttypes.InstanceUid
	copy(instanceUID[:], "0123456789abcdef")
	acknowledged := make(chan struct{}, 1)
	settings := clienttypes.StartSettings{
		OpAMPServerURL: "ws://" + strings.TrimPrefix(url, "http://") + opamp.Path,
		InstanceUid:    instanceUID,
		Header: http.Header{
			"Authorization": {"Bearer " + token},
		},
		Callbacks: clienttypes.Callbacks{
			OnMessage: func(context.Context, *clienttypes.MessageData) {
				select {
				case acknowledged <- struct{}{}:
				default:
				}
			},
		},
	}
	if err := client.Start(ctx, settings); err != nil {
		t.Fatalf("start OpAMP client: %v", err)
	}
	clientStopped := false
	t.Cleanup(func() {
		if clientStopped {
			return
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Stop(stopCtx); err != nil {
			t.Errorf("stop OpAMP client: %v", err)
		}
	})

	select {
	case <-acknowledged:
	case <-time.After(2 * time.Second):
		t.Fatal("OpAMP client did not receive an acknowledgement")
	}

	cookie := loginController(t, url)
	response := getResponse(t, url+"/api/v1/agents", cookie)
	if response.StatusCode != http.StatusOK {
		t.Errorf("agents status code = %d, want %d", response.StatusCode, http.StatusOK)
	}
	agentsResponse := decodeAgentsResponse(t, response)
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close agents response: %v", err)
	}
	if len(agentsResponse.Agents) != 1 {
		t.Fatalf("agents response contains %d agents, want 1", len(agentsResponse.Agents))
	}
	storedAgent := agentsResponse.Agents[0]
	if got, want := storedAgent.InstanceUID, "30313233-3435-3637-3839-616263646566"; got != want {
		t.Errorf("instance UID = %q, want %q", got, want)
	}
	if storedAgent.Hostname == nil || *storedAgent.Hostname != "collector-01" {
		t.Errorf("hostname = %v, want collector-01", storedAgent.Hostname)
	}
	if storedAgent.Version == nil || *storedAgent.Version != "0.159.0" {
		t.Errorf("version = %v, want 0.159.0", storedAgent.Version)
	}
	if !storedAgent.Connected {
		t.Error("agent is disconnected, want connected")
	}
	if storedAgent.LastSeenAt == nil {
		t.Fatal("last seen at = nil, want a value")
	}
	lastSeenAt := *storedAgent.LastSeenAt

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := client.Stop(shutdownCtx)
	clientStopped = shutdownErr == nil
	cancel()
	if shutdownErr != nil {
		t.Fatalf("stop OpAMP client: %v", shutdownErr)
	}

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		agents, err := agent.NewStore(db).List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(agents) == 1 && !agents[0].Connected {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("agent remained connected after client shutdown")
		case <-ticker.C:
		}
	}
	response = getResponse(t, url+"/api/v1/agents", cookie)
	agentsResponse = decodeAgentsResponse(t, response)
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close agents response after disconnect: %v", err)
	}
	if len(agentsResponse.Agents) != 1 {
		t.Fatalf(
			"agents response after disconnect contains %d agents, want 1",
			len(agentsResponse.Agents),
		)
	}
	storedAgent = agentsResponse.Agents[0]
	if storedAgent.Connected {
		t.Error("agent is connected after disconnect, want disconnected")
	}
	if storedAgent.LastSeenAt == nil || storedAgent.LastSeenAt.Before(lastSeenAt) {
		t.Errorf("last seen at after client disconnect = %v, want at least %s", storedAgent.LastSeenAt, lastSeenAt)
	}
	reconnected := make(chan struct{}, 1)
	settings.Callbacks.OnMessage = func(context.Context, *clienttypes.MessageData) {
		select {
		case reconnected <- struct{}{}:
		default:
		}
	}
	secondClient := opampclient.NewWebSocket(nil)
	if err := secondClient.SetAgentDescription(description); err != nil {
		t.Fatal(err)
	}
	if err := secondClient.Start(ctx, settings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := secondClient.Stop(stopCtx); err != nil {
			t.Errorf("stop reconnected client: %v", err)
		}
	})
	select {
	case <-reconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("reconnected client received no acknowledgement")
	}
	response = getResponse(t, url+"/api/v1/agents", cookie)
	agentsResponse = decodeAgentsResponse(t, response)
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(agentsResponse.Agents) != 1 || !agentsResponse.Agents[0].Connected || agentsResponse.Agents[0].InstanceUID != storedAgent.InstanceUID {
		t.Fatalf("reconnection did not reuse the same agent: %+v", agentsResponse)
	}
	seenAfterReconnect := agentsResponse.Agents[0].LastSeenAt
	if seenAfterReconnect == nil || seenAfterReconnect.Before(lastSeenAt) {
		t.Fatal("reconnection lost last-seen timestamp")
	}
	stopController()
	agents, err := agent.NewStore(db).List(ctx)
	if err != nil || len(agents) != 1 || agents[0].Connected || agents[0].LastSeenAt == nil || !agents[0].LastSeenAt.Equal(*seenAfterReconnect) {
		t.Fatalf("controller shutdown did not persist disconnection: %+v, %v", agents, err)
	}
}

type agentsAPIResponse struct {
	Agents []agentAPIResponse `json:"agents"`
}

type agentAPIResponse struct {
	InstanceUID string     `json:"instance_uid"`
	Hostname    *string    `json:"hostname"`
	Version     *string    `json:"version"`
	Connected   bool       `json:"connected"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

func decodeAgentsResponse(
	t *testing.T,
	response *http.Response,
) agentsAPIResponse {
	t.Helper()

	var body agentsAPIResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode agents response: %v", err)
	}

	return body
}

func getResponse(t *testing.T, url string, cookie *http.Cookie) *http.Response {
	t.Helper()

	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		t.Fatalf("create GET request: %v", err)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}

	return doControllerRequest(t, request)
}
