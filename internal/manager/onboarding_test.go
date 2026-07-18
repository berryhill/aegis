package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
)

func TestExternalLocalOnboardingDiscoveryAndAtomicConfiguration(t *testing.T) {
	candidate := Candidates()[0]
	digest := strings.Repeat("a", 64)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/version":
			_, _ = writer.Write([]byte(`{"version":"0.32.0"}`))
		case "/api/tags":
			_, _ = fmt.Fprintf(writer, `{"models":[{"name":%q,"model":%q,"modified_at":"2026-07-18T12:24:35Z","size":3389983735,"digest":%q,"details":{"parent_model":"","format":"gguf","family":"qwen35","families":["qwen35"],"parameter_size":"4.7B","quantization_level":"Q4_K_M","context_length":262144,"embedding_length":2560},"capabilities":["vision","completion","tools","thinking"]}]}`, candidate.OllamaName, candidate.OllamaName, digest)
		default:
			t.Errorf("unexpected onboarding request %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	discovery, err := DiscoverInstalledCandidates(context.Background(), server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Installed) != 1 || discovery.Installed[0].Candidate.ID != candidate.ID || discovery.Installed[0].Digest != "sha256:"+digest || !discovery.NoDownload {
		t.Fatalf("discovery=%+v", discovery)
	}
	if requests.Load() != 2 {
		t.Fatalf("unexpected request count %d", requests.Load())
	}

	root := t.TempDir()
	configPath := onboardingConfig(t, root)
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewExternalModelConfiguration(configPath, filepath.Join(root, "state"), "", discovery.Installed[0])
	if err != nil {
		t.Fatal(err)
	}
	if preview.Endpoint != server.URL || !preview.NoDownload || !preview.NoCopy || preview.Model != candidate.OllamaName {
		t.Fatalf("preview=%+v", preview)
	}
	unchanged, _ := os.ReadFile(configPath)
	if string(unchanged) != string(original) {
		t.Fatal("preview changed configuration")
	}
	if err = ApplyModelConfiguration(preview); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config mode=%v err=%v", info.Mode().Perm(), err)
	}
	loaded, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manager.Inference.Mode != "external-local" || loaded.Manager.Inference.Endpoint != server.URL || loaded.Manager.Inference.Model != candidate.OllamaName || loaded.Manager.Inference.ModelDigest != "sha256:"+digest || !strings.HasPrefix(loaded.Manager.Inference.Certification, filepath.Join(root, "state")+string(os.PathSeparator)) {
		t.Fatalf("configured inference=%+v", loaded.Manager.Inference)
	}
}

func TestOnboardingRejectsUnapprovedNonLoopbackAndChangedConfig(t *testing.T) {
	if _, err := CandidateByID("unapproved"); err == nil {
		t.Fatal("unapproved candidate accepted")
	}
	if _, err := NewOllamaClient("http://192.0.2.1:11434", time.Second); err == nil {
		t.Fatal("non-loopback endpoint accepted")
	}
	root := t.TempDir()
	configPath := onboardingConfig(t, root)
	candidate := Candidates()[0]
	installed := InstalledCandidate{Candidate: candidate, Artifact: OllamaModel{Name: candidate.OllamaName, Model: candidate.OllamaName, Digest: strings.Repeat("b", 64)}, Digest: "sha256:" + strings.Repeat("b", 64), Endpoint: "http://127.0.0.1:11434"}
	if err := os.Chmod(configPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewExternalModelConfiguration(configPath, filepath.Join(root, "state"), "", installed); err == nil {
		t.Fatal("insecure configuration was accepted")
	}
	if err := os.Chmod(configPath, 0600); err != nil {
		t.Fatal(err)
	}
	malformed := []byte("manager: [unterminated\n")
	if err := os.WriteFile(configPath, malformed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewExternalModelConfiguration(configPath, filepath.Join(root, "state"), "", installed); err == nil {
		t.Fatal("malformed configuration was accepted")
	}
	if retained := mustRead(t, configPath); string(retained) != string(malformed) {
		t.Fatalf("malformed config changed: %q", retained)
	}
	configPath = onboardingConfig(t, root)
	if _, err := PreviewExternalModelConfiguration(configPath, filepath.Join(root, "state"), filepath.Join(root, "outside.json"), installed); err == nil {
		t.Fatal("certification destination outside state accepted")
	}
	preview, err := PreviewExternalModelConfiguration(configPath, filepath.Join(root, "state"), "", installed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, append([]byte("# changed after preview\n"), mustRead(t, configPath)...), 0600); err != nil {
		t.Fatal(err)
	}
	changed := mustRead(t, configPath)
	if err = ApplyModelConfiguration(preview); err == nil {
		t.Fatal("changed configuration was overwritten")
	}
	if string(mustRead(t, configPath)) != string(changed) {
		t.Fatal("failed apply altered changed configuration")
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".aegis-model-config-*.yaml")); len(matches) != 0 {
		t.Fatalf("temporary configuration retained: %v", matches)
	}
}

func onboardingConfig(t *testing.T, root string) string {
	t.Helper()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "aegis.yaml")
	data := fmt.Sprintf("# retained comment\nstate_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
