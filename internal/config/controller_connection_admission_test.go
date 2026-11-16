//go:build linux

package config

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllerConnectionPublicationAdmission(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"connect", "replace", "reviewed-replace"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "owner.yaml")
			original := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			binding := ControllerCodexLogin{ProfileHome: filepath.Join(root, ".hermes"), CredentialID: "nc-" + strings.Repeat("a", 64), AccountID: "synthetic-account", ApprovedBy: "operator"}
			next := binding
			next.AccountID = "new-account"
			if mode != "connect" {
				if err := ConnectControllerCodex(path, original, binding); err != nil {
					t.Fatal(err)
				}
			}
			var review ControllerCodexConfigReview
			if mode == "reviewed-replace" {
				original = append(original, []byte(nativeTrustBlock)...)
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				_, review, err = ReadControllerCodexForReview(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			called := false
			deny := func() error {
				called = true
				staged, _ := filepath.Glob(filepath.Join(root, ".controller-codex-*"))
				if len(staged) != 1 {
					t.Fatal("admission not after staging", staged)
				}
				if mode == "connect" {
					if _, err := os.Stat(path + connectionSuffix); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("published before admission")
					}
				}
				if matches, _ := filepath.Glob(path + connectionSuffix + ".replacement-*"); len(matches) != 0 {
					t.Fatal("replacement published before admission")
				}
				return errors.New("fixture expired at publication")
			}
			switch mode {
			case "connect":
				err = ConnectControllerCodex(path, original, binding, deny)
			case "replace":
				err = ReplaceControllerCodex(path, original, binding, next, deny)
			case "reviewed-replace":
				err = ReplaceControllerCodexReviewed(path, original, binding, binding, review, deny)
			}
			if err == nil || !called {
				t.Fatal("publication admission not enforced", err, called)
			}
			if staged, _ := filepath.Glob(filepath.Join(root, ".controller-codex-*")); len(staged) != 0 {
				t.Fatal("staged inode leaked")
			}
			if mode == "reviewed-replace" {
				prior, actualReview, err := ReadControllerCodexForReview(path)
				if err != nil || prior == nil || *prior != binding || actualReview != review {
					t.Fatal("review history changed", err)
				}
				if _, err := ReadControllerCodexBinding(path); err == nil {
					t.Fatal("denied trust addition became executable")
				}
			} else {
				actual, err := ReadControllerCodexBinding(path)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "connect" {
					if actual != nil {
						t.Fatal("denied connect persisted")
					}
				} else if actual == nil || *actual != binding {
					t.Fatal("denied replacement persisted")
				}
			}
		})
	}
}
