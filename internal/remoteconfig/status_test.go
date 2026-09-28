package remoteconfig

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
)

func TestStoreKeepsLatestReportedStatus(t *testing.T) {
	ctx := t.Context()
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))

	agentUID := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(ctx, agent.UpsertParams{
		InstanceUID: agentUID,
	}); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	store := NewStore(db)
	if err := store.RecordStatus(ctx, agentUID, StatusReport{
		ConfigHash: []byte{1},
		Status:     ApplyStatusApplying,
		ReportedAt: time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record applying status: %v", err)
	}

	want := StatusReport{
		ConfigHash:   []byte{2},
		Status:       ApplyStatusFailed,
		ErrorMessage: "invalid receiver",
		ReportedAt:   time.Date(2026, time.September, 2, 12, 1, 0, 0, time.UTC),
	}
	if err := store.RecordStatus(ctx, agentUID, want); err != nil {
		t.Fatalf("record failed status: %v", err)
	}

	got, err := store.LatestStatus(ctx, agentUID)
	if err != nil {
		t.Fatalf("LatestStatus() error = %v, want nil", err)
	}
	if !bytes.Equal(got.ConfigHash, want.ConfigHash) {
		t.Errorf("config hash = %x, want %x", got.ConfigHash, want.ConfigHash)
	}
	if got.Status != want.Status {
		t.Errorf("status = %q, want %q", got.Status, want.Status)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("error message = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
	if !got.ReportedAt.Equal(want.ReportedAt) {
		t.Errorf("reported at = %v, want %v", got.ReportedAt, want.ReportedAt)
	}
}

func TestStatusUsesOnlyTheDesiredConfigurationReport(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if _, err := store.Status(t.Context(), uid); !errors.Is(err, ErrNoDesiredRevision) {
		t.Fatalf("missing desired configuration = %v", err)
	}
	desired, err := store.Save(t.Context(), uid, []byte("receivers: {}"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.Status(t.Context(), uid); err != nil || got.State != ApplyStatusApplying || got.Reported != nil || got.LastFailure != nil || got.LastWorking != nil {
		t.Fatalf("unreported configuration = %+v, %v", got, err)
	}
	for index, test := range []struct {
		name           string
		hash           []byte
		reported, want ApplyStatus
	}{
		{"matching applying", desired.ConfigHash[:], ApplyStatusApplying, ApplyStatusApplying},
		{"matching applied", desired.ConfigHash[:], ApplyStatusApplied, ApplyStatusApplied},
		{"matching failed", desired.ConfigHash[:], ApplyStatusFailed, ApplyStatusFailed},
		{"matching applying after failure", desired.ConfigHash[:], ApplyStatusApplying, ApplyStatusFailed},
		{"different applied", []byte("unknown hash"), ApplyStatusApplied, ApplyStatusFailed},
		{"different failed", []byte("unknown hash"), ApplyStatusFailed, ApplyStatusFailed},
		{"matching applied clears failure", desired.ConfigHash[:], ApplyStatusApplied, ApplyStatusApplied},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: test.hash, Status: test.reported, ReportedAt: time.Unix(int64(index), 0)}); err != nil {
				t.Fatal(err)
			}
			got, err := store.Status(t.Context(), uid)
			if err != nil || got.State != test.want || got.Desired.Number != desired.Number || got.Reported == nil || got.Reported.Status != test.reported {
				t.Fatalf("configuration status = %+v, %v", got, err)
			}
		})
	}
	got, err := store.Status(t.Context(), uid)
	if err != nil || got.LastFailure == nil || !bytes.Equal(got.LastFailure.ConfigHash, []byte("unknown hash")) {
		t.Fatalf("last failure = %+v, %v", got.LastFailure, err)
	}
}

func TestSelectingDifferentTargetReleasesFailureWithoutInventingWorkingRevision(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "status.db"))
	uid := agent.InstanceUID{1}
	if err := agent.NewStore(db).Upsert(t.Context(), agent.UpsertParams{InstanceUID: uid}); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	content := []byte("rejected")
	first, err := store.Save(t.Context(), uid, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordStatus(t.Context(), uid, StatusReport{ConfigHash: first.ConfigHash[:], Status: ApplyStatusFailed, ReportedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Saving the same bytes is idempotent, not an explicit retry command.
	if _, err := store.Save(t.Context(), uid, content); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Status(t.Context(), uid); err != nil || got.State != ApplyStatusFailed || got.LastWorking != nil {
		t.Fatalf("rejected target = %+v, %v", got, err)
	}
	second, err := store.Save(t.Context(), uid, []byte("corrected"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.Status(t.Context(), uid); err != nil || got.Desired.Number != second.Number || got.State != ApplyStatusApplying || got.LastWorking != nil {
		t.Fatalf("new target = %+v, %v", got, err)
	}
	confirmedAt := time.Now().UTC().Truncate(time.Millisecond)
	for _, report := range []StatusReport{
		{ConfigHash: second.ConfigHash[:], Status: ApplyStatusApplied, ReportedAt: confirmedAt},
		{ConfigHash: []byte("unknown"), Status: ApplyStatusApplied, ReportedAt: confirmedAt.Add(time.Second)},
		{ConfigHash: first.ConfigHash[:], Status: ApplyStatusApplying, ReportedAt: confirmedAt.Add(2 * time.Second)},
		{ConfigHash: first.ConfigHash[:], Status: ApplyStatusFailed, ReportedAt: confirmedAt.Add(3 * time.Second)},
	} {
		if err := store.RecordStatus(t.Context(), uid, report); err != nil {
			t.Fatal(err)
		}
		working, err := store.LastWorking(t.Context(), uid)
		if err != nil || working.Revision == nil || *working.Revision != second.Number || !bytes.Equal(working.ConfigHash, second.ConfigHash[:]) || !working.ReportedAt.Equal(confirmedAt) {
			t.Fatalf("last confirmed working = %+v, %v", working, err)
		}
	}
}
