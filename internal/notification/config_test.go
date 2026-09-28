package notification_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/notification"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestAlertmanagerConfigMapsStoredChannelsAndRules(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	store := notification.NewStore(db)
	empty, err := store.AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != "route:\n    receiver: arveld\nreceivers:\n    - name: arveld\n" {
		t.Fatalf("unexpected empty configuration: %s", empty)
	}
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO agents (instance_uid) VALUES (zeroblob(16));
 INSERT INTO monitors (id,name,protocol,agent_instance_uid,endpoint,interval_seconds,timeout_seconds)
 VALUES ('monitor','Homepage','tcp',zeroblob(16),'example.com:443',30,5);
 INSERT INTO alert_rules (id,monitor_id,condition,for_seconds,severity) VALUES
 ('rule','monitor','failed',60,'warning'), ('rule-two','monitor','no_data',60,'warning');
 INSERT INTO notification_channels (id,name,type,config) VALUES
 ('zulu','Operations','webhook','{"url":"https://hooks.example.com/zulu?token=test-only&label=%22ops%22"}'),
 ('alpha','Operations','webhook','{"url":"https://hooks.example.com/alpha?token=test-only&label=%22ops%22"}');
 INSERT INTO alert_rule_notifications (rule_id,notification_id) VALUES ('rule','zulu'),('rule','alpha'),('rule-two','alpha');
 `); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/alertmanager.yml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.AlertmanagerConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("configuration:\n%s\nwant:\n%s", got, want)
	}
}
