package evidence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedFileExactRawBytesRejectsWhitespace(t *testing.T) {
	for _, value := range []string{"hello", "hello\n", " hello", "hello ", "hello\r\n", "HELLO"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
			policy := SelectedFilePolicy{Version: SelectedFilePolicyV1, RelativePath: "result.txt", Mode: SelectedFileExactBytes, Text: "hello"}
			digest, err := policy.Digest()
			if err != nil {
				t.Fatal(err)
			}
			binding := SelectedFileBinding{AttemptID: "attempt", ActionID: "verify", RunID: "run", OwnerID: "owner", AuthorityContextID: "authority", AuthorityContextDigest: "sha256:authority"}
			result, err := VerifySelectedFile(context.Background(), root, policy, digest, binding)
			if err != nil {
				t.Fatal(err)
			}
			if (result.Outcome == Passed) != (value == "hello") {
				t.Fatalf("raw byte assertion accepted %q: %+v", value, result)
			}
			if result.Outcome == Passed && string(result.Content) != "hello" {
				t.Fatal("passed bytes not retained exactly")
			}
			if value == "hello\n" {
				policy.Mode = SelectedFileText
				digest, err = policy.Digest()
				if err != nil {
					t.Fatal(err)
				}
				legacy, err := VerifySelectedFile(context.Background(), root, policy, digest, binding)
				if err != nil || legacy.Outcome != Passed {
					t.Fatal("historical trimmed-text semantics changed", err)
				}
			}
		})
	}
}
