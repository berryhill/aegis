//go:build linux

package config

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

const nativeTrustBlock = "\nimplementation:\n  go_binary: /synthetic/go\n  authorized_contracts:\n    - sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58\n"

func TestNativeTrustConfigReview(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"valid", "prior-replacement", "profile", "account", "selector", "approver", "drift", "duplicate", "unknown", "laya", "old-block", "wrong-commitment", "review-drift", "history-tamper", "replay"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "owner.yaml")
			old := []byte("# exact original\nprincipal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			if kind == "old-block" {
				old = append(old, []byte("implementation: {}\n")...)
			}
			write := func(b []byte) {
				t.Helper()
				if e := os.WriteFile(path, b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			write(old)
			binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes"), CredentialID: "nc-" + strings.Repeat("a", 64), AccountID: "synthetic-account", ApprovedBy: "operator"}
			if e := ConnectControllerCodex(path, old, binding); e != nil {
				t.Fatal(e)
			}
			if kind == "prior-replacement" {
				next := binding
				next.AccountID = "second-account"
				if e := ReplaceControllerCodex(path, old, binding, next); e != nil {
					t.Fatal(e)
				}
				binding = next
			}
			base, _ := os.ReadFile(path + connectionSuffix)
			updated := append(append([]byte{}, old...), []byte(nativeTrustBlock)...)
			switch kind {
			case "drift":
				updated = bytes.ReplaceAll(updated, []byte("# exact original"), []byte("# unrelated edit"))
			case "duplicate":
				updated = append(updated, []byte("  go_binary: /other/go\n")...)
			case "unknown":
				updated = append(updated, []byte("  unknown: true\n")...)
			case "laya":
				updated = append(updated, []byte("  laya_home: /synthetic\n")...)
			case "wrong-commitment":
				base = bytes.ReplaceAll(base, []byte(connectionDigest(old)), []byte(strings.Repeat("b", 64)))
				if e := os.WriteFile(path+connectionSuffix, base, 0600); e != nil {
					t.Fatal(e)
				}
			}
			write(updated)
			if _, e := ReadControllerCodexBinding(path); e == nil {
				t.Fatal("runtime admitted drift before review")
			}
			previous, review, e := ReadControllerCodexForReview(path)
			switch kind {
			case "drift", "duplicate", "unknown", "laya", "old-block", "wrong-commitment":
				if e == nil {
					t.Fatal("unproven delta admitted")
				}
				return
			}
			if e != nil || previous == nil || *previous != binding || !review.Additive {
				t.Fatal("review failed", e)
			}
			if _, e := ReadControllerCodexBinding(path); e == nil {
				t.Fatal("preview changed runtime authority")
			}
			next := binding
			switch kind {
			case "profile":
				next.ProfileHome = filepath.Join(u.HomeDir, ".hermes", "profiles", "other")
			case "account":
				next.AccountID = "different-account"
			case "selector":
				next.CredentialID = "nc-" + strings.Repeat("c", 64)
			case "approver":
				next.ApprovedBy = "other"
			case "review-drift":
				write(append(append([]byte{}, updated...), []byte("# after review\n")...))
			case "history-tamper":
				if e := os.WriteFile(path+connectionSuffix, append(base, ' '), 0600); e != nil {
					t.Fatal(e)
				}
			}
			e = ReplaceControllerCodexReviewed(path, updated, binding, next, review)
			switch kind {
			case "profile", "account", "selector", "approver", "review-drift", "history-tamper":
				if e == nil {
					t.Fatal("changed review applied")
				}
				return
			}
			if e != nil {
				t.Fatal("reviewed transition failed", e)
			}
			actual, e := ReadControllerCodexBinding(path)
			if e != nil || actual == nil || *actual != binding {
				t.Fatal("readback failed", e)
			}
			after, _ := os.ReadFile(path)
			retained, _ := os.ReadFile(path + connectionSuffix)
			if !bytes.Equal(after, updated) || !bytes.Equal(retained, base) {
				t.Fatal("immutable bytes changed")
			}
			if ReplaceControllerCodexReviewed(path, updated, binding, binding, review) == nil {
				t.Fatal("replayed transition accepted")
			}
			// Ordinary replacement after a config transition retains its existing meaning.
			next = binding
			next.AccountID = "later-account"
			if e := ReplaceControllerCodex(path, updated, binding, next); e != nil {
				t.Fatal("ordinary replacement after transition", e)
			}
			if _, e := ReadControllerCodexBinding(path); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestNativeTrustExactReconstruction(t *testing.T) {
	old := []byte("principal:\n  id: operator\n")
	for _, suffix := range []string{nativeTrustBlock, "\n" + nativeTrustBlock} {
		if !nativeTrustAddition(append(append([]byte{}, old...), []byte(suffix)...), connectionDigest(old)) {
			t.Fatal("exact append not proven")
		}
	}
	for _, suffix := range []string{"\nimplementation: {go_binary: /go, authorized_contracts: [sha256:abc]}\n", nativeTrustBlock + "principal:\n  id: changed\n", strings.Replace(nativeTrustBlock, "go_binary:", "go_binary: &anchor", 1), nativeTrustBlock + "---\n{}\n"} {
		if nativeTrustAddition(append(append([]byte{}, old...), []byte(suffix)...), connectionDigest(old)) {
			t.Fatal("ambiguous append accepted")
		}
	}
}
