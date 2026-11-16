//go:build linux

package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// This opt-in runs in the isolated networkless PM fixture. It tests the real
// public code command, parser and compiled production qualification without a test qualifier.
func TestInstalledNativeActivationParser(t *testing.T) {
	install := os.Getenv("AEGIS_TEST_INSTALLED_NATIVE_SOURCE")
	if install == "" {
		t.Skip("isolated PM-created generation and network namespace required")
	}
	d := core.RuntimeDescriptor{Version: "0.23.0", Installation: install}
	if err := qualifyProviderTransport(d); err != nil {
		t.Fatal("accepted exact installed record denied", err)
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("nc-%x", sha256.Sum256([]byte("aegis-native-codex-id-v1\x00chosen")))
	for _, kind := range []string{"valid", "ambiguous", "expired", "borrowed", "external", "unknown-status", "missing", "refresh"} {
		t.Run(kind, func(t *testing.T) {
			home := nativeFixture(t)
			token := syntheticProviderToken(now.Add(time.Hour))
			entry := map[string]any{"id": "chosen", "auth_type": "oauth", "source": "device_code", "access_token": token, "refresh_token": "SYNTHETIC_NEVER_REFRESH"}
			switch kind {
			case "expired":
				entry["access_token"] = syntheticProviderToken(now.Add(time.Minute))
			case "borrowed":
				entry["source"] = "global:device_code"
			case "external":
				entry["secret_source"] = "vault:SYNTHETIC"
			case "unknown-status":
				entry["last_status"] = "future-unknown"
			case "missing":
				entry["id"] = "replacement"
			}
			entries := []any{entry}
			if kind == "ambiguous" {
				entries = append(entries, map[string]any{"id": "other", "auth_type": "oauth", "source": "device_code", "access_token": syntheticProviderToken(now.Add(2 * time.Hour))})
			}
			nativeStore(t, home, "SYNTHETIC_SINGLETON_NOT_SELECTED", entries)
			before, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			if kind == "valid" {
				a := New(filepath.Join(install, ".hermes/bin/hermes"), slog.New(slog.NewTextHandler(io.Discard, nil)))
				c, e := a.InspectNativeCodexCandidates(context.Background(), home, now)
				if e != nil || len(c.Candidates) != 1 || !c.Candidates[0].Eligible || c.Candidates[0].CredentialID != id {
					t.Fatal("public qualified candidates", e)
				}
				m, e := a.PreviewNativeCodexLogin(context.Background(), home, now, id)
				if e != nil || m.AccountID != "synthetic-account" {
					t.Fatal("public qualified metadata", e)
				}
				p, e := a.ResolveNativeCodexLogin(context.Background(), "codex", home, "synthetic-account", now, id)
				if e != nil || p.value != token {
					t.Fatal("public qualified resolver", e)
				}
			}
			candidates, err := inspectNativeCodexCandidates(context.Background(), d, home, now)
			if err != nil {
				t.Fatal("actual native inspection", err)
			}
			if len(candidates.Candidates) != len(entries) {
				t.Fatal("candidate count")
			}
			if kind == "ambiguous" {
				if _, err := resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now); err == nil {
					t.Fatal("implicit pool choice")
				}
			}
			p, err := resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now, id)
			allowed := kind == "valid" || kind == "ambiguous" || kind == "refresh"
			if allowed {
				if err != nil || p.value != token {
					t.Fatal("exact native selection", err)
				}
			} else if err == nil {
				t.Fatal("unsafe native selection")
			}
			if _, err = resolveNativeCodexLogin(context.Background(), d, "codex", home, "other-account", now, id); err == nil {
				t.Fatal("account drift")
			}
			after, _ := os.ReadFile(filepath.Join(home, "auth.json"))
			if string(before) != string(after) {
				t.Fatal("native source mutated")
			}
			files, _ := os.ReadDir(home)
			if len(files) != 1 {
				t.Fatal("native source artifacts")
			}
			if kind == "refresh" {
				// External owner refresh of the SAME selected entry/account is allowed;
				// the parser itself must never refresh or select a replacement.
				entry["access_token"] = syntheticProviderToken(now.Add(2 * time.Hour))
				nativeStore(t, home, "SYNTHETIC_SINGLETON_NOT_SELECTED", entries)
				p, err = resolveNativeCodexLogin(context.Background(), d, "codex", home, "synthetic-account", now, id)
				if err != nil || p.value == token {
					t.Fatal("same-entry owner refresh", err)
				}
			}
		})
	}
	// Public module command remains complete; the child itself activates/leasing
	// from installation PM facts, never a copied profile's dependency tree.
	g, err := discoverGatewayCommand(context.Background(), d, t.TempDir())
	if err != nil || !g.modern {
		t.Fatal("public module activation", err)
	}
	code := "import json, sys; from pathlib import Path; from hermes_cli.runtime_state import leases_held; import openai; print(json.dumps({'leased': leases_held(Path(" + controllerCodeLiteral(os.Getenv("AEGIS_TEST_INSTALLED_NATIVE_GENERATION")) + ")), 'sdk': openai.__version__, 'no_sync': 'pm.install' not in sys.modules}))"
	probe, err := discoverRuntimeCommand(context.Background(), d, t.TempDir(), code, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, status, err := runBoundedRuntime(context.Background(), probe, "/nonexistent", t.TempDir(), 2048)
	var result struct {
		Leased bool
		SDK    string
		NoSync bool `json:"no_sync"`
	}
	if err != nil || status != 0 || json.Unmarshal(out, &result) != nil || !result.Leased || !result.NoSync || result.SDK == "" {
		t.Fatal("actual generation lease/SDK", status, err)
	}
	t.Log("actual public module/code activation, native parser, same-account refresh and held generation lease passed; accepted production qualification passed without a test qualifier")
}
