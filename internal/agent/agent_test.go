package agent

import "testing"

func TestNewInstanceUIDCopiesValidValue(t *testing.T) {
	value := make([]byte, InstanceUIDSize)
	value[0] = 1

	uid, err := NewInstanceUID(value)
	if err != nil {
		t.Fatalf("NewInstanceUID() error = %v, want nil", err)
	}

	value[0] = 2
	if uid[0] != 1 {
		t.Errorf("UID first byte = %d, want 1", uid[0])
	}
}

func TestNewInstanceUIDRejectsInvalidLength(t *testing.T) {
	_, err := NewInstanceUID(make([]byte, InstanceUIDSize-1))
	if err == nil {
		t.Fatal("NewInstanceUID() error = nil, want non-nil")
	}
}

func TestParseInstanceUIDParsesCanonicalValue(t *testing.T) {
	got, err := ParseInstanceUID("12000000-0000-0000-0000-0000000000ab")
	if err != nil {
		t.Fatalf("ParseInstanceUID() error = %v, want nil", err)
	}

	want := InstanceUID{}
	want[0] = 0x12
	want[15] = 0xab
	if got != want {
		t.Errorf("ParseInstanceUID() = %s, want %s", got, want)
	}
}

func TestParseInstanceUIDRejectsValueWithoutSeparators(t *testing.T) {
	_, err := ParseInstanceUID("120000000000000000000000000000ab")
	if err == nil {
		t.Fatal("ParseInstanceUID() error = nil, want non-nil")
	}
}

func TestParseInstanceUIDRejectsMisplacedSeparators(t *testing.T) {
	_, err := ParseInstanceUID("1200000-00000-0000-0000-0000000000ab")
	if err == nil {
		t.Fatal("ParseInstanceUID() error = nil, want non-nil")
	}
}

func TestInstanceUIDString(t *testing.T) {
	uid := InstanceUID{}
	uid[0] = 0x12
	uid[15] = 0xab

	got := uid.String()
	want := "12000000-0000-0000-0000-0000000000ab"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
