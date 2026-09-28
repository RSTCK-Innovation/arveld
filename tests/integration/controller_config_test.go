package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/app"
	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestLoginCookieSecurityConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		secure     bool
	}{
		{"default", "", true},
		{"local HTTP", "session_cookie_secure: false\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "arveld.yml")
			if err := os.WriteFile(path, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			config, err := app.LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if config.SessionCookieSecure != test.secure {
				t.Errorf("SessionCookieSecure = %t, want %t", config.SessionCookieSecure, test.secure)
			}
			config.DatabasePath = filepath.Join(t.TempDir(), "arveld.db")
			config.HTTPAddress = "127.0.0.1:0"
			db := testutil.OpenDatabase(t, config.DatabasePath)
			createAdministrator(t, db)
			url, _ := startController(t, controllerTestConfig{
				Config: config, prometheusURL: "http://127.0.0.1:1", alertmanagerURL: "http://127.0.0.1:1",
			})
			if cookie := loginController(t, url); cookie.Secure != test.secure {
				t.Errorf("cookie Secure = %t, want %t", cookie.Secure, test.secure)
			}
		})
	}
}
