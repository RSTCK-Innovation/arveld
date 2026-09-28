package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/remoteconfig"
)

func TestPutAgentConfigStoresDesiredRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{1}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	content := "receivers:\n  otlp:\n"
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"/api/v1/agents/"+agentUID.String()+"/config",
		strings.NewReader(content),
	)
	request.Header.Set("Content-Type", "application/x-yaml")
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"revision\":1}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	desired, err := configStore.Desired(t.Context(), agentUID)
	if err != nil {
		t.Fatalf("Desired() error = %v, want nil", err)
	}
	if desired.Number != 1 {
		t.Errorf("desired revision number = %d, want 1", desired.Number)
	}
	if !bytes.Equal(desired.Content, []byte(content)) {
		t.Errorf("desired revision content = %q, want %q", desired.Content, content)
	}
}

func TestPostAgentConfigRollbackRestoresPreviousRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{4}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	if _, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n"),
	); err != nil {
		t.Fatalf("store first configuration: %v", err)
	}
	if _, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n  prometheus:\n"),
	); err != nil {
		t.Fatalf("store second configuration: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/agents/"+agentUID.String()+"/config/rollback",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"revision\":1}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestPostAgentConfigRollbackReturnsConflictWithoutPreviousRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{5}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	if _, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n"),
	); err != nil {
		t.Fatalf("store configuration: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/v1/agents/"+agentUID.String()+"/config/rollback",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusConflict,
		)
	}
	if got, want := response.Body.String(), "Conflict\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestGetAgentConfigStatusReturnsDesiredAndReportedRevisions(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{6}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	firstRevision, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n"),
	)
	if err != nil {
		t.Fatalf("store first configuration: %v", err)
	}
	reportedAt := time.Date(
		2026,
		time.September,
		2,
		13,
		0,
		0,
		0,
		time.UTC,
	)
	if err := configStore.RecordStatus(t.Context(), agentUID, remoteconfig.StatusReport{
		ConfigHash: firstRevision.ConfigHash[:],
		Status:     remoteconfig.ApplyStatusApplied,
		ReportedAt: reportedAt,
	}); err != nil {
		t.Fatalf("record configuration status: %v", err)
	}
	if _, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n  prometheus:\n"),
	); err != nil {
		t.Fatalf("store second configuration: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents/"+agentUID.String()+"/config/status",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"state\":\"applying\",\"desired\":{\"revision\":2,\"config_hash\":\"d13bcbd9465f9da271d1b80a85b49c4159e7ab1f9ce9d77518b6bdcfee84e523\"},\"reported\":{\"revision\":1,\"config_hash\":\"b78a3cd731efa5449c24e08abafbba0cd20d4f2eb9612573365b1b1f38b26c53\",\"status\":\"applied\",\"error_message\":\"\",\"reported_at\":\"2026-09-02T13:00:00Z\"},\"last_working\":{\"revision\":1,\"config_hash\":\"b78a3cd731efa5449c24e08abafbba0cd20d4f2eb9612573365b1b1f38b26c53\",\"status\":\"applied\",\"error_message\":\"\",\"reported_at\":\"2026-09-02T13:00:00Z\"}}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestGetAgentConfigStatusUsesReportStateForDesiredRevision(t *testing.T) {
	tests := []struct {
		name   string
		status remoteconfig.ApplyStatus
	}{
		{name: "applying", status: remoteconfig.ApplyStatusApplying},
		{name: "applied", status: remoteconfig.ApplyStatusApplied},
		{name: "failed", status: remoteconfig.ApplyStatusFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
			createAdministrator(t, db)
			handler := newAccountHandler(db)
			cookie := login(t, handler, "a long password for testing")
			agentStore := agent.NewStore(db)
			configStore := remoteconfig.NewStore(db)
			agentUID := agent.InstanceUID{7}
			if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
				InstanceUID: agentUID,
			}); err != nil {
				t.Fatalf("store agent: %v", err)
			}

			desired, err := configStore.Save(
				t.Context(),
				agentUID,
				[]byte("receivers:\n  otlp:\n"),
			)
			if err != nil {
				t.Fatalf("store configuration: %v", err)
			}
			if err := configStore.RecordStatus(
				t.Context(),
				agentUID,
				remoteconfig.StatusReport{
					ConfigHash: desired.ConfigHash[:],
					Status:     test.status,
					ReportedAt: time.Date(
						2026,
						time.September,
						2,
						13,
						0,
						0,
						0,
						time.UTC,
					),
				},
			); err != nil {
				t.Fatalf("record configuration status: %v", err)
			}

			request := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				"/api/v1/agents/"+agentUID.String()+"/config/status",
				nil,
			)
			response := httptest.NewRecorder()

			request.AddCookie(cookie)
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf(
					"status code = %d, want %d",
					response.Code,
					http.StatusOK,
				)
			}
			var body struct {
				State remoteconfig.ApplyStatus `json:"state"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.State != test.status {
				t.Errorf("state = %q, want %q", body.State, test.status)
			}
		})
	}
}

func TestGetAgentConfigStatusReturnsApplyingWithoutReport(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{7}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}
	if _, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  otlp:\n"),
	); err != nil {
		t.Fatalf("store configuration: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents/"+agentUID.String()+"/config/status",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusOK,
		)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "{\"state\":\"applying\",\"desired\":{\"revision\":1,\"config_hash\":\"b78a3cd731efa5449c24e08abafbba0cd20d4f2eb9612573365b1b1f38b26c53\"},\"reported\":null}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestGetAgentConfigStatusKeepsLastFailureAfterRollback(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	configStore := remoteconfig.NewStore(db)
	agentUID := agent.InstanceUID{8}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	firstRevision, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  nop:\n"),
	)
	if err != nil {
		t.Fatalf("store first configuration: %v", err)
	}
	secondRevision, err := configStore.Save(
		t.Context(),
		agentUID,
		[]byte("receivers:\n  invalid:\n"),
	)
	if err != nil {
		t.Fatalf("store second configuration: %v", err)
	}
	failedAt := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	if err := configStore.RecordStatus(t.Context(), agentUID, remoteconfig.StatusReport{
		ConfigHash:   secondRevision.ConfigHash[:],
		Status:       remoteconfig.ApplyStatusFailed,
		ErrorMessage: "unknown receiver type",
		ReportedAt:   failedAt,
	}); err != nil {
		t.Fatalf("record failed configuration: %v", err)
	}
	if _, err := configStore.Rollback(t.Context(), agentUID); err != nil {
		t.Fatalf("roll back configuration: %v", err)
	}
	if err := configStore.RecordStatus(t.Context(), agentUID, remoteconfig.StatusReport{
		ConfigHash: firstRevision.ConfigHash[:],
		Status:     remoteconfig.ApplyStatusApplied,
		ReportedAt: failedAt.Add(time.Minute),
	}); err != nil {
		t.Fatalf("record restored configuration: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents/"+agentUID.String()+"/config/status",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		LastFailure *struct {
			Revision     *int64                   `json:"revision"`
			ConfigHash   string                   `json:"config_hash"`
			Status       remoteconfig.ApplyStatus `json:"status"`
			ErrorMessage string                   `json:"error_message"`
			ReportedAt   time.Time                `json:"reported_at"`
		} `json:"last_failure"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LastFailure == nil {
		t.Fatal("last failure = nil, want failed second revision")
	}
	if body.LastFailure.Revision == nil ||
		*body.LastFailure.Revision != secondRevision.Number {
		t.Errorf(
			"last failure revision = %v, want %d",
			body.LastFailure.Revision,
			secondRevision.Number,
		)
	}
	if body.LastFailure.Status != remoteconfig.ApplyStatusFailed {
		t.Errorf(
			"last failure status = %q, want %q",
			body.LastFailure.Status,
			remoteconfig.ApplyStatusFailed,
		)
	}
	if body.LastFailure.ErrorMessage != "unknown receiver type" {
		t.Errorf(
			"last failure error message = %q, want %q",
			body.LastFailure.ErrorMessage,
			"unknown receiver type",
		)
	}
	if !body.LastFailure.ReportedAt.Equal(failedAt) {
		t.Errorf(
			"last failure reported at = %v, want %v",
			body.LastFailure.ReportedAt,
			failedAt,
		)
	}
}

func TestGetAgentConfigStatusReturnsNotFoundWithoutDesiredRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	agentStore := agent.NewStore(db)
	agentUID := agent.InstanceUID{8}
	if err := agentStore.Upsert(t.Context(), agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/api/v1/agents/"+agentUID.String()+"/config/status",
		nil,
	)
	response := httptest.NewRecorder()

	request.AddCookie(cookie)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusNotFound,
		)
	}
	if got, want := response.Body.String(), "Not Found\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
