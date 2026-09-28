package integration

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/agentauth"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/opamp"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerAuthenticatesOpAMPRequestsWithPersistedKeys(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	keys := agentauth.NewStore(db)
	key, token, err := keys.CreateKey(t.Context(), agentauth.CreateKeyParams{Name: "First agent"})
	if err != nil {
		t.Fatal(err)
	}
	otherToken := createAgentKey(t, db)
	url, _ := startController(t, config)
	message := &protobufs.AgentToServer{InstanceUid: []byte("0123456789abcdef")}
	sendAgentMessage(t, url, token, message)
	sendAgentMessage(t, url, otherToken, message)
	// Authentication must observe revocation without restarting the controller.
	if err := keys.RevokeKey(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	cookie := loginController(t, url)
	_, readToken, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Management read", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writeToken, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Management write", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	rejectedUID := []byte("fedcba9876543210")
	payload, err := proto.Marshal(&protobufs.AgentToServer{InstanceUid: rejectedUID})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		for _, test := range []struct {
			name          string
			authorization []string
			cookie        *http.Cookie
		}{
			{"revoked key", []string{"Bearer " + token}, nil},
			{"unknown key", []string{"Bearer unknown-token"}, nil},
			{"legacy static token", []string{"Bearer secret-token"}, nil},
			{"management read key", []string{"Bearer " + readToken}, nil},
			{"management write key", []string{"Bearer " + writeToken}, nil},
			{"duplicate headers with active key first", []string{"Bearer " + otherToken, "Bearer unknown-token"}, nil},
			{"browser session", nil, cookie},
		} {
			t.Run(method+"/"+test.name, func(t *testing.T) {
				request, err := http.NewRequestWithContext(t.Context(), method, url+opamp.Path, bytes.NewReader(payload))
				if err != nil {
					t.Fatal(err)
				}
				if method == http.MethodPost {
					request.Header.Set("Content-Type", "application/x-protobuf")
				} else {
					request.Header.Set("Connection", "Upgrade")
					request.Header.Set("Upgrade", "websocket")
					request.Header.Set("Sec-WebSocket-Version", "13")
					request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				}
				for _, header := range test.authorization {
					request.Header.Add("Authorization", header)
				}
				if test.cookie != nil {
					request.AddCookie(test.cookie)
				}
				response := doControllerRequest(t, request)
				defer func() {
					if err := response.Body.Close(); err != nil {
						t.Errorf("close rejected response: %v", err)
					}
				}()
				if response.StatusCode != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401 before processing OpAMP", response.StatusCode)
				}
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if len(body) != 0 || response.Header.Get("WWW-Authenticate") != "Bearer" {
					t.Errorf("response = %q, want empty 401 with Bearer challenge", body)
				}
			})
		}
	}
	agents, err := agent.NewStore(db).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || !bytes.Equal(agents[0].InstanceUID[:], message.GetInstanceUid()) {
		t.Fatal("rejected requests changed the registered agents")
	}
	sendAgentMessage(t, url, otherToken, message)
}

func TestOpAMPRejectsConnectionWhenKeyStorageIsUnavailable(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	token := createAgentKey(t, db)
	server, err := opamp.NewServer(agent.NewStore(db), remoteconfig.NewStore(db), testLogger(), agentauth.NewStore(db))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close OpAMP server: %v", err)
		}
	})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, opamp.Path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.Len() != 0 {
		t.Errorf("response = %d %q, want empty 500", response.Code, response.Body.String())
	}
	if response.Header().Get("WWW-Authenticate") != "" {
		t.Error("storage failure must not be reported as invalid credentials")
	}
}
