package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublishedReleaseMatrix exercises real, unmodified official archives supplied
// by release verification. It never executes a downloaded binary or opens state.
func TestPublishedReleaseMatrix(t *testing.T) {
	dir := os.Getenv("AEGIS_TEST_RELEASE_ASSETS")
	if dir == "" {
		t.Skip("official release asset directory not supplied")
	}
	version := os.Getenv("AEGIS_TEST_RELEASE_VERSION")
	revision := os.Getenv("AEGIS_TEST_RELEASE_REVISION")
	if _, ok := CompareVersions(version, version); !ok || len(revision) != 40 {
		t.Fatal("exact release version and source revision required")
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	checksums := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 || filepath.Base(fields[1]) != fields[1] {
			t.Fatal("invalid checksum record")
		}
		if _, exists := checksums[fields[1]]; exists {
			t.Fatal("duplicate checksum record")
		}
		checksums[fields[1]] = fields[0]
	}
	for _, platform := range []struct{ os, arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		t.Run(platform.os+"_"+platform.arch, func(t *testing.T) {
			name := "aegis_v" + version + "_" + platform.os + "_" + platform.arch + ".tar.gz"
			archive, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(archive)
			if hex.EncodeToString(sum[:]) != checksums[name] {
				t.Fatal("archive checksum mismatch")
			}
			binary, err := extractBinary(archive)
			if err != nil {
				t.Fatal(err)
			}
			claims, err := inspectCandidate(binary, version, platform.os, platform.arch)
			if err != nil || claims.Version != version || claims.Source != revision {
				t.Fatalf("candidate claims=%+v error=%v", claims, err)
			}
			wrongArch := "amd64"
			if platform.arch == wrongArch {
				wrongArch = "arm64"
			}
			if _, err := inspectCandidate(binary, version, platform.os, wrongArch); err == nil {
				t.Fatal("wrong platform accepted")
			}
		})
	}
}
