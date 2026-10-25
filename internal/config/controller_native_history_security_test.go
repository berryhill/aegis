//go:build linux

package config

import (
	"bytes"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeTrustSuccessorReviewSecurity(t *testing.T) {
	for _, kind := range []string{"model", "provider", "principal", "runtime", "credentials", "go-binary", "removed-trust", "broader-trust", "duplicate-yaml", "stale-config", "substituted-history", "stale-history"} {
		t.Run(kind, func(t *testing.T) {
			u, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "owner.yaml")
			old := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			write := func(path string, b []byte) {
				t.Helper()
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(path, old)
			binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes"), AccountID: "synthetic-account", ApprovedBy: "operator"}
			if err := ConnectControllerCodex(path, old, binding); err != nil {
				t.Fatal(err)
			}
			first := append(append([]byte{}, old...), []byte(nativeTrustBlock)...)
			write(path, first)
			_, initial, err := ReadControllerCodexForReview(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ReplaceControllerCodexReviewed(path, first, binding, binding, initial); err != nil {
				t.Fatal(err)
			}
			factPath := path + connectionSuffix + ".replacement-000001"
			fact, err := os.ReadFile(factPath)
			if err != nil {
				t.Fatal(err)
			}
			next := append(append([]byte{}, first...), []byte(nextNativeContract)...)
			drift := map[string]string{"model": "hermes:\n  model: changed\n", "provider": "manager:\n  provider: changed\n", "runtime": "hermes_executable: /other/hermes\n", "credentials": "credentials:\n  providers: {}\n", "broader-trust": "    - '*'\n", "duplicate-yaml": "  go_binary: /other/go\n"}
			if text, ok := drift[kind]; ok {
				next = append(next, []byte(text)...)
			}
			switch kind {
			case "principal":
				next = bytes.ReplaceAll(next, []byte("id: operator"), []byte("id: other"))
			case "go-binary":
				next = bytes.ReplaceAll(next, []byte("/synthetic/go"), []byte("/other/go"))
			case "removed-trust":
				next = bytes.ReplaceAll(next, []byte("    - sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58\n"), nil)
			}
			write(path, next)
			if _, err := ReadControllerCodexBinding(path); err == nil {
				t.Fatal("runtime admitted drift")
			}
			_, review, err := ReadControllerCodexForReview(path)
			if !strings.HasPrefix(kind, "stale-") && kind != "substituted-history" {
				if err == nil {
					t.Fatal("unrelated/broadened config reviewed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "stale-config":
				write(path, append(append([]byte{}, next...), []byte("# drift\n")...))
			case "substituted-history":
				var record controllerReplacementRecord
				if err := json.Unmarshal(fact, &record); err != nil {
					t.Fatal(err)
				}
				record.PreviousHistorySHA256 = strings.Repeat("a", 64)
				changed, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				write(factPath, changed)
			case "stale-history":
				if err := ReplaceControllerCodexReviewed(path, next, binding, binding, review); err != nil {
					t.Fatal(err)
				}
			}
			if err := ReplaceControllerCodexReviewed(path, next, binding, binding, review); err == nil {
				t.Fatal("stale/substituted review committed")
			}
			if kind != "stale-history" {
				if _, err := os.Stat(path + connectionSuffix + ".replacement-000002"); !os.IsNotExist(err) {
					t.Fatal("denial published replacement")
				}
			}
		})
	}
}
