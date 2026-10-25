package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardPolicyStrictDecodeAndIndependentBounds(t *testing.T) {
	for _, key := range []string{"session_ttl", "action_auth_ttl"} {
		for _, value := range []string{"6h", "1h", "15m", "0s", "-1s", "6h1ns", "forever", "6", "21600000000000", "true", "null", "[]", "999999999999999999999h"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "aegis.yaml")
				data := "principal:\n  uid: '4242'\n  user: operator\napi:\n  console:\n    " + key + ": " + value + "\n"
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err := Load(path, nil)
				valid := value == "6h" || value == "1h" || value == "15m"
				if (err == nil) != valid {
					t.Fatalf("valid=%t err=%v", valid, err)
				}
				if valid && cfg.Principal.AuthTTL != 15*time.Minute {
					t.Fatal("dashboard policy widened shared principal")
				}
			})
		}
	}
	cfg := Defaults()
	cfg.Principal.UID = "4242"
	cfg.Principal.User = "operator"
	for _, value := range []time.Duration{0, -1, 6*time.Hour + 1} {
		cfg.API.Console.ActionAuthTTL = value
		if cfg.Validate() == nil {
			t.Fatal("invalid typed dashboard action policy")
		}
	}
	for _, value := range []string{"", "6", "6h1ns", "-1s", "forever"} {
		t.Run("env/"+value, func(t *testing.T) {
			t.Setenv("AEGIS_API_CONSOLE_ACTION_AUTH_TTL", value)
			if _, err := Load("", nil); err == nil {
				t.Fatal("invalid environment duration admitted")
			}
		})
	}
}
