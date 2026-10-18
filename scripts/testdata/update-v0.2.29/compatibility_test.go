package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Exercise unchanged v0.2.29 production Prepare and Identity, never Install.
func TestReleaseCompatibility(t *testing.T) {
	dist, version, revision := os.Getenv("AEGIS_COMPAT_DIST"), os.Getenv("AEGIS_COMPAT_VERSION"), os.Getenv("AEGIS_COMPAT_REVISION")
	if dist == "" || version == "" || revision == "" {
		t.Fatal("explicit release compatibility inputs required")
	}
	for _, target := range []struct{ os, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		asset := fmt.Sprintf("aegis_v%s_%s_%s.tar.gz", version, target.os, target.arch)
		archive, err := os.ReadFile(filepath.Join(dist, asset))
		if err != nil {
			t.Fatal(err)
		}
		binary, err := extractBinary(archive)
		if err != nil {
			t.Fatal(err)
		}
		if err = validateCandidate(binary, version, target.os, target.arch); err != nil {
			t.Fatalf("%s: %v", asset, err)
		}
		if err = validateCandidate(binary, "999.999.999", target.os, target.arch); err == nil {
			t.Fatal("wrong version accepted")
		}
		t.Logf("old validator accepted %s; wrong version rejected", asset)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			fmt.Fprintf(w, `{"tag_name":"v%s","html_url":"%s/releases/tag/v%s","published_at":"2026-10-17T00:00:00Z","draft":false,"prerelease":false}`, version, server.URL, version)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, filepath.Base(r.URL.Path)))
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "aegis")
	original := []byte("isolated old installation sentinel")
	if err := os.WriteFile(target, original, 0700); err != nil {
		t.Fatal(err)
	}
	u := New("0.2.29")
	u.APIURL = server.URL + "/latest"
	u.DownloadURL = server.URL + "/download"
	u.RepositoryURL = server.URL
	u.Executable = func() (string, error) { return target, nil }
	if version == "0.0.0" {
		u.CurrentVersion = "development"
	} // non-release CI fixture only
	p, err := u.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	identity, err := p.Identity()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(dist, fmt.Sprintf("aegis_v%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := extractBinary(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(b)
	if identity.Version != version || identity.Source != revision || identity.Digest != hex.EncodeToString(digest[:]) || identity.Executable != target {
		t.Fatalf("identity mismatch: %+v", identity)
	}
	if err = p.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(original) {
		t.Fatal("Prepare modified installation")
	}
	t.Logf("v0.2.29 Prepare + Identity accepted: version=%s source=%s digest=%s; installation unchanged", identity.Version, identity.Source, identity.Digest)
}
