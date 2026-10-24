package implementation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestContextExecutorCheckerExcludesUnlistedInputs(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"private.go", "package wrong\n"},
		{"private_test.go", "package synthetic\nimport \"testing\"\nfunc TestPrivate(t *testing.T) { t.Fatal(\"UNLISTED_EXECUTED\") }\n"},
		{"private.go", "package synthetic\nfunc init() { panic(\"UNLISTED_EXECUTED\") }\n"},
	} {
		t.Run(tc.name+tc.text[:7], func(t *testing.T) {
			e, c := addContextFixture(t)
			if err := os.WriteFile(filepath.Join(c.Workspace, tc.name), []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
				return []Edit{{Path: "add.go", Content: []byte(addRight)}}, nil
			})
			r, err := e.Run(context.Background(), "isolated-check", c)
			if err != nil || r.State != "succeeded" {
				t.Fatalf("declared candidate rejected: %+v %v", r, err)
			}
			out, err := e.loadBlob(r.Passes[0].OutputDigest)
			if err != nil || bytes.Contains(out, []byte("UNLISTED_EXECUTED")) || !bytes.Contains(out, []byte(`"Test":"TestAdd"`)) {
				t.Fatalf("wrong checker output: %s %v", out, err)
			}
			if err = e.Revalidate(r.RunID, c); err != nil {
				t.Fatal(err)
			}
		})
	}
}
