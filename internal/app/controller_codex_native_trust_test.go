//go:build linux

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

const additiveNativeTrust = "\nimplementation:\n  go_binary: /synthetic/go\n  authorized_contracts:\n    - sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58\n"

// The config/identity denials need no native executable. Positive cases use
// the production qualified read-only parser with an isolated synthetic store.
func TestControllerCodexNativeTrustReview(t *testing.T) {
	for _, kind := range []string{"drift", "duplicate", "unknown", "model", "principal", "old-block", "profile", "selector", "foreign-preview", "valid", "account", "account-after-review", "expiry", "foreign-apply", "review-drift", "history-tamper"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := testService(t)
			u, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			// Native source admission requires non-writable ancestry. Match the
			// owning-transport fixture instead of chmodding shared scratch roots.
			root, err := os.MkdirTemp(u.HomeDir, ".aegis-native-trust-test-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			owner := *u
			owner.HomeDir = root
			s.Current = func() (*user.User, error) { return &owner, nil }
			// Use the same isolated named-profile shape as the owning-transport
			// fixture. A synthetic shared root is not the operator's real root.
			home := filepath.Join(root, ".hermes", "profiles", "synthetic")
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			s.ConfigFile = filepath.Join(root, "owner.yaml")
			old := []byte("# exact original\nprincipal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			if kind == "old-block" {
				old = append(old, []byte("implementation: {}\n")...)
			}
			write := func(path string, b []byte) {
				t.Helper()
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(s.ConfigFile, old)
			s.Config, err = config.Load(s.ConfigFile, nil)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			s.Now = func() time.Time { return now }
			sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
			binding := config.ControllerCodexLogin{ProfileHome: home, AccountID: "synthetic-account", ApprovedBy: sub.PrincipalID}
			if err := config.ConnectControllerCodex(s.ConfigFile, old, binding); err != nil {
				t.Fatal(err)
			}
			base, err := os.ReadFile(s.ConfigFile + ".controller-codex.json")
			if err != nil {
				t.Fatal(err)
			}
			updated := append(append([]byte{}, old...), []byte(additiveNativeTrust)...)
			switch kind {
			case "drift":
				updated = bytes.ReplaceAll(updated, []byte("exact original"), []byte("unrelated"))
			case "duplicate":
				updated = append(updated, []byte("  go_binary: /other/go\n")...)
			case "unknown":
				updated = append(updated, []byte("  unexpected: true\n")...)
			case "model":
				updated = append(updated, []byte("hermes:\n  model: changed\n")...)
			case "principal":
				updated = bytes.ReplaceAll(updated, []byte("id: operator"), []byte("id: other"))
			}
			write(s.ConfigFile, updated)
			if _, err := s.ReadControllerCodexAs(ctx, sub); err != ErrConflict {
				t.Fatalf("show: %v", err)
			}
			if _, err := s.effectiveControllerCodexConfig(); err == nil {
				t.Fatal("inference admitted drift")
			}
			input := ControllerCodexPreviewInput{ProfileHome: home, Replace: true}
			switch kind {
			case "profile":
				input.ProfileHome = filepath.Join(root, ".hermes", "profiles", "other")
			case "selector":
				input.CredentialID = "nc-" + strings.Repeat("a", 64)
			case "foreign-preview":
				sub.PrincipalID = "other"
			}
			switch kind {
			case "drift", "duplicate", "unknown", "model", "principal", "old-block", "profile", "selector", "foreign-preview":
				if _, err := s.PreviewControllerCodexAs(ctx, sub, input); err == nil {
					t.Fatal("invalid preview accepted")
				}
				retained, err := os.ReadFile(s.ConfigFile + ".controller-codex.json")
				if err != nil || !bytes.Equal(retained, base) {
					t.Fatal("denial changed base")
				}
				return
			}
			install := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
			if install == "" {
				t.Skip("explicit qualified parser installation required; no native source accessed")
			}
			executable := filepath.Join(root, "qualified-hermes")
			write(executable, []byte("#!/bin/sh\nprintf 'Hermes Agent v0.21.3\\nInstall directory: "+install+"\\n'\n"))
			if err := os.Chmod(executable, 0700); err != nil {
				t.Fatal(err)
			}
			s.Hermes = hermes.New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
			save := func(account string) {
				claims, _ := json.Marshal(map[string]any{"exp": now.Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account}})
				store, _ := json.Marshal(map[string]any{"version": 1, "providers": map[string]any{}, "credential_pool": map[string]any{"openai-codex": []any{map[string]any{"id": "synthetic", "auth_type": "oauth", "source": "device_code", "access_token": "header." + base64.RawURLEncoding.EncodeToString(claims) + ".synthetic"}}}})
				write(filepath.Join(home, "auth.json"), store)
			}
			save("synthetic-account")
			if kind == "account" {
				save("different-account")
			}
			review, err := s.PreviewControllerCodexAs(ctx, sub, input)
			if kind == "account" {
				if err == nil {
					t.Fatal("changed account reviewed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ReadControllerCodexAs(ctx, sub); err != ErrConflict {
				t.Fatal("preview had runtime effect", err)
			}
			switch kind {
			case "account-after-review":
				save("different-account")
			case "expiry":
				now = now.Add(6 * time.Minute)
			case "foreign-apply":
				sub.PrincipalID = "other"
			case "review-drift":
				write(s.ConfigFile, append(append([]byte{}, updated...), []byte("# drift\n")...))
			case "history-tamper":
				write(s.ConfigFile+".controller-codex.json", append(append([]byte{}, base...), ' '))
			}
			out, err := s.ApplyControllerCodexAs(ctx, sub, ControllerCodexApplyInput{ReviewID: review.ReviewID})
			if kind != "valid" {
				if err == nil {
					t.Fatal("invalid apply accepted")
				}
				return
			}
			if err != nil || out.Status != "connected" || out.AccountID != binding.AccountID {
				t.Fatal("connect", out, err)
			}
			after, err := os.ReadFile(s.ConfigFile)
			if err != nil || !bytes.Equal(after, updated) {
				t.Fatal("config bytes changed")
			}
			retained, err := os.ReadFile(s.ConfigFile + ".controller-codex.json")
			if err != nil || !bytes.Equal(retained, base) {
				t.Fatal("base bytes changed")
			}
			if _, err := s.effectiveControllerCodexConfig(); err != nil {
				t.Fatal("runtime readback", err)
			}
			if _, err := s.ApplyControllerCodexAs(ctx, sub, ControllerCodexApplyInput{ReviewID: review.ReviewID}); err != ErrDenied {
				t.Fatal("replay", err)
			}
		})
	}
}
