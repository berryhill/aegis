package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestReleaseTrimpathCandidate(t *testing.T) {
	dir := t.TempDir()
	clone := filepath.Join(dir, "source")
	run := func(cwd string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(".", "git", "clone", "--quiet", "--no-tags", "../..", clone)
	revision := run(clone, "git", "rev-parse", "HEAD")
	run(clone, "git", "tag", "v0.99.99")
	path := filepath.Join(dir, "aegis")
	run(clone, "go", "build", "-p=1", "-trimpath", "-ldflags=-s -w -X github.com/berryhill/aegis/internal/buildinfo.Version=0.99.99 -X github.com/berryhill/aegis/internal/buildinfo.SourceRevision="+revision, "-o", path, "./cmd/aegis")
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	if info.Main.Version != "v0.99.99" {
		t.Fatalf("module version %q", info.Main.Version)
	}
	for _, s := range info.Settings {
		if s.Key == "-ldflags" {
			t.Fatalf("trimpath unexpectedly retained ldflags: %q", s.Value)
		}
	}
	if err := validateCandidate(binary, "0.99.99", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct{ version, goos, goarch string }{
		{"0.99.98", runtime.GOOS, runtime.GOARCH},
		{"0.99.99", "wrong-os", runtime.GOARCH},
		{"0.99.99", runtime.GOOS, "wrong-arch"},
	} {
		if err := validateCandidate(binary, invalid.version, invalid.goos, invalid.goarch); err == nil {
			t.Fatal("mismatched candidate accepted")
		}
	}
	foreign := bytes.ReplaceAll(binary, []byte("github.com/berryhill/aegis"), []byte("github.com/barryhill/aegis"))
	if err := validateCandidate(foreign, "0.99.99", runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("foreign module accepted")
	}

	archive := testArchive(t, binary)
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprint(w, testReleaseJSON(r, "v0.99.99", false, false))
		case "/v0.99.99/SHA256SUMS":
			fmt.Fprintf(w, "%x  aegis_v0.99.99_linux_amd64.tar.gz\n", digest)
		default:
			w.Write(archive)
		}
	}))
	defer server.Close()
	for _, current := range []bool{false, true} {
		target := filepath.Join(t.TempDir(), "aegis")
		old := []byte("old image")
		version := "0.1.0"
		if current {
			old = binary
			version = "0.99.99"
		}
		if err := os.WriteFile(target, old, 0750); err != nil {
			t.Fatal(err)
		}
		u := testUpdater(server, target, version)
		prepared, err := u.Prepare(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		id, err := prepared.Identity()
		if err != nil || id.Source != revision || id.Version != "0.99.99" {
			t.Fatalf("current=%v identity=%+v err=%v", current, id, err)
		}
		if err := prepared.Validate(context.Background()); err != nil {
			t.Fatal(err)
		}
		identityPath := prepared.candidate
		if current {
			identityPath = target
		}
		if err := os.WriteFile(identityPath, append(binary, 0), 0750); err != nil {
			t.Fatal(err)
		}
		if _, err := prepared.Identity(); err == nil {
			t.Fatal("unsealed identity accepted")
		}
		prepared.Close()
	}

}

// The optional path is an operator-provided, read-only published archive, not an installed executable.
func TestPublishedCandidateArchive(t *testing.T) {
	path := os.Getenv("AEGIS_TEST_PUBLISHED_ARCHIVE")
	if path == "" {
		t.Skip("published archive not supplied")
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCandidate(binary, "0.2.29", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "aegis")
	if err := os.WriteFile(path, binary, 0600); err != nil {
		t.Fatal(err)
	}
	p := &Prepared{target: path, targetDigest: sha256.Sum256(binary), result: Result{LatestVersion: "0.2.29"}}
	id, err := p.Identity()
	if err != nil || id.Source != "e9f68106c504088998d1988d9f7a6a82798a968e" {
		t.Fatalf("identity=%+v err=%v", id, err)
	}
}

func TestReleaseClaimsFailClosed(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	const prefix = "github.com/berryhill/aegis/internal/buildinfo."
	goodFlags := "-s -w -X " + prefix + "Version=1.2.0 -X " + prefix + "SourceRevision=" + revision
	tests := []struct {
		name   string
		mutate func(*debug.BuildInfo, map[string]string)
	}{
		{"wrong module version", func(i *debug.BuildInfo, s map[string]string) { i.Main.Version = "v1.3.0" }},
		{"development module", func(i *debug.BuildInfo, s map[string]string) { i.Main.Version = "(devel)" }},
		{"replacement module", func(i *debug.BuildInfo, s map[string]string) { i.Main.Replace = &debug.Module{Path: "other"} }},
		{"missing revision", func(i *debug.BuildInfo, s map[string]string) { delete(s, "vcs.revision") }},
		{"malformed revision", func(i *debug.BuildInfo, s map[string]string) { s["vcs.revision"] = "bad" }},
		{"uppercase revision", func(i *debug.BuildInfo, s map[string]string) { s["vcs.revision"] = strings.ToUpper(revision) }},
		{"dirty", func(i *debug.BuildInfo, s map[string]string) { s["vcs.modified"] = "true" }},
		{"missing clean flag", func(i *debug.BuildInfo, s map[string]string) { delete(s, "vcs.modified") }},
		{"not git", func(i *debug.BuildInfo, s map[string]string) { s["vcs"] = "hg" }},
		{"not trimpath", func(i *debug.BuildInfo, s map[string]string) { delete(s, "-trimpath") }},
		{"empty explicit flags", func(i *debug.BuildInfo, s map[string]string) { s["-ldflags"] = "" }},
		{"wrong explicit version", func(i *debug.BuildInfo, s map[string]string) {
			s["-ldflags"] = strings.Replace(goodFlags, "Version=1.2.0", "Version=1.3.0", 1)
		}},
		{"missing explicit source", func(i *debug.BuildInfo, s map[string]string) { s["-ldflags"] = "-X " + prefix + "Version=1.2.0" }},
		{"conflicting source", func(i *debug.BuildInfo, s map[string]string) {
			s["-ldflags"] = strings.Replace(goodFlags, revision, strings.Repeat("a", 40), 1)
		}},
		{"duplicate", func(i *debug.BuildInfo, s map[string]string) {
			s["-ldflags"] = goodFlags + " -X " + prefix + "Version=1.2.0"
		}},
		{"trailing X", func(i *debug.BuildInfo, s map[string]string) { s["-ldflags"] = goodFlags + " -X" }},
		{"quoted", func(i *debug.BuildInfo, s map[string]string) { s["-ldflags"] = goodFlags + " -X 'foo=bar'" }},
		{"equals X", func(i *debug.BuildInfo, s map[string]string) { s["-ldflags"] = goodFlags + " -X=foo=bar" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/berryhill/aegis", Version: "v1.2.0"}}
			settings := map[string]string{"-trimpath": "true", "vcs": "git", "vcs.revision": revision, "vcs.modified": "false"}
			tt.mutate(info, settings)
			if _, err := extractCandidateClaims(info, settings, "1.2.0"); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}
}
