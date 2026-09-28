package main

import "testing"

func TestResetPasswordCommandDispatch(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		code      int
	}{
		{"unknown command", []string{"unknown-command"}, 2},
		{"missing stdin flag", []string{"reset-password"}, 2},
		{"config before command", []string{"--config", "unused.yml", "reset-password"}, 2},
		{"command help", []string{"reset-password", "--help"}, 0},
		{"config before command help", []string{"--config", "unused.yml", "reset-password", "--help"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if code := run(test.arguments); code != test.code {
				t.Fatalf("dispatch exit = %d, want %d", code, test.code)
			}
		})
	}
}
