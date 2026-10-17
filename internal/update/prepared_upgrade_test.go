package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var candidateFixtures sync.Map

// Build real Aegis executables; arbitrary strings cannot prove provenance.
func candidateFixture(t *testing.T, version string) []byte {
	t.Helper()
	if data, ok := candidateFixtures.Load(version); ok {
		return data.([]byte)
	}
	path := filepath.Join(t.TempDir(), "candidate")
	flags := "-s -w -X github.com/berryhill/aegis/internal/buildinfo.Version=" + version + " -X github.com/berryhill/aegis/internal/buildinfo.SourceRevision=0123456789abcdef0123456789abcdef01234567"
	if version == "missing-source" {
		flags = "-s -w -X github.com/berryhill/aegis/internal/buildinfo.Version=1.2.0"
	}
	cmd := exec.Command("go", "build", "-buildvcs=false", "-ldflags="+flags, "-o", path, "../../cmd/aegis")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	candidateFixtures.Store(version, data)
	return data
}

func sliceCandidate(t *testing.T, failure string) (*Updater, string) {
	t.Helper()
	binary := candidateFixture(t, "1.2.0")
	if failure == "executable" {
		binary = []byte("not an executable")
	}
	if failure == "version" {
		binary = candidateFixture(t, "1.3.0")
	}
	if failure == "source" {
		binary = candidateFixture(t, "missing-source")
	}
	archive := testArchive(t, binary)
	if failure == "archive" {
		archive = []byte("invalid gzip")
	}
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			if failure == "release" {
				fmt.Fprint(w, `{}`)
				return
			}
			fmt.Fprint(w, testReleaseJSON(r, "v1.2.0", false, false))
		case "/v1.2.0/SHA256SUMS":
			if failure == "checksum" {
				fmt.Fprintf(w, "%064x  aegis_v1.2.0_linux_amd64.tar.gz\n", 0)
				return
			}
			fmt.Fprintf(w, "%x  aegis_v1.2.0_linux_amd64.tar.gz\n", digest)
		default:
			if failure == "download" {
				http.Error(w, "unavailable", 503)
				return
			}
			w.Write(archive)
		}
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "aegis")
	if err := os.WriteFile(path, []byte("old-aegis"), 0750); err != nil {
		t.Fatal(err)
	}
	return testUpdater(server, path, "1.1.0"), path
}

func TestUpgradePreparationBeforeBoundary(t *testing.T) {
	for _, failure := range []string{"release", "checksum", "archive", "download", "executable", "version", "source"} {
		t.Run(failure, func(t *testing.T) {
			u, path := sliceCandidate(t, failure)
			authenticated, stopped := false, false
			_, err := RunUpgrade(context.Background(), u, func(context.Context) (func(context.Context) error, error) {
				authenticated = true
				return func(context.Context) error { stopped = true; return nil }, nil
			})
			contents, _ := os.ReadFile(path)
			if err == nil || authenticated || stopped || string(contents) != "old-aegis" {
				t.Fatalf("err=%v authenticated=%v stopped=%v unchanged=%v", err, authenticated, stopped, string(contents) == "old-aegis")
			}
		})
	}
}
