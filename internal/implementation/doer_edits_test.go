package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func doerEditContract(t *testing.T) loop.DoerContract {
	t.Helper()
	return loop.DoerContract{Task: "write result", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 3}
}

func TestApplyDoerEditsWritesOnlyAllowedFile(t *testing.T) {
	c := doerEditContract(t)
	protected := filepath.Join(c.Workspace, "protected.txt")
	if err := os.WriteFile(protected, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: "result.txt", Content: []byte("hello")}}, func(_ context.Context, effect string) error { calls = append(calls, effect); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(c.Workspace, "result.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("result %q: %v", got, err)
	}
	if got, err := os.ReadFile(protected); err != nil || string(got) != "untouched" {
		t.Fatalf("protected %q: %v", got, err)
	}
	if len(calls) != 1 || calls[0] != "write:result.txt" {
		t.Fatalf("admissions: %v", calls)
	}
}

func TestApplyDoerEditsRejectsOutOfScopeWithoutWriting(t *testing.T) {
	c := doerEditContract(t)
	for _, name := range []string{"protected.txt", "../escape.txt", "/absolute.txt", "result.txt/../protected.txt"} {
		t.Run(name, func(t *testing.T) {
			err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: name, Content: []byte("bad")}}, func(context.Context, string) error { return nil })
			if err == nil {
				t.Fatal("accepted unauthorized path")
			}
			if _, err := os.Lstat(filepath.Join(c.Workspace, "result.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestApplyDoerEditsRejectsSymlinkFileAndParent(t *testing.T) {
	c := doerEditContract(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "protected.txt")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(c.Workspace, "result.txt")); err != nil {
		t.Fatal(err)
	}
	admit := func(context.Context, string) error { return nil }
	if err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: "result.txt", Content: []byte("bad")}}, admit); err == nil {
		t.Fatal("accepted symlink target")
	}
	if err := os.Remove(filepath.Join(c.Workspace, "result.txt")); err != nil {
		t.Fatal(err)
	}
	c.WritableFiles = []string{"link/result.txt"}
	c.VerifyFile = "link/result.txt"
	if err := os.Symlink(outside, filepath.Join(c.Workspace, "link")); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: "link/result.txt", Content: []byte("bad")}}, admit); err == nil {
		t.Fatal("accepted symlink parent")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "untouched" {
		t.Fatalf("protected %q: %v", got, err)
	}
}

func TestApplyDoerEditsRejectsCanceledOrDeniedAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		context context.Context
		admit   Admit
	}{
		{name: "canceled", context: func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(), admit: func(context.Context, string) error { return nil }},
		{name: "denied", context: context.Background(), admit: func(context.Context, string) error { return &Halt{State: "denied"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := doerEditContract(t)
			err := ApplyDoerEdits(tc.context, c, []Edit{{Path: "result.txt", Content: []byte("bad")}}, tc.admit)
			if err == nil {
				t.Fatal("accepted")
			}
			if _, err := os.Lstat(filepath.Join(c.Workspace, "result.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected write: %v", err)
			}
		})
	}
}

func TestApplyDoerEditsRejectsDuplicateAndOversizeBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits []Edit
	}{
		{"duplicate", []Edit{{Path: "result.txt", Content: []byte("first")}, {Path: "result.txt", Content: []byte("second")}}},
		{"oversize", []Edit{{Path: "result.txt", Content: []byte(strings.Repeat("x", maxBytes+1))}}},
		{"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := doerEditContract(t)
			if err := ApplyDoerEdits(context.Background(), c, tc.edits, func(context.Context, string) error { return nil }); err == nil {
				t.Fatal("accepted invalid edits")
			}
			if _, err := os.Lstat(filepath.Join(c.Workspace, "result.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unexpected write: %v", err)
			}
		})
	}
}

func TestApplyDoerEditsFreshAdmissionPerWriteAndRegularReplacement(t *testing.T) {
	c := doerEditContract(t)
	c.WritableFiles = []string{"first.txt", "result.txt"}
	if err := os.WriteFile(filepath.Join(c.Workspace, "result.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: "first.txt", Content: []byte("first")}, {Path: "result.txt", Content: []byte("bad")}}, func(context.Context, string) error {
		calls++
		if calls == 2 {
			return &Halt{State: "denied"}
		}
		return nil
	})
	if err == nil || calls != 2 {
		t.Fatalf("admission error=%v calls=%d", err, calls)
	}
	if got, err := os.ReadFile(filepath.Join(c.Workspace, "result.txt")); err != nil || string(got) != "original" {
		t.Fatalf("second write occurred: %q %v", got, err)
	}
}

func TestApplyDoerEditsReplacesExistingRegularFileAndAllowsEmptyBytes(t *testing.T) {
	c := doerEditContract(t)
	c.WritableFiles = []string{"nested/result.txt"}
	c.VerifyFile = "nested/result.txt"
	if err := os.Mkdir(filepath.Join(c.Workspace, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(c.Workspace, "nested", "result.txt")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: c.VerifyFile, Content: []byte{}}}, func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || len(got) != 0 {
		t.Fatalf("replacement %q: %v", got, err)
	}
}

func TestApplyDoerEditsRejectsInternalParentSymlink(t *testing.T) {
	c := doerEditContract(t)
	c.WritableFiles = []string{"alias/result.txt"}
	c.VerifyFile = "alias/result.txt"
	if err := os.Mkdir(filepath.Join(c.Workspace, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(c.Workspace, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: c.VerifyFile, Content: []byte("bad")}}, func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("accepted in-root symlink parent")
	}
	if _, err := os.Lstat(filepath.Join(c.Workspace, "real", "result.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected write: %v", err)
	}
}
