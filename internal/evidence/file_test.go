package evidence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectedPolicy() SelectedFilePolicy {
	return SelectedFilePolicy{Version: SelectedFilePolicyV1, RelativePath: "out/result.txt", Mode: SelectedFileText, Text: "ready"}
}
func selectedBinding() SelectedFileBinding {
	return SelectedFileBinding{AttemptID: "attempt", ActionID: "action", RunID: "run", OwnerID: "owner", AuthorityContextID: "auth", AuthorityContextDigest: "sha256:authority"}
}
func verifySelected(t *testing.T, root string, p SelectedFilePolicy) SelectedFileResult {
	t.Helper()
	pin, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r, err := VerifySelectedFile(context.Background(), root, p, pin, selectedBinding())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestSelectedFileTextAndPresence(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "out"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out/result.txt"), []byte("  ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := selectedPolicy()
	r := verifySelected(t, root, p)
	b := selectedBinding()
	if r.Outcome != Passed || string(r.Content) != "  ready\n" || r.ContentDigest != sha256Reference([]byte("  ready\n")) || r.PolicyDigest == "" || r.AttemptID != b.AttemptID || r.RunID != b.RunID || r.OwnerID != b.OwnerID || r.AuthorityContextDigest != b.AuthorityContextDigest {
		t.Fatalf("bad binding: %+v", r)
	}
	p.Mode = SelectedFilePresence
	p.Text = ""
	r = verifySelected(t, root, p)
	if r.Outcome != Passed {
		t.Fatalf("presence: %+v", r)
	}
	p.Mode = SelectedFileText
	p.Text = "wrong"
	r = verifySelected(t, root, p)
	if r.Outcome != Failed || r.FailureCategory != "text_mismatch" {
		t.Fatalf("text mismatch: %+v", r)
	}
}
func TestSelectedFileRejectsUnsafePathsAndPolicies(t *testing.T) {
	root := t.TempDir()
	p := selectedPolicy()
	for _, path := range []string{"", "/etc/passwd", "../outside", "out/../result.txt", "out//result.txt", "./out/result.txt", "out\\result.txt", "out/./result.txt"} {
		p.RelativePath = path
		if _, err := p.Digest(); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	p = selectedPolicy()
	p.Mode = SelectedFilePresence
	if _, err := p.Digest(); err == nil {
		t.Fatal("presence accepted text assertion")
	}
	p = selectedPolicy()
	p.Text = ""
	if _, err := p.Digest(); err != nil {
		t.Fatalf("empty expected text must be valid in text mode: %v", err)
	}
	p = selectedPolicy()
	p.Version = "unknown"
	if _, err := p.Digest(); err == nil {
		t.Fatal("accepted unknown version")
	}
	p = selectedPolicy()
	pin, _ := p.Digest()
	p.Text = "other"
	if _, err := VerifySelectedFile(context.Background(), root, p, pin, selectedBinding()); err == nil {
		t.Fatal("accepted post-pin mutation")
	}
}
func TestSelectedFileDeniesSymlinksAndNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("ready"), 0600)
	os.Symlink(outside, filepath.Join(root, "out"))
	p := selectedPolicy()
	r := verifySelected(t, root, p)
	if r.Outcome != Failed {
		t.Fatalf("directory symlink: %+v", r)
	}
	os.Remove(filepath.Join(root, "out"))
	os.Mkdir(filepath.Join(root, "out"), 0700)
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "out/result.txt"))
	r = verifySelected(t, root, p)
	if r.Outcome != Failed {
		t.Fatalf("file symlink: %+v", r)
	}
	os.Remove(filepath.Join(root, "out/result.txt"))
	os.Mkdir(filepath.Join(root, "out/result.txt"), 0700)
	r = verifySelected(t, root, p)
	if r.Outcome != Failed {
		t.Fatalf("directory as file: %+v", r)
	}
}
func TestSelectedFileRejectsInternalSymlinkAndInvalidUTF8(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "out"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "real"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "out", "result.txt")); err != nil {
		t.Fatal(err)
	}
	p := selectedPolicy()
	if r := verifySelected(t, root, p); r.Outcome != Failed {
		t.Fatalf("internal symlink: %+v", r)
	}
	if err := os.Remove(filepath.Join(root, "out", "result.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "result.txt"), []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if r := verifySelected(t, root, p); r.Outcome != Failed || r.FailureCategory != "invalid_utf8" {
		t.Fatalf("invalid utf8: %+v", r)
	}
	p.Mode = SelectedFilePresence
	p.Text = ""
	if r := verifySelected(t, root, p); r.Outcome != Passed {
		t.Fatalf("presence should not require UTF-8: %+v", r)
	}
}

func TestSelectedFileTextStripParity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte(" \n\t "), 0600); err != nil {
		t.Fatal(err)
	}
	p := selectedPolicy()
	p.RelativePath = "result.txt"
	p.Text = ""
	if r := verifySelected(t, root, p); r.Outcome != Passed {
		t.Fatalf("empty assertion: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte(" \tready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p.Text = " \n ready\t"
	if r := verifySelected(t, root, p); r.Outcome != Passed {
		t.Fatalf("stripped assertion: %+v", r)
	}
	p.Text = "other"
	if r := verifySelected(t, root, p); r.Outcome != Failed {
		t.Fatalf("mismatch: %+v", r)
	}
}

func TestSelectedFileStaticPinAndDynamicBinding(t *testing.T) {
	p := selectedPolicy()
	pin, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "out"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "result.txt"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	b := selectedBinding()
	first, err := VerifySelectedFile(context.Background(), root, p, pin, b)
	if err != nil || first.Outcome != Passed {
		t.Fatalf("first: %+v %v", first, err)
	}
	b.AttemptID = "second-attempt"
	b.RunID = "second-run"
	b.AuthorityContextID = "second-authority"
	second, err := VerifySelectedFile(context.Background(), root, p, pin, b)
	if err != nil || second.Outcome != Passed || second.PolicyDigest != pin || second.AttemptID != b.AttemptID || second.RunID != b.RunID || second.AuthorityContextID != b.AuthorityContextID || second.AttemptID == first.AttemptID {
		t.Fatalf("dynamic binding: %+v %v", second, err)
	}
	b.OwnerID = ""
	if _, err := VerifySelectedFile(context.Background(), root, p, pin, b); err == nil {
		t.Fatal("accepted missing owner")
	}
}

func TestSelectedFileMissingOversizeAndTamper(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "out"), 0700)
	p := selectedPolicy()
	r := verifySelected(t, root, p)
	if r.Outcome != Failed {
		t.Fatalf("missing: %+v", r)
	}
	path := filepath.Join(root, "out/result.txt")
	os.WriteFile(path, []byte(strings.Repeat("x", SelectedFileMaxBytes+1)), 0600)
	r = verifySelected(t, root, p)
	if r.Outcome != Failed || r.ContentDigest != "" {
		t.Fatalf("oversize: %+v", r)
	}
	os.WriteFile(path, []byte("ready"), 0600)
	r = verifySelected(t, root, p)
	if r.Outcome != Passed {
		t.Fatalf("initial: %+v", r)
	}
	os.WriteFile(path, []byte("changed"), 0600)
	after := verifySelected(t, root, p)
	if after.Outcome != Failed || after.ContentDigest == r.ContentDigest {
		t.Fatalf("tamper: %+v", after)
	}
}
