package remoteconfig

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestReportedStatusAndFailureAbsence(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
	store := NewStore(db)
	uid := agent.InstanceUID{1}
	if _, err := store.LatestStatus(t.Context(), uid); !errors.Is(err, ErrNoReportedStatus) {
		t.Fatalf("missing status = %v", err)
	}
	if _, err := store.LatestFailure(t.Context(), uid); !errors.Is(err, ErrNoReportedFailure) {
		t.Fatalf("missing failure = %v", err)
	}
	if _, err := store.LastWorking(t.Context(), uid); !errors.Is(err, ErrNoWorkingRevision) {
		t.Fatalf("missing working revision = %v", err)
	}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	for _, state := range []ApplyStatus{ApplyStatusApplying, ApplyStatusApplied} {
		if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: []byte("unknown configuration"), Status: state, ReportedAt: at}); err != nil {
			t.Fatal(err)
		}
		report, err := store.LatestStatus(t.Context(), uid)
		if err != nil || report.Revision != nil || report.Status != state || !report.ReportedAt.Equal(at) {
			t.Fatalf("unmatched report = %+v, %v", report, err)
		}
		if _, err := store.LatestFailure(t.Context(), uid); !errors.Is(err, ErrNoReportedFailure) {
			t.Fatalf("successful report created failure: %v", err)
		}
		if _, err := store.LastWorking(t.Context(), uid); !errors.Is(err, ErrNoWorkingRevision) {
			t.Fatalf("unknown hash created working revision: %v", err)
		}
	}
}

func TestLatestFailurePersistsAfterAgentRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.db")
	db := testutil.OpenDatabase(t, path)
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	first, err := store.Save(t.Context(), uid, []byte("receivers: {}"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(t.Context(), uid, []byte("receivers: {invalid: {}}"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	failed := StatusReport{ConfigHash: second.ConfigHash[:], Status: ApplyStatusFailed, ErrorMessage: "invalid receiver", ReportedAt: at}
	if err := store.RecordStatus(t.Context(), uid, failed); err != nil {
		t.Fatal(err)
	}
	if desired, err := store.Desired(t.Context(), uid); err != nil || desired.Number != second.Number {
		t.Fatalf("failure changed the desired target = %+v, %v", desired, err)
	}
	if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: first.ConfigHash[:], Status: ApplyStatusApplied, ReportedAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store = NewStore(testutil.OpenDatabase(t, path))
	report, err := store.LatestStatus(t.Context(), uid)
	if err != nil || report.Revision == nil || *report.Revision != first.Number || report.Status != ApplyStatusApplied {
		t.Fatalf("restored status = %+v, %v", report, err)
	}
	failure, err := store.LatestFailure(t.Context(), uid)
	failed.Revision = &second.Number
	if err != nil || !reflect.DeepEqual(failure, failed) {
		t.Fatalf("persisted failure = %+v, %v; want %+v", failure, err, failed)
	}
	if _, err := store.LatestFailure(t.Context(), agent.InstanceUID{2}); !errors.Is(err, ErrNoReportedFailure) {
		t.Fatalf("another agent inherited failure: %v", err)
	}
	// A later report for an unknown hash has no revision, but remains visible.
	unknown := StatusReport{ConfigHash: []byte("unknown hash"), Status: ApplyStatusFailed, ErrorMessage: "external configuration", ReportedAt: at.Add(2 * time.Minute)}
	if err := store.RecordStatus(t.Context(), uid, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rollback(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestFailure(t.Context(), uid)
	if err != nil || !reflect.DeepEqual(latest, unknown) {
		t.Fatalf("latest unknown failure = %+v, %v", latest, err)
	}
	// Repeating an older failure must not hide a newer failure with a different hash.
	if err := store.RecordStatus(t.Context(), uid, failed); err != nil {
		t.Fatal(err)
	}
	latest, err = store.LatestFailure(t.Context(), uid)
	if err != nil || !reflect.DeepEqual(latest, unknown) {
		t.Fatalf("latest failure ordering = %+v, %v", latest, err)
	}
	unknown.ErrorMessage = "updated failure"
	unknown.ReportedAt = at.Add(3 * time.Minute)
	if err := store.RecordStatus(t.Context(), uid, unknown); err != nil {
		t.Fatal(err)
	}
	latest, err = store.LatestFailure(t.Context(), uid)
	if err != nil || !reflect.DeepEqual(latest, unknown) {
		t.Fatalf("repeated failure metadata = %+v, %v", latest, err)
	}
}

func TestFailedStatusTransactionPreservesDesiredAndReportedState(t *testing.T) {
	for _, table := range []string{"agent_remote_config_statuses", "agent_remote_config_failures", "agent_config_assignments"} {
		t.Run(table, func(t *testing.T) {
			db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
			uid := agent.InstanceUID{1}
			if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
				t.Fatal(err)
			}
			store := NewStore(db)
			first, err := store.Save(t.Context(), uid, []byte("first"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := store.Save(t.Context(), uid, []byte("second"))
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
			if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: first.ConfigHash[:], Status: ApplyStatusApplied, ReportedAt: at}); err != nil {
				t.Fatal(err)
			}
			event := "INSERT"
			if table == "agent_config_assignments" {
				event = "UPDATE"
			}
			if _, err := db.ExecContext(t.Context(), "CREATE TRIGGER reject_status BEFORE "+event+" ON "+table+" BEGIN SELECT RAISE(ABORT, 'status write failure'); END;"); err != nil {
				t.Fatal(err)
			}
			if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: second.ConfigHash[:], Status: ApplyStatusFailed, ErrorMessage: "failed", ReportedAt: at.Add(time.Minute)}); err == nil {
				t.Fatal("status transaction ignored SQL failure")
			}
			if desired, err := store.Desired(t.Context(), uid); err != nil || desired.Number != second.Number {
				t.Fatalf("failed status changed desired: %+v, %v", desired, err)
			}
			if report, err := store.LatestStatus(t.Context(), uid); err != nil || report.Status != ApplyStatusApplied || !bytes.Equal(report.ConfigHash, first.ConfigHash[:]) {
				t.Fatalf("failed status changed report: %+v, %v", report, err)
			}
			if _, err := store.LatestFailure(t.Context(), uid); !errors.Is(err, ErrNoReportedFailure) {
				t.Fatalf("failed transaction persisted failure: %v", err)
			}
		})
	}
}

func TestAppliedStatusTransactionPreservesFailureAndWorkingRevision(t *testing.T) {
	for _, test := range []struct{ name, query string }{
		{"working confirmation", "CREATE TRIGGER reject_confirmation BEFORE UPDATE ON agent_remote_config_working BEGIN SELECT RAISE(ABORT, 'confirmation write failure'); END;"},
		{"target outcome", "CREATE TRIGGER reject_confirmation BEFORE UPDATE ON agent_config_assignments BEGIN SELECT RAISE(ABORT, 'confirmation write failure'); END;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
			uid := agent.InstanceUID{1}
			if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
				t.Fatal(err)
			}
			store := NewStore(db)
			for _, content := range []string{"working", "failed"} {
				revision, err := store.Save(t.Context(), uid, []byte(content))
				if err != nil {
					t.Fatal(err)
				}
				state := ApplyStatusApplied
				if content == "failed" {
					state = ApplyStatusFailed
				}
				if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: revision.ConfigHash[:], Status: state, ReportedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.Status(t.Context(), uid)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(t.Context(), test.query); err != nil {
				t.Fatal(err)
			}
			if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: before.Desired.ConfigHash[:], Status: ApplyStatusApplied, ReportedAt: time.Now()}); err == nil {
				t.Fatal("confirmation ignored SQL failure")
			}
			after, err := store.Status(t.Context(), uid)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("failed confirmation changed state: %+v, %v; before %+v", after, err, before)
			}
		})
	}
}

func TestStatusStoreErrors(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
	store := NewStore(db)
	uid := agent.InstanceUID{1}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.LatestStatus(ctx, uid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled status = %v", err)
	}
	if _, err := store.LatestFailure(ctx, uid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled failure = %v", err)
	}
	if _, err := store.LastWorking(ctx, uid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled working revision = %v", err)
	}
	if err := store.RecordStatus(ctx, uid, StatusReport{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled report = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LatestStatus(t.Context(), uid); err == nil {
		t.Fatal("closed status read succeeded")
	}
	if _, err := store.LatestFailure(t.Context(), uid); err == nil {
		t.Fatal("closed failure read succeeded")
	}
	if _, err := store.LastWorking(t.Context(), uid); err == nil {
		t.Fatal("closed working revision read succeeded")
	}
	if err := store.RecordStatus(t.Context(), uid, StatusReport{}); err == nil {
		t.Fatal("closed report succeeded")
	}
}
