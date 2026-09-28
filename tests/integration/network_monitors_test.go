package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestMonitorAPIAndReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arveld.db")
	db := testutil.OpenDatabase(t, path)
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	const common = `"name":"  Monitored service  ","agent_instance_uid":"01000000-0000-0000-0000-000000000000","interval_seconds":30,"timeout_seconds":5,`
	want := make(map[string]map[string]any)
	for _, test := range []struct{ protocol, fields string }{
		{"http", `"endpoint":"https://example.com","method":"GET"`},
		{"tcp", `"endpoint":"localhost:5432"`},
		{"icmp", `"endpoint":"127.0.0.1","ping_count":3`},
		{"dns", `"endpoint":"example.com","dns_server":"1.1.1.1:53","record_type":"A","transport":"udp"`},
	} {
		response := request(http.MethodPost, "/api/v1/monitors/"+test.protocol, "{"+common+test.fields+"}")
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", test.protocol, response.Code, response.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		id, ok := value["id"].(string)
		if !ok || id == "" || value["name"] != "Monitored service" {
			t.Fatalf("invalid Monitor response: %v", value)
		}
		if response.Header().Get("Location") != "/api/v1/monitors/"+test.protocol+"/"+id || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid creation headers: %v", response.Header())
		}
		if test.protocol == "http" {
			if _, exists := value["protocol"]; exists {
				t.Fatal("legacy HTTP creation gained a protocol field")
			}
		} else if value["protocol"] != test.protocol {
			t.Fatalf("incorrect protocol in creation response: %v", value)
		}
		for _, protocol := range []string{"http", "tcp", "icmp", "dns"} {
			if protocol != test.protocol {
				if wrong := request(http.MethodGet, "/api/v1/monitors/"+protocol+"/"+id, ""); wrong.Code != http.StatusNotFound {
					t.Fatalf("cross-protocol read returned %d", wrong.Code)
				}
			}
		}
		value["protocol"] = test.protocol
		want[id] = value
		for _, path := range []string{"/api/v1/monitors/" + id, "/api/v1/monitors/" + test.protocol + "/" + id} {
			read := request(http.MethodGet, path, "")
			var got map[string]any
			if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &got) != nil {
				t.Fatalf("read %s: %d %s", path, read.Code, read.Body.String())
			}
			got["protocol"] = test.protocol
			if !reflect.DeepEqual(got, value) {
				t.Fatalf("read changed settings: %+v, want %+v", got, value)
			}
			if head := request(http.MethodHead, path, ""); head.Code != http.StatusOK || head.Body.Len() != 0 {
				t.Fatalf("HEAD %s: %d %s", path, head.Code, head.Body.String())
			}
		}
	}
	store := remoteconfig.NewStore(db)
	desired, err := store.Desired(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	var spec configuration.Specification
	if err := json.Unmarshal(desired.Specification, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.HTTPMonitors) != 1 || len(spec.TCPMonitors) != 1 || len(spec.ICMPMonitors) != 1 || len(spec.DNSMonitors) != 1 {
		t.Fatalf("incomplete Agent specification: %+v", spec)
	}
	for _, receiver := range []string{"hostmetrics:", "http_check/", "tcp_check/", "icmpcheckreceiver/", "dns_check/", "${env:ARVELD_AGENT_TOKEN}"} {
		if !strings.Contains(string(desired.Content), receiver) {
			t.Fatalf("missing %s from Agent configuration", receiver)
		}
	}
	if err := store.ReconcileAgent(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.Desired(t.Context(), uid)
	if err != nil || unchanged.Number != desired.Number {
		t.Fatalf("unchanged inputs added a revision: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testutil.OpenDatabase(t, path)
	handler = newAccountHandler(db)
	response := request(http.MethodGet, "/api/v1/monitors", "")
	var inventory struct {
		Monitors []map[string]any `json:"monitors"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &inventory) != nil || len(inventory.Monitors) != 4 {
		t.Fatalf("reopened inventory: %d %s", response.Code, response.Body.String())
	}
	lastID := ""
	for _, value := range inventory.Monitors {
		id, ok := value["id"].(string)
		if !ok || id <= lastID || !reflect.DeepEqual(value, want[id]) {
			t.Fatalf("reopened or unsorted Monitor: %v", value)
		}
		lastID = id
	}
}

func TestNetworkMonitorAPIRejectsFieldsFromOtherProtocols(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: agent.InstanceUID{1}}); err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	const common = `"name":"Network service","agent_instance_uid":"01000000-0000-0000-0000-000000000000","interval_seconds":30,"timeout_seconds":5,`
	for _, test := range []struct{ protocol, fields string }{
		{"tcp", `"endpoint":"localhost:5432"`},
		{"icmp", `"endpoint":"127.0.0.1","ping_count":3`},
		{"dns", `"endpoint":"example.com","dns_server":"1.1.1.1:53","record_type":"A","transport":"udp"`},
	} {
		for _, field := range []struct {
			protocol, name string
			values         []string
		}{
			{"icmp", "ping_count", []string{"3", "0", "null"}},
			{"dns", "dns_server", []string{`"1.1.1.1:53"`, `""`, "null"}},
			{"dns", "record_type", []string{`"A"`, `""`, "null"}},
			{"dns", "transport", []string{`"udp"`, `""`, "null"}},
		} {
			if field.protocol == test.protocol {
				continue
			}
			t.Run(test.protocol+"/"+field.name, func(t *testing.T) {
				for _, value := range field.values {
					body := "{" + common + test.fields + `,"` + field.name + `":` + value + "}"
					req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
						"https://arveld.example/api/v1/monitors/"+test.protocol, strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.AddCookie(cookie)
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, req)
					if response.Code != http.StatusBadRequest {
						t.Errorf("foreign field %s=%s returned %d, want 400", field.name, value, response.Code)
					}
				}
			})
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://arveld.example/api/v1/monitors", nil)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"monitors":[]}` {
		t.Fatalf("foreign fields must not create Monitors: %d %s", response.Code, response.Body.String())
	}
}

func TestNetworkMonitorAPIRejectsInvalidInputAndUnauthorizedAccess(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: agent.InstanceUID{1}}); err != nil {
		t.Fatal(err)
	}
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	_, readKey, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writeKey, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, session *http.Cookie, token, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	const common = `"name":"Network service","agent_instance_uid":"01000000-0000-0000-0000-000000000000","interval_seconds":30,"timeout_seconds":5,`
	for _, test := range []struct{ protocol, fields string }{
		{"tcp", `"endpoint":"localhost:5432"`},
		{"icmp", `"endpoint":"127.0.0.1","ping_count":3`},
		{"dns", `"endpoint":"example.com","dns_server":"1.1.1.1:53","record_type":"A","transport":"udp"`},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			path := "/api/v1/monitors/" + test.protocol
			valid := "{" + common + test.fields + "}"
			for _, denied := range []struct {
				body          string
				session       *http.Cookie
				token, origin string
				status        int
			}{
				{valid, nil, "", "", 401},
				{valid, nil, readKey, "", 403},
				{valid, cookie, "", "https://foreign.example", 403},
				{`{`, cookie, "", "", 400},
				{strings.Replace(valid, `"name":`, `"id":"chosen","name":`, 1), cookie, "", "", 400},
				{strings.Replace(valid, `"name":`, `"method":"GET","name":`, 1), cookie, "", "", 400},
				{strings.Replace(valid, "Network service", " ", 1), cookie, "", "", 422},
				{strings.Replace(valid, "01000000", "02000000", 1), cookie, "", "", 422},
				{strings.Replace(valid, `"timeout_seconds":5`, `"timeout_seconds":31`, 1), cookie, "", "", 422},
			} {
				response := request(http.MethodPost, path, denied.body, denied.session, denied.token, denied.origin)
				if response.Code != denied.status || response.Header().Get("Location") != "" {
					t.Fatalf("rejected %s: %d %s, want %d", path, response.Code, response.Body.String(), denied.status)
				}
			}
			if empty := request(http.MethodGet, path, "", nil, readKey, ""); empty.Code != 200 || strings.TrimSpace(empty.Body.String()) != `{"monitors":[]}` {
				t.Fatalf("rejections wrote a Monitor: %d %s", empty.Code, empty.Body.String())
			}
			response := request(http.MethodPost, path, valid, nil, writeKey, "")
			if response.Code != 201 {
				t.Fatalf("write-key creation = %d %s", response.Code, response.Body.String())
			}
			if head := request(http.MethodHead, path, "", nil, readKey, ""); head.Code != 200 || head.Body.Len() != 0 {
				t.Fatalf("HEAD = %d %s", head.Code, head.Body.String())
			}
			if missing := request(http.MethodGet, path+"/missing", "", cookie, "", ""); missing.Code != 404 {
				t.Fatalf("missing Monitor = %d", missing.Code)
			}
		})
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_network_creation BEFORE INSERT ON monitors BEGIN SELECT RAISE(ABORT, 'private storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	failed := request(http.MethodPost, "/api/v1/monitors/tcp", "{"+common+`"endpoint":"localhost:5432"}`, cookie, "", "")
	if failed.Code != 500 || failed.Body.String() != "Internal Server Error\n" {
		t.Fatalf("storage failure = %d %s", failed.Code, failed.Body.String())
	}
}
