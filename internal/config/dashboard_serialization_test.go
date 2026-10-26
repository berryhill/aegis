package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDashboardClosedConsoleJSON(t *testing.T) {
	for name, object := range map[string]string{
		"unknown":           `{"session_ttl":"1h","unexpected":true}`,
		"duplicate-session": `{"session_ttl":"1h","session_ttl":"6h"}`,
		"duplicate-action":  `{"action_auth_ttl":"1h","action_auth_ttl":"6h"}`,
		"null":              `null`, "array": `[]`, "scalar": `true`,
	} {
		t.Run(name, func(t *testing.T) {
			data := `{"principal":{"uid":"4242","user":"operator"},"api":{"console":` + object + `}}`
			t.Run("strict-decoder", func(t *testing.T) {
				cfg := Defaults()
				decoder := json.NewDecoder(strings.NewReader(data))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&cfg); err == nil {
					t.Fatal("accepted malformed Console object")
				}
			})
			t.Run("real-loader", func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "owner.json")
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(path, nil); err == nil {
					t.Fatal("accepted malformed Console object")
				}
			})
		})
	}
}

func TestDashboardConfigSerializedRoundTrip(t *testing.T) {
	for _, ttl := range []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour} {
		cfg := Defaults()
		cfg.Principal.UID = "4242"
		cfg.Principal.User = "operator"
		cfg.API.Console.SessionTTL = ttl
		cfg.API.Console.ActionAuthTTL = ttl
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"session_ttl":"`+ttl.String()+`"`) {
			t.Fatal("producer did not emit explicit duration units")
		}
		var decoded Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "owner.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.API.Console != cfg.API.Console || decoded.API.Console != cfg.API.Console || loaded.Principal.AuthTTL != 15*time.Minute {
			t.Fatal("roundtrip changed policy")
		}
	}
}

func TestDashboardLegacyJSONNanoseconds(t *testing.T) {
	for _, value := range []string{`900000000000`, `21600000000000`, `1`, `0`, `-1`, `21600000000001`, `1.5`, `null`, `true`, `"21600000000000"`} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.json")
			data := `{"principal":{"uid":"4242","user":"operator"},"api":{"console":{"session_ttl":` + value + `,"action_auth_ttl":` + value + `}}}`
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path, nil)
			valid := value == `900000000000` || value == `21600000000000` || value == `1`
			if (err == nil) != valid {
				t.Fatalf("valid=%v err=%v", valid, err)
			}
			var decoded Config
			decodeErr := json.Unmarshal([]byte(data), &decoded)
			if (decodeErr == nil) != valid {
				t.Fatalf("typed JSON valid=%v err=%v", valid, decodeErr)
			}
			if valid && loaded.API.Console.SessionTTL != decoded.API.Console.SessionTTL {
				t.Fatal("legacy units disagree")
			}
			if value == `1` && loaded.API.Console.SessionTTL != time.Nanosecond {
				t.Fatal("numeric value widened to larger units")
			}
		})
	}
}
