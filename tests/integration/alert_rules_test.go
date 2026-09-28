package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/auth"
	"github.com/RSTCK-Innovation/arveld/internal/monitor"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerPersistsMonitorAlertRuleAfterRestart(t *testing.T) {
	for _, condition := range []string{"failed", "no_data", "latency"} {
		t.Run(condition, func(t *testing.T) {
			config := controllerConfig(t, "http://127.0.0.1:1")
			db := testutil.OpenDatabase(t, config.DatabasePath)
			createAdministrator(t, db)
			uid := agent.InstanceUID{1}
			if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			url, stop := startController(t, config)
			cookie := loginController(t, url)
			owner := createControllerHTTPMonitor(t, url, cookie, uid)
			request := func(method, path, body string) (int, http.Header, []byte) {
				t.Helper()
				req, err := http.NewRequestWithContext(t.Context(), method, url+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(cookie)
				response := doControllerRequest(t, req)
				content, err := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("read alert rule response: %v, %v", err, closeErr)
				}
				return response.StatusCode, response.Header, content
			}
			threshold := 0.0
			extra := ""
			if condition == "latency" {
				threshold, extra = 500.5, `,"threshold":500.5`
			}
			status, headers, content := request(http.MethodPost, "/api/v1/monitors/"+owner.ID+"/alert-rules",
				fmt.Sprintf(`{"condition":%q,"for_seconds":120,"severity":"critical"%s}`, condition, extra))
			if status != http.StatusCreated {
				t.Fatalf("create Monitor alert rule = %d %s, want 201", status, content)
			}
			type ruleResponse struct {
				ID         string  `json:"id"`
				MonitorID  string  `json:"monitor_id"`
				Condition  string  `json:"condition"`
				ForSeconds int     `json:"for_seconds"`
				Severity   string  `json:"severity"`
				Threshold  float64 `json:"threshold"`
			}
			var created ruleResponse
			if err := json.Unmarshal(content, &created); err != nil {
				t.Fatal(err)
			}
			if created.ID == "" || strings.ContainsAny(created.ID, "/: ") || created.MonitorID != owner.ID ||
				created.Condition != condition || created.ForSeconds != 120 || created.Severity != "critical" || created.Threshold != threshold {
				t.Fatalf("created alert rule = %+v, want an ID and the exact Monitor-owned settings", created)
			}
			location := "/api/v1/alert-rules/" + created.ID
			if headers.Get("Location") != location || headers.Get("Cache-Control") != "no-store" {
				t.Fatal("creation must return the rule's Location and prevent caching")
			}
			stop()
			url, _ = startController(t, config)
			status, headers, content = request(http.MethodGet, location, "")
			if status != http.StatusOK || headers.Get("Cache-Control") != "no-store" {
				t.Fatalf("read alert rule after restart = %d %s, want uncached 200", status, content)
			}
			var got ruleResponse
			if err := json.Unmarshal(content, &got); err != nil {
				t.Fatal(err)
			}
			if got != created {
				t.Fatalf("alert rule after restart = %+v, want %+v", got, created)
			}
			status, headers, content = request(http.MethodHead, location, "")
			if status != http.StatusOK || len(content) != 0 || headers.Get("Content-Type") != "application/json" {
				t.Fatalf("HEAD alert rule = %d %s, want 200 with JSON headers and no body", status, content)
			}
			otherOwner := createControllerHTTPMonitor(t, url, cookie, uid)
			status, otherHeaders, content := request(http.MethodPost, "/api/v1/monitors/"+otherOwner.ID+"/alert-rules",
				`{"condition":"failed","for_seconds":1,"severity":"info"}`)
			if status != http.StatusCreated || otherHeaders.Get("Location") == location {
				t.Fatalf("create another Monitor's rule = %d %s, want a distinct rule", status, content)
			}
			status, _, content = request(http.MethodDelete, "/api/v1/monitors/"+owner.ID, "")
			if status != http.StatusNoContent {
				t.Fatalf("delete Monitor with an alert rule = %d %s, want 204", status, content)
			}
			status, _, content = request(http.MethodGet, location, "")
			if status != http.StatusNotFound {
				t.Fatalf("rule of deleted Monitor = %d %s, want 404", status, content)
			}
			status, _, content = request(http.MethodGet, otherHeaders.Get("Location"), "")
			if status != http.StatusOK {
				t.Fatalf("another Monitor's rule after deletion = %d %s, want 200", status, content)
			}
		})
	}
}

