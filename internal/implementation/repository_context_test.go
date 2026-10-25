package implementation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestRepositoryContextCollection(t *testing.T) {
	for _, tc := range []struct {
		name, path, text string
		setup            string
		deny             bool
	}{
		{"exact", "main.go", "package main\n", "", false},
		{"trusted test", "main_test.go", "package main\n", "", false},
		{"escape", "../main.go", "", "", true},
		{"absolute", "/main.go", "", "", true},
		{"unclean", "a/../main.go", "", "", true},
		{"env", ".env.local", "SENSITIVE", "", true},
		{"git", ".git/config", "SENSITIVE", "", true},
		{"ssh", ".ssh/id_rsa", "SENSITIVE", "", true},
		{"credentials", "credentials.go", "SENSITIVE", "", true},
		{"key", "private.pem", "SENSITIVE", "", true},
		{"nul", "main.go", "a\x00b", "", true},
		{"utf8", "main.go", "\xff", "", true},
		{"binary", "main.go", "a\x01b", "", true},
		{"drift", "main.go", "package main\n", "drift", true},
		{"budget", "main.go", "package main\n", "budget", true},
		{"symlink", "main.go", "package main\n", "symlink", true},
		{"parent symlink", "sub/main.go", "package main\n", "parent", true},
		{"directory", "main.go", "", "directory", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if !tc.deny || tc.path == "main.go" || tc.setup == "parent" {
				target := filepath.Join(root, tc.path)
				if tc.setup == "directory" {
					if err := os.Mkdir(target, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, []byte(tc.text), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if tc.setup == "symlink" {
					if err := os.Rename(target, target+".real"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target+".real", target); err != nil {
						t.Fatal(err)
					}
				}
				if tc.setup == "parent" {
					if err := os.Rename(filepath.Join(root, "sub"), filepath.Join(root, "real")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("real", filepath.Join(root, "sub")); err != nil {
						t.Fatal(err)
					}
				}
			}
			role := "source"
			if strings.HasSuffix(tc.path, "_test.go") {
				role = "trusted_test"
			}
			m := &loop.RepositoryContext{TaskDigest: digest([]byte("task")), SourceDigest: digest([]byte("source state")), MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048, Files: []loop.RepositoryContextFile{{Path: tc.path, Bytes: int64(len(tc.text)), Digest: digest([]byte(tc.text)), Role: role}}}
			if tc.setup == "drift" {
				m.Files[0].Digest = digest([]byte("other"))
			}
			if tc.setup == "budget" {
				m.MaxFileBytes = 1
			}
			c := loop.VerifiedImplementation{Task: "task", Workspace: root, WritableFiles: []string{"main.go"}, RepositoryContext: m}
			got, err := CollectRepositoryContext(context.Background(), c, m.SourceDigest)
			if tc.deny {
				if err == nil || got != nil {
					t.Fatal("expected closed rejection")
				}
				if err.Error() != "repository context rejected" {
					t.Fatal("unsafe error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			id, err := m.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if got.Identity != id || got.Files[0].Text != tc.text || got.Files[0].Digest != digest([]byte(tc.text)) {
				t.Fatal("exact context mismatch")
			}
			if _, err := CollectRepositoryContext(context.Background(), c, digest([]byte("changed state"))); err == nil {
				t.Fatal("source binding accepted")
			}
		})
	}
}
