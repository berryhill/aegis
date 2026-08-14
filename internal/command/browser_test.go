package command

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenBrowserPassesTargetAsOneArgumentWithoutShellInterpretation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux xdg-open argv contract")
	}
	root := t.TempDir()
	capture := filepath.Join(root, "capture")
	opener := filepath.Join(root, "xdg-open")
	script := "#!/bin/sh\nprintf '%s\\n%s' \"$#\" \"$1\" > \"$BROWSER_CAPTURE\"\n"
	if err := os.WriteFile(opener, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("BROWSER_CAPTURE", capture)
	marker := filepath.Join(root, "should-not-run")
	target := "http://127.0.0.1:18443/console?value=$(touch " + marker + ")&other=a b"
	if err := openBrowser(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSuffix(string(captured), "\n"), "1\n"+target; got != want {
		t.Fatalf("browser argv capture = %q, want %q", got, want)
	}
	if _, err = os.Stat(filepath.Join(root, "should-not-run")); !os.IsNotExist(err) {
		t.Fatalf("target underwent shell interpretation: %v", err)
	}
}