func TestMonitorAlertRulesRejectInvalidOrUnauthorizedCreation(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	if err := monitor.NewStore(db).Create(t.Context(), monitor.Monitor{
		ID: "homepage", Name: "Homepage", Protocol: "http", AgentInstanceUID: uid,
		Endpoint: "https://example.com", Method: "GET", IntervalSeconds: 30, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	_, reader, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Reader", Permission: "read"})
	if err != nil {
		t.Fatal(err)
	}
	_, writer, err := auth.NewStore(db).CreateAPIKey(t.Context(), auth.CreateAPIKeyParams{Name: "Writer", Permission: "write"})
	if err != nil {
		t.Fatal(err)
	}
	agentKey := createAgentKey(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	request := func(method, path, body, token, origin string, session *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), method, "https://arveld.example"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if session != nil {
			req.AddCookie(session)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	const collection = "/api/v1/monitors/homepage/alert-rules"
	const valid = `{"condition":"failed","for_seconds":120,"severity":"critical"}`
	for _, invalid := range []struct {
		name, body string
		status     int
	}{
		{"unsupported condition", strings.Replace(valid, "failed", "unsupported", 1), 422},
		{"latency requires threshold", strings.Replace(valid, "failed", "latency", 1), 422},
		{"failure rejects threshold", `{"condition":"failed","for_seconds":120,"severity":"critical","threshold":500}`, 422},
		{"zero threshold", `{"condition":"latency","for_seconds":120,"severity":"critical","threshold":0}`, 422},
		{"excessive threshold", `{"condition":"latency","for_seconds":120,"severity":"critical","threshold":60001}`, 422},
		{"zero duration", strings.Replace(valid, "120", "0", 1), 422},
		{"negative duration", strings.Replace(valid, "120", "-1", 1), 422},
		{"duration over one day", strings.Replace(valid, "120", "86401", 1), 422},
		{"unknown severity", strings.Replace(valid, "critical", "urgent", 1), 422},
		{"missing fields", `{}`, 422},
		{"null rule", `null`, 422},
		{"fractional seconds", strings.Replace(valid, "120", "1.5", 1), 400},
		{"caller-supplied identity", strings.Replace(valid, `"condition":`, `"id":"chosen","condition":`, 1), 400},
		{"caller-supplied ownership", strings.Replace(valid, `"condition":`, `"monitor_id":"other","condition":`, 1), 400},
		{"malformed JSON", `{`, 400},
		{"multiple documents", valid + `{}`, 400},
		{"oversized body", valid + strings.Repeat(" ", 4096), 413},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			response := request(http.MethodPost, collection, invalid.body, "", "", cookie)
			if response.Code != invalid.status || response.Body.String() != http.StatusText(invalid.status)+"\n" {
				t.Fatalf("invalid alert rule = %d %q, want generic %d", response.Code, response.Body.String(), invalid.status)
			}
			if response.Header().Get("Location") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("rejected creation must not publish a Location or allow caching")
			}
		})
	}
	for _, access := range []struct {
		name, token, origin string
		session             *http.Cookie
		status              int
	}{
		{"anonymous", "", "", nil, 401},
		{"reader", reader, "", nil, 403},
		{"Agent credential", agentKey, "", nil, 401},
		{"invalid bearer overrides session", "invalid", "", cookie, 401},
		{"foreign origin", "", "https://foreign.example", cookie, 403},
	} {
		t.Run(access.name, func(t *testing.T) {
			response := request(http.MethodPost, collection, valid, access.token, access.origin, access.session)
			if response.Code != access.status || response.Header().Get("Location") != "" {
				t.Fatalf("unauthorized rule creation = %d, want %d without Location", response.Code, access.status)
			}
		})
	}
	missing := request(http.MethodPost, "/api/v1/monitors/missing/alert-rules", valid, writer, "", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("rule for missing Monitor = %d %s, want 404", missing.Code, missing.Body.String())
	}
	created := request(http.MethodPost, collection,
		`{"condition":"failed","for_seconds":86400,"severity":"warning"}`, writer, "", nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("write-key creation at maximum duration = %d %s, want 201", created.Code, created.Body.String())
	}
	location := created.Header().Get("Location")
	read := request(http.MethodGet, location, "", reader, "", nil)
	if read.Code != http.StatusOK || read.Body.String() != created.Body.String() {
		t.Fatalf("read-only key must retrieve the saved rule: %d %s", read.Code, read.Body.String())
	}
	if got := request(http.MethodGet, location, "", "", "", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous rule read = %d, want 401", got)
	}
	if got := request(http.MethodHead, "/api/v1/alert-rules/missing", "", reader, "", nil).Code; got != http.StatusNotFound {
		t.Fatalf("missing rule HEAD = %d, want 404", got)
	}
}
