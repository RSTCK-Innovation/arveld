package auth

import (
	"strings"
	"testing"
)

func TestPasswordVerification(t *testing.T) {
	const password = "a long password for the local administrator"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == "" || hash == password {
		t.Fatal("HashPassword() must return a nonempty hash, not the password")
	}

	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !match {
		t.Error("VerifyPassword() = false for the original password, want true")
	}

	match, err = VerifyPassword("a different password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v for an incorrect password", err)
	}
	if match {
		t.Error("VerifyPassword() = true for an incorrect password, want false")
	}

	secondHash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("second HashPassword() error = %v", err)
	}
	if secondHash == hash {
		t.Error("HashPassword() reused the same hash for the same password")
	}

	match, err = VerifyPassword(password, "not-an-encoded-hash")
	if err == nil {
		t.Error("VerifyPassword() error = nil for an unreadable hash, want an error")
	}
	if match {
		t.Error("VerifyPassword() = true for an unreadable hash, want false")
	}
}

func TestValidatePassword(t *testing.T) {
	for _, test := range []struct {
		name, password string
		valid          bool
	}{
		{"empty", "", false},
		{"short ASCII", strings.Repeat("a", 14), false},
		{"minimum ASCII", strings.Repeat("a", 15), true},
		{"maximum ASCII", strings.Repeat("a", 128), true},
		{"long ASCII", strings.Repeat("a", 129), false},
		{"short Unicode", strings.Repeat("🔑", 14), false},
		{"minimum Unicode", strings.Repeat("🔑", 15), true},
		{"maximum Unicode", strings.Repeat("🔑", 128), true},
		{"long Unicode", strings.Repeat("🔑", 129), false},
		{"invalid UTF-8", strings.Repeat("a", 15) + "\xff", false},
		{"whitespace preserved", " 1234567890123 ", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidatePassword(test.password); (err == nil) != test.valid {
				t.Fatalf("ValidatePassword() = %v, want valid %t", err, test.valid)
			}
		})
	}
}
