package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateChecksChecksumAndAtomicallyReplacesExecutable(t *testing.T) {
	archive := testArchive(t, []byte("new-aegis"))
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			fmt.Fprint(response, `{"tag_name":"v1.2.0"}`)
		case "/v1.2.0/SHA256SUMS":
			fmt.Fprintf(response, "%x  aegis_v1.2.0_linux_amd64.tar.gz\n", digest)
		case "/v1.2.0/aegis_v1.2.0_linux_amd64.tar.gz":
			_, _ = response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "aegis")
	if err := os.WriteFile(executable, []byte("old-aegis"), 0750); err != nil {
		t.Fatal(err)
	}
	u := testUpdater(server, executable, "1.1.0")
	result, err := u.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || !result.Updated || result.LatestVersion != "1.2.0" {
		t.Fatalf("unexpected result: %#v", result)
	}
	contents, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new-aegis" {
		t.Fatalf("executable was not replaced: %q", contents)
	}
	info, err := os.Stat(executable)
	if err != nil || info.Mode().Perm() != 0750 {
		t.Fatalf("executable mode was not preserved: %v %v", info, err)
	}
}

func TestUpdateCheckDoesNotReplaceExecutable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `{"tag_name":"v2.0.0"}`)
	}))
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "aegis")
	if err := os.WriteFile(executable, []byte("unchanged"), 0755); err != nil {
		t.Fatal(err)
	}
	result, err := testUpdater(server, executable, "1.0.0").Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || result.Updated {
		t.Fatalf("unexpected result: %#v", result)
	}
	contents, _ := os.ReadFile(executable)
	if string(contents) != "unchanged" {
		t.Fatal("check-only mode modified the executable")
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	archive := testArchive(t, []byte("untrusted"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			fmt.Fprint(response, `{"tag_name":"v1.1.0"}`)
		case "/v1.1.0/SHA256SUMS":
			fmt.Fprintf(response, "%064x  aegis_v1.1.0_linux_amd64.tar.gz\n", 0)
		default:
			_, _ = response.Write(archive)
		}
	}))
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "aegis")
	if err := os.WriteFile(executable, []byte("trusted"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := testUpdater(server, executable, "1.0.0").Run(context.Background(), false); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	contents, _ := os.ReadFile(executable)
	if string(contents) != "trusted" {
		t.Fatal("checksum failure modified the executable")
	}
}

func TestSemanticVersionComparison(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
		ok          bool
	}{
		{"1.2.3", "1.2.4", -1, true},
		{"2.0.0", "1.9.9", 1, true},
		{"1.2.3", "1.2.3", 0, true},
		{"dev", "1.0.0", 0, false},
		{"1.02.0", "1.2.0", 0, false},
	}
	for _, test := range tests {
		got, ok := compare(test.left, test.right)
		if got != test.want || ok != test.ok {
			t.Errorf("compare(%q, %q) = %d, %t; want %d, %t", test.left, test.right, got, ok, test.want, test.ok)
		}
	}
}

func testUpdater(server *httptest.Server, executable, current string) *Updater {
	return &Updater{
		CurrentVersion: current,
		APIURL:         server.URL + "/latest",
		DownloadURL:    server.URL,
		Client:         server.Client(),
		Executable:     func() (string, error) { return executable, nil },
		GOOS:           "linux",
		GOARCH:         "amd64",
	}
}

func testArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "aegis", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
