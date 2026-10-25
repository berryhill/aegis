package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardFilePresenceAndEnvironmentPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"null.json", `{"principal":{"uid":"4242","user":"operator"},"api":{"console":{"action_auth_ttl":null}}}`, false},
		{"valid.json", `{"principal":{"uid":"4242","user":"operator"},"api":{"console":{"action_auth_ttl":"1h"}}}`, true},
		{"duplicate.yaml", "principal:\n  uid: '4242'\n  user: operator\napi:\n  console:\n    action_auth_ttl: 1h\n    action_auth_ttl: 6h\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "aegis.yaml")
	if err := os.WriteFile(path, []byte("principal:\n  uid: '4242'\n  user: operator\napi:\n  console:\n    action_auth_ttl: 1h\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_API_CONSOLE_ACTION_AUTH_TTL", "15m")
	cfg, err := Load(path, nil)
	if err != nil || cfg.API.Console.ActionAuthTTL != 15*time.Minute || cfg.API.Console.SessionTTL != 6*time.Hour {
		t.Fatalf("precedence not preserved: %v", err)
	}
}
