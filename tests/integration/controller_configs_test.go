package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerKeepsFailedTargetAfterAgentRecoveryAndRestart(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	url, stop := startController(t, config)
	cookie := loginController(t, url)
	uid := agent.InstanceUID{1}
	capabilities := uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig) |
		uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig)
	send := func(status *protobufs.RemoteConfigStatus) *protobufs.ServerToAgent {
		t.Helper()
		return sendAgentMessage(t, url, token, &protobufs.AgentToServer{InstanceUid: uid[:], Capabilities: capabilities, RemoteConfigStatus: status})
	}
	// The initial host metrics configuration occupies revision 1.
	send(nil)
	path := "/api/v1/agents/" + uid.String() + "/config"
	contents := []string{"receivers: {}", "receivers: {unconfirmed: {}}", "receivers: {invalid: {}}"}
	var firstHash, failedHash []byte
	for index, content := range contents {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url+path, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-yaml")
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close controller response: %v", err)
			}
		}()
		var saved struct {
			Revision int64 `json:"revision"`
		}
		if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || saved.Revision != int64(index+2) {
			t.Fatalf("saved revision = %+v, status %d", saved, response.StatusCode)
		}
		message := send(nil)
		remote := message.GetRemoteConfig()
		wantHash := sha256.Sum256([]byte(content))
		if remote == nil || !bytes.Equal(remote.GetConfigHash(), wantHash[:]) || !bytes.Equal(remote.GetConfig().GetConfigMap()[""].GetBody(), []byte(content)) {
			t.Fatal("controller did not deliver the HTTP configuration over OpAMP")
		}
		if index == 0 {
			firstHash = bytes.Clone(remote.GetConfigHash())
			if send(&protobufs.RemoteConfigStatus{
				LastRemoteConfigHash: firstHash,
				Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
			}).GetRemoteConfig() != nil {
				t.Fatal("confirmed working configuration was offered again")
			}
		} else if index == len(contents)-1 {
			failedHash = bytes.Clone(remote.GetConfigHash())
		}
	}
	failed := send(&protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: failedHash,
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
		ErrorMessage:         "unknown receiver type",
	})
	if failed.GetRemoteConfig() != nil {
		t.Fatal("a failure must not offer a rollback or retry from the server")
	}
	checkStatus := func(reportedRevision int64) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url+path+"/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response := doControllerRequest(t, request)
		defer func() {
			if err := response.Body.Close(); err != nil {
				t.Errorf("close controller response: %v", err)
			}
		}()
		var status struct {
			State   string `json:"state"`
			Desired struct {
				Revision   int64  `json:"revision"`
				ConfigHash string `json:"config_hash"`
			} `json:"desired"`
			Reported *struct {
				Revision *int64 `json:"revision"`
			} `json:"reported"`
			LastWorking *struct {
				Revision   *int64 `json:"revision"`
				ConfigHash string `json:"config_hash"`
				Status     string `json:"status"`
			} `json:"last_working"`
			LastFailure *struct {
				Revision     *int64 `json:"revision"`
				ConfigHash   string `json:"config_hash"`
				Status       string `json:"status"`
				ErrorMessage string `json:"error_message"`
			} `json:"last_failure"`
		}
		if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || status.State != "failed" || status.Desired.Revision != 4 || status.Desired.ConfigHash != hex.EncodeToString(failedHash) {
			t.Fatalf("failed target HTTP status = %+v, code %d", status, response.StatusCode)
		}
		if status.Reported == nil || status.Reported.Revision == nil || *status.Reported.Revision != reportedRevision {
			t.Fatalf("latest observation = %+v, want revision %d", status.Reported, reportedRevision)
		}
		working := status.LastWorking
		if working == nil || working.Revision == nil || *working.Revision != 2 || working.ConfigHash != hex.EncodeToString(firstHash) || working.Status != "applied" {
			t.Fatalf("last working configuration = %+v, want confirmed revision 2, not unconfirmed revision 3", working)
		}
		failure := status.LastFailure
		if failure == nil || failure.Revision == nil || *failure.Revision != 4 || failure.ConfigHash != hex.EncodeToString(failedHash) || failure.Status != "failed" || failure.ErrorMessage != "unknown receiver type" {
			t.Fatalf("HTTP failure = %+v", failure)
		}
	}
	checkStatus(4)
	// The Agent recovers locally; its report must not replace the server's intent.
	applied := send(&protobufs.RemoteConfigStatus{LastRemoteConfigHash: firstHash, Status: protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED})
	if applied.GetRemoteConfig() != nil {
		t.Fatal("local recovery caused an automatic reapplication of the failed target")
	}
	checkStatus(2)
	stop()
	url, _ = startController(t, config)
	for range 2 {
		if send(nil).GetRemoteConfig() != nil {
			t.Fatal("reconnection without a new report retried the failed target")
		}
	}
	checkStatus(2)
}

func sendAgentMessage(t *testing.T, url, token string, message *protobufs.AgentToServer) *protobufs.ServerToAgent {
	t.Helper()
	body, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+opamp.Path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Authorization", "Bearer "+token)
	response := doControllerRequest(t, request)
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close controller response: %v", err)
		}
	}()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OpAMP status = %d, body %q", response.StatusCode, payload)
	}
	var received protobufs.ServerToAgent
	if err := proto.Unmarshal(payload, &received); err != nil {
		t.Fatal(err)
	}
	if received.GetErrorResponse() != nil || !bytes.Equal(received.GetInstanceUid(), message.GetInstanceUid()) {
		t.Fatalf("OpAMP response = %v", &received)
	}
	return &received
}
