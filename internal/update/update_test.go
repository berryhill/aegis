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
	"strings"
	"testing"
)

func TestUpdateChecksChecksumAndAtomicallyReplacesExecutable(t *testing.T) {
	archive := testArchive(t, []byte("new-aegis"))
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			fmt.Fprint(response, testReleaseJSON(request, "v1.2.0", false, false))
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
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fmt.Fprint(response, testReleaseJSON(request, "v2.0.0", false, false))
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

func TestUpdateCheckExplainsMissingPublishedRelease(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	u := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "1.0.0")
	_, err := u.Run(context.Background(), true)
	if err == nil {
		t.Fatal("missing release was accepted")
	}
	want := "no published release is visible at " + server.URL + "/latest (HTTP 404 Not Found); publish a non-draft GitHub release before using self-update"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("missing-release error = %q; want it to contain %q", err, want)
	}
}

func TestUpdateRejectsChecksumMismatch(t *testing.T) {
	archive := testArchive(t, []byte("untrusted"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			fmt.Fprint(response, testReleaseJSON(request, "v1.1.0", false, false))
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

func TestPublishedStableReleaseDiscovery(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		latest    string
		available bool
	}{
		{name: "v0.1.3 remains current before v0.1.4 publication", current: "0.1.3", latest: "v0.1.3", available: false},
		{name: "v0.1.4 becomes available after publication", current: "0.1.3", latest: "v0.1.4", available: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				fmt.Fprint(response, testReleaseJSON(request, test.latest, false, false))
			}))
			defer server.Close()
			result, err := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), test.current).Run(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			if result.UpdateAvailable != test.available || result.Updated || result.LatestVersion != strings.TrimPrefix(test.latest, "v") {
				t.Fatalf("unexpected discovery result: %#v", result)
			}
		})
	}
}

func TestPublishedReleaseDiscoveryIsNotCached(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		tag := "v0.1.3"
		if requests > 1 {
			tag = "v0.1.4"
		}
		fmt.Fprint(response, testReleaseJSON(request, tag, false, false))
	}))
	defer server.Close()
	updater := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "0.1.3")
	first, err := updater.Run(context.Background(), true)
	if err != nil || first.UpdateAvailable || first.LatestVersion != "0.1.3" {
		t.Fatalf("first discovery = %#v, %v", first, err)
	}
	second, err := updater.Run(context.Background(), true)
	if err != nil || !second.UpdateAvailable || second.LatestVersion != "0.1.4" {
		t.Fatalf("second discovery = %#v, %v", second, err)
	}
}

func TestUpdateRejectsDraftAndPrereleaseMetadata(t *testing.T) {
	for _, test := range []struct {
		name       string
		draft      bool
		prerelease bool
	}{
		{name: "draft", draft: true},
		{name: "prerelease", prerelease: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				fmt.Fprint(response, testReleaseJSON(request, "v0.1.4", test.draft, test.prerelease))
			}))
			defer server.Close()
			_, err := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "0.1.3").Run(context.Background(), true)
			if err == nil || !strings.Contains(err.Error(), "draft or prerelease") {
				t.Fatalf("non-stable release error = %v", err)
			}
		})
	}
}

func TestUpdateRejectsWrongRepositoryIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `{"tag_name":"v0.1.4","html_url":"https://github.com/attacker/aegis/releases/tag/v0.1.4","published_at":"2026-07-18T00:00:00Z"}`)
	}))
	defer server.Close()
	_, err := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "0.1.3").Run(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "repository identity mismatch") {
		t.Fatalf("wrong-repository error = %v", err)
	}
}

func TestUpdateRejectsRedirectWithoutFollowing(t *testing.T) {
	contacted := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { contacted = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer server.Close()
	_, err := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "0.1.3").Run(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302 Found") {
		t.Fatalf("redirect error = %v", err)
	}
	if contacted {
		t.Fatal("updater followed a redirect")
	}
}

func TestUpdateRejectsMissingChecksumAndMalformedArchive(t *testing.T) {
	for _, test := range []struct {
		name      string
		checksums string
		archive   []byte
		want      string
	}{
		{name: "missing checksum", archive: []byte("unused"), want: "has no valid checksum"},
		{name: "malformed archive", checksums: "valid", archive: []byte("not a gzip archive"), want: "open release archive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest := sha256.Sum256(test.archive)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/latest":
					fmt.Fprint(response, testReleaseJSON(request, "v0.1.4", false, false))
				case "/v0.1.4/SHA256SUMS":
					if test.checksums == "valid" {
						fmt.Fprintf(response, "%x  aegis_v0.1.4_linux_amd64.tar.gz\n", digest)
					}
				case "/v0.1.4/aegis_v0.1.4_linux_amd64.tar.gz":
					_, _ = response.Write(test.archive)
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()
			executable := filepath.Join(t.TempDir(), "aegis")
			if err := os.WriteFile(executable, []byte("unchanged"), 0755); err != nil {
				t.Fatal(err)
			}
			_, err := testUpdater(server, executable, "0.1.3").Run(context.Background(), false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want %q", err, test.want)
			}
			contents, readErr := os.ReadFile(executable)
			if readErr != nil || string(contents) != "unchanged" {
				t.Fatalf("failed update modified executable: %q, %v", contents, readErr)
			}
		})
	}
}

func TestUpdateRejectsDowngrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fmt.Fprint(response, testReleaseJSON(request, "v0.1.3", false, false))
	}))
	defer server.Close()
	_, err := testUpdater(server, filepath.Join(t.TempDir(), "aegis"), "0.1.4").Run(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "refusing downgrade") {
		t.Fatalf("downgrade error = %v", err)
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
		RepositoryURL:  server.URL,
		Client:         server.Client(),
		Executable:     func() (string, error) { return executable, nil },
		GOOS:           "linux",
		GOARCH:         "amd64",
	}
}

func testReleaseJSON(request *http.Request, tag string, draft, prerelease bool) string {
	return fmt.Sprintf(`{"tag_name":%q,"html_url":%q,"published_at":"2026-07-18T00:00:00Z","draft":%t,"prerelease":%t}`,
		tag, "http://"+request.Host+"/releases/tag/"+tag, draft, prerelease)
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
