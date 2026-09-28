package configuration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/agent"
	"github.com/RSTCK-Innovation/arveld/internal/configuration"
)

func TestBaseSpecificationJSONRoundTrip(t *testing.T) {
	// This is the durable Arveld contract, independent of Collector config types.
	const saved = `{
		"schema_version": 1,
		"base_version": 4,
		"instance_uid": "12000000-0000-0000-0000-0000000000ab"
	}`
	var specification configuration.Specification
	if err := json.Unmarshal([]byte(saved), &specification); err != nil {
		t.Fatal(err)
	}
	uid, err := agent.ParseInstanceUID("12000000-0000-0000-0000-0000000000ab")
	if err != nil {
		t.Fatal(err)
	}
	current := configuration.NewSpecification(uid)
	if !reflect.DeepEqual(specification, current) {
		t.Fatal("saved specification does not describe the current Arveld base")
	}
	encoded, err := json.Marshal(specification)
	if err != nil {
		t.Fatal(err)
	}
	var restored configuration.Specification
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, specification) {
		t.Fatal("JSON round-trip changed the Arveld specification")
	}

	// Controller environment values must not become Agent configuration values.
	t.Setenv("ARVELD_URL", "http://controller-only.invalid")
	t.Setenv("ARVELD_AGENT_TOKEN", "controller-only-secret")
	t.Setenv("ARVELD_HOST_ROOT", "/controller-only-root")
	content, err := configuration.Compile(restored)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/base-v4.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, want) {
		t.Fatalf("compiled configuration changed the established base:\n%s", content)
	}

	// Unknown references must fail instead of silently selecting another base.
	cases := []struct {
		name      string
		value     configuration.Specification
		wantError string
	}{
		{name: "schema", value: restored, wantError: "schema version"},
		{name: "base", value: restored, wantError: "base version"},
		{name: "identity", value: restored, wantError: "instance UID"},
		{name: "empty", wantError: "schema version"},
	}
	cases[0].value.SchemaVersion = 2
	cases[1].value.BaseVersion = 5
	cases[2].value.InstanceUID = "invalid-agent-id"
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := configuration.Compile(test.value)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Compile() error = %v, want %q", err, test.wantError)
			}
			if len(got) != 0 {
				t.Fatal("invalid specification produced a configuration")
			}
		})
	}
}
