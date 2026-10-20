package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestInstalledNativeCodexCandidatesSelection(t *testing.T) {
	install := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
	if install == "" {
		t.Skip("explicit installed source qualification required")
	}
	d := core.RuntimeDescriptor{Version: "0.21.3", Installation: install}
	if e := qualifyProviderTransport(d); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	selectedID := fmt.Sprintf("nc-%x", sha256.Sum256([]byte("aegis-native-codex-id-v1\x00chosen")))
	for _, kind := range []string{"valid", "same-account-distinct-grants", "unsupported-unselected", "missing", "duplicate", "changed", "type", "expired", "borrowed", "external", "dead", "unknown-status", "token-absent", "token-invalid", "id-invalid", "id-absent", "suppressed", "too-many"} {
		t.Run(kind, func(t *testing.T) {
			home := nativeFixture(t)
			token := syntheticProviderToken(now.Add(time.Hour))
			other := syntheticProviderToken(now.Add(2 * time.Hour))
			entry := map[string]any{"id": "chosen", "label": "LABEL_CANARY_MUST_NOT_ESCAPE", "auth_type": "oauth", "source": "device_code", "access_token": token, "refresh_token": "REFRESH_CANARY"}
			second := map[string]any{"id": "other", "auth_type": "oauth", "source": "manual:device_code", "access_token": other}
			switch kind {
			case "valid", "unsupported-unselected":
				// A second grant/account must never become an implicit selection.
				claims, _ := json.Marshal(map[string]any{"exp": now.Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "other-account"}})
				other = "header." + base64.RawURLEncoding.EncodeToString(claims) + ".synthetic"
				second["access_token"] = other
				if kind == "unsupported-unselected" {
					second["auth_type"] = "api_key"
				}
			case "missing", "changed":
				entry["id"] = "replacement"
			case "duplicate":
				second["id"] = "chosen"
			case "type":
				entry["auth_type"] = "api_key"
			case "expired":
				entry["access_token"] = syntheticProviderToken(now.Add(time.Minute))
			case "borrowed":
				entry["source"] = "global:device_code"
			case "external":
				entry["secret_source"] = "vault:CANARY"
			case "dead":
				entry["last_status"] = "dead"
			case "unknown-status":
				entry["last_status"] = "STATUS_CANARY"
			case "token-absent":
				entry["access_token"] = ""
			case "token-invalid":
				entry["access_token"] = "TOKEN_CANARY"
			case "id-invalid":
				entry["id"] = "id.CANARY"
			case "id-absent":
				delete(entry, "id")
			}
			entries := []any{entry, second}
			if kind == "too-many" {
				for len(entries) < 65 {
					entries = append(entries, second)
				}
			}
			nativeStore(t, home, "SINGLETON_CANARY", entries)
			if kind == "suppressed" {
				data, _ := os.ReadFile(filepath.Join(home, "auth.json"))
				var store map[string]any
				json.Unmarshal(data, &store)
				store["suppressed_sources"] = map[string]any{"openai-codex": []string{"device_code"}}
				data, _ = json.Marshal(store)
				os.WriteFile(filepath.Join(home, "auth.json"), data, 0600)
			}
			before, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			candidates, e := inspectNativeCodexCandidates(context.Background(), d, home, now)
			if kind == "suppressed" || kind == "too-many" {
				if e == nil {
					t.Fatal("unbounded/suppressed inspection admitted")
				}
				return
			}
			if e != nil || len(candidates.Candidates) != 2 {
				t.Fatal("inspection failed", e)
			}
			if candidates.SelectionPolicy != "explicit-or-unique-token" {
				t.Fatal("invented default policy")
			}
			encoded, _ := json.Marshal(candidates)
			for _, secret := range []string{token, other, "CANARY", "chosen", "replacement"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("candidate metadata leak")
				}
			}
			reason := map[string]string{"type": "unsupported_auth_type", "expired": "token_expired", "borrowed": "source_approval_required", "external": "source_approval_required", "dead": "unusable_status", "unknown-status": "unusable_status", "token-absent": "token_absent", "token-invalid": "token_invalid", "duplicate": "id_duplicate", "id-invalid": "id_missing_or_invalid", "id-absent": "id_missing_or_invalid"}[kind]
			if reason != "" && candidates.Candidates[0].Reason != reason {
				t.Fatalf("reason %s want %s", candidates.Candidates[0].Reason, reason)
			}
			if kind == "valid" || kind == "same-account-distinct-grants" {
				if candidates.DistinctEligibleGrants != 2 {
					t.Fatal("distinct grants conflated")
				}
				same := candidates.Candidates[0].AccountFingerprint == candidates.Candidates[1].AccountFingerprint
				if same != (kind == "same-account-distinct-grants") {
					t.Fatal("account fingerprint mismatch")
				}
			}
			_, defaultErr := resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now)
			if kind == "valid" || kind == "same-account-distinct-grants" || kind == "unsupported-unselected" {
				if defaultErr == nil {
					t.Fatal("implicit credential selected")
				}
			}
			p, exactErr := resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now, selectedID)
			allowed := kind == "valid" || kind == "same-account-distinct-grants" || kind == "unsupported-unselected"
			if kind == "missing" || kind == "changed" || kind == "duplicate" || kind == "id-invalid" || kind == "id-absent" {
				if NativeProviderFailureCode(exactErr) != "doer_provider_auth_native_credential_selection_invalid" {
					t.Fatal("exact ID failure stage lost", exactErr)
				}
			}
			if allowed {
				if exactErr != nil || p.value != token {
					t.Fatal("exact credential failed", exactErr)
				}
			} else if exactErr == nil {
				t.Fatal("unsafe selector admitted")
			}
			if _, e = resolveNativeCodexLogin(context.Background(), d, "codex", home, "different-account", now, selectedID); e == nil {
				t.Fatal("account pin bypassed")
			}
			if _, e = resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now, "TOKEN_CANARY"); e == nil {
				t.Fatal("invalid selector admitted")
			}
			after, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			if string(before) != string(after) {
				t.Fatal("source bytes changed")
			}
			names, _ := os.ReadDir(home)
			if len(names) != 1 {
				t.Fatal("source artifact created")
			}
		})
	}
}
