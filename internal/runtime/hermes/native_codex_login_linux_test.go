package hermes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func nativeFixture(t *testing.T) string {
	t.Helper()
	// Avoid /tmp's writable ancestry: this fixture owns only a new private path.
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(current.HomeDir, ".aegis-native-login-test-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	home := filepath.Join(root, ".hermes", "profiles", "synthetic")
	if err = os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	return home
}

func nativeStore(t *testing.T, home, token string, entries []any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": 1, "providers": map[string]any{"openai-codex": map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"access_token": token, "refresh_token": "REFRESH_CANARY_MUST_NOT_ESCAPE"}}}, "credential_pool": map[string]any{"openai-codex": entries}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, "auth.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCodexSourceGuards(t *testing.T) {
	st := syscall.Stat_t{Mode: syscall.S_IFREG | 0600, Uid: 1000, Nlink: 1, Size: 1}
	if !nativeLoginStatAllowed(st, 1000) || nativeLoginStatAllowed(st, 1001) {
		t.Fatal("source ownership predicate")
	}

	for _, kind := range []string{"missing", "profile-symlink", "file-symlink", "mode", "hardlink", "directory", "oversize", "unsafe-ancestor"} {
		t.Run(kind, func(t *testing.T) {
			home := nativeFixture(t)
			file := filepath.Join(home, "auth.json")
			nativeStore(t, home, "SYNTHETIC", nil)
			switch kind {
			case "missing":
				os.Remove(file)
			case "profile-symlink":
				moved := home + "-real"
				os.Rename(home, moved)
				os.Symlink(moved, home)
			case "file-symlink":
				os.Rename(file, file+"-real")
				os.Symlink(file+"-real", file)
			case "mode":
				os.Chmod(file, 0644)
			case "hardlink":
				os.Link(file, file+"-alias")
			case "directory":
				os.Remove(file)
				os.Mkdir(file, 0700)
			case "oversize":
				os.WriteFile(file, make([]byte, (1<<20)+1), 0600)
			case "unsafe-ancestor":
				os.Chmod(filepath.Dir(home), 0777)
			}
			got, err := openNativeLogin(home)
			if got != nil {
				got.Close()
				t.Fatal("unsafe source opened")
			}
			if err == nil || strings.Contains(err.Error(), "SYNTHETIC") {
				t.Fatal("unsafe source not sanitized")
			}
		})
	}
	for _, home := range []string{"relative", "/home/user/.hermes", "/home/user/.hermes/profiles/x/../y"} {
		if f, e := openNativeLogin(home); e == nil {
			f.Close()
			t.Fatal("non-exact profile admitted")
		}
	}
}

// Actual installed native parser, synthetic stores only. No production login is
// opened, and no fake resolver/client can satisfy this test.
func TestInstalledNativeCodexLoginSynthetic(t *testing.T) {
	root := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
	if root == "" {
		t.Skip("explicit installed source qualification required")
	}
	d := core.RuntimeDescriptor{Version: "0.21.3", Installation: root}
	if err := qualifyProviderTransport(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, kind := range []string{"valid", "alias", "pool-only", "pool-only-alias", "pool-only-no-tokens", "pool-only-expired", "pool-only-wrong-account", "pool-only-dead", "pool-only-borrowed", "pool-only-mixed", "pool-only-distinct", "stale-singleton", "legacy-mode", "api-key-pool", "expired", "wrong-account", "independent-pool", "missing-singleton", "malformed", "missing-token", "missing-local-pool", "dead-pool", "borrowed-pool", "external-custody", "suppressed"} {
		t.Run(kind, func(t *testing.T) {
			home := nativeFixture(t)
			token := syntheticProviderToken(now.Add(time.Hour))
			account := "synthetic-account"
			if kind == "expired" || kind == "pool-only-expired" {
				token = syntheticProviderToken(now.Add(time.Minute))
			}
			if kind == "wrong-account" || kind == "pool-only-wrong-account" {
				account = "not-the-account"
			}
			entries := []any{map[string]any{"auth_type": "oauth", "source": "device_code", "access_token": token}}
			if kind == "missing-local-pool" {
				entries = nil
			}
			if kind == "dead-pool" || kind == "pool-only-dead" {
				entries[0].(map[string]any)["last_status"] = "dead"
			}
			if kind == "api-key-pool" {
				entries[0].(map[string]any)["auth_type"] = "api_key"
			}
			if kind == "borrowed-pool" || kind == "pool-only-borrowed" {
				entries[0].(map[string]any)["source"] = "global:device_code"
			}
			if kind == "alias" || kind == "pool-only-alias" {
				entries = append(entries, map[string]any{"auth_type": "oauth", "source": "device_code", "access_token": token})
			}
			if kind == "independent-pool" || kind == "pool-only-distinct" {
				claims, _ := json.Marshal(map[string]any{"exp": now.Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "distinct-account"}})
				other := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".synthetic"
				if _, e := NewProviderAccessToken("codex", other, now); e != nil {
					t.Fatal("distinct fixture must be usable", e)
				}
				entries = append(entries, map[string]any{"auth_type": "oauth", "source": "manual:device_code", "access_token": other})
			}
			if kind == "pool-only-mixed" {
				entries = append(entries, map[string]any{"auth_type": "api_key", "source": "device_code", "access_token": "API_KEY_CANARY"})
			}
			if kind == "missing-token" {
				token = ""
			}
			nativeStore(t, home, token, entries)
			if kind == "stale-singleton" {
				nativeStore(t, home, "STALE_SINGLETON_CANARY", entries)
			}
			if strings.HasPrefix(kind, "pool-only") || kind == "external-custody" || kind == "suppressed" {
				file := filepath.Join(home, "auth.json")
				data, _ := os.ReadFile(file)
				var store map[string]any
				if e := json.Unmarshal(data, &store); e != nil {
					t.Fatal(e)
				}
				if kind == "pool-only-no-tokens" {
					store["providers"] = map[string]any{"openai-codex": map[string]any{}}
				} else {
					store["providers"] = map[string]any{}
				}
				if kind == "external-custody" {
					entries[0].(map[string]any)["secret_source"] = "vault:unapproved"
					store["credential_pool"] = map[string]any{"openai-codex": entries}
				}
				if kind == "suppressed" {
					store["suppressed_sources"] = map[string]any{"openai-codex": []string{"device_code"}}
				}
				data, _ = json.Marshal(store)
				if e := os.WriteFile(file, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "legacy-mode" {
				file := filepath.Join(home, "auth.json")
				data, _ := os.ReadFile(file)
				var store map[string]any
				if e := json.Unmarshal(data, &store); e != nil {
					t.Fatal(e)
				}
				store["providers"].(map[string]any)["openai-codex"].(map[string]any)["auth_mode"] = "oauth"
				data, _ = json.Marshal(store)
				if e := os.WriteFile(file, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "malformed" {
				os.WriteFile(filepath.Join(home, "auth.json"), []byte("{INVALID_SECRET_CANARY"), 0600)
			}
			if kind == "missing-singleton" {
				os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"providers":{},"credential_pool":{}}`), 0600)
			}
			before, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			p, err := resolveNativeCodexLogin(context.Background(), d, "codex", home, account, now)
			after, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			if string(before) != string(after) {
				t.Fatal("native source mutated")
			}
			if kind == "valid" || kind == "alias" || kind == "legacy-mode" || kind == "pool-only" || kind == "pool-only-alias" || kind == "pool-only-no-tokens" || kind == "missing-token" || kind == "stale-singleton" {
				if err != nil {
					t.Fatal(err)
				}
				expected := entries[0].(map[string]any)["access_token"].(string)
				if p.value != expected {
					t.Fatal("native source token mismatch")
				}
				encoded, _ := json.Marshal(p)
				for _, out := range []string{fmt.Sprintf("%v %#v", p, p), string(encoded)} {
					if strings.Contains(out, expected) || strings.Contains(out, "REFRESH_CANARY") {
						t.Fatal("token formatting leaked")
					}
				}
				disposable := t.TempDir()
				if err = writeProviderAuthentication(disposable, p); err != nil {
					t.Fatal(err)
				}
				runtimeAuth, _ := os.ReadFile(filepath.Join(disposable, "auth.json"))
				if strings.Contains(string(runtimeAuth), "refresh_token") || strings.Contains(string(runtimeAuth), "REFRESH_CANARY") {
					t.Fatal("refresh material escaped")
				}
			} else {
				if err == nil {
					t.Fatal("unsafe native login admitted")
				}
				if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), token) && token != "" {
					t.Fatal("native failure leaked")
				}
				if (kind == "expired" || kind == "pool-only-expired") && !errors.Is(err, ErrProviderAuthExpired) {
					t.Fatal("expiry not preserved", err)
				}
				if (kind == "wrong-account" || kind == "pool-only-wrong-account") && !errors.Is(err, ErrProviderAuthUnauthorized) {
					t.Fatal("account pin not preserved", err)
				}
				stage := map[string]string{"independent-pool": "source_ambiguous", "pool-only-distinct": "source_ambiguous", "pool-only-mixed": "source_ambiguous", "pool-only-dead": "pool_unusable", "pool-only-borrowed": "source_approval_required", "borrowed-pool": "source_approval_required", "external-custody": "source_approval_required", "suppressed": "source_suppressed", "api-key-pool": "source_ambiguous", "missing-local-pool": "pool_invalid", "dead-pool": "pool_unusable"}[kind]
				if stage != "" && NativeProviderFailureCode(err) != "doer_provider_auth_native_"+stage {
					t.Fatal("native failure stage not preserved", kind, err)
				}
			}
			names, _ := os.ReadDir(home)
			if len(names) != 1 {
				t.Fatal("source artifacts created")
			}
		})
	}
}
