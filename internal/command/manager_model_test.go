package command

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	managerdomain "github.com/berryhill/aegis/internal/manager"
)

func TestDegradedManagerHelpStatusAndAuditAreTruthful(t *testing.T) {
	configPath := managerTestConfig(t)
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("/help\n/status\nexit\n"), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", configPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, required := range []string{"Reason: manager_model_absent", "Credential authority: absent", "Model: absent", "Inference: degraded", "plain quit and exit also work", "Aegis manager stopped; cleanup complete"} {
		if !strings.Contains(text, required) {
			t.Fatalf("degraded output missing %q: %s", required, text)
		}
	}
	for _, prohibited := range []string{"/secret put", "/secret rotate", "/secret revoke", "manager_scanner_failed"} {
		if strings.Contains(text, prohibited) {
			t.Fatalf("degraded output falsely advertised or reported %q: %s", prohibited, text)
		}
	}
	var audit bytes.Buffer
	auditRoot := NewRoot(Dependencies{In: strings.NewReader(""), Out: &audit, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	auditRoot.SetArgs([]string{"--config", configPath, "audit", "list"})
	if err := auditRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit.String(), managerdomain.ReasonModelAbsent) || !strings.Contains(audit.String(), "manager_startup") || strings.Count(audit.String(), "manager_session_closed") != 1 {
		t.Fatalf("startup audit omitted exact reason: %s", audit.String())
	}
}

func TestManagerModelCommandsDiscoverDeclineAndConfigureWithoutDownload(t *testing.T) {
	candidate := managerdomain.Candidates()[0]
	digest := strings.Repeat("c", 64)
	var unexpected bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/version":
			_, _ = writer.Write([]byte(`{"version":"0.32.0"}`))
		case "/api/tags":
			_, _ = fmt.Fprintf(writer, `{"models":[{"name":%q,"model":%q,"digest":%q,"details":{"quantization_level":"Q4"}}]}`, candidate.OllamaName, candidate.OllamaName, digest)
		default:
			unexpected = true
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	configPath := managerTestConfig(t)
	if err := os.Chmod(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(configPath)

	run := func(input string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		root := NewRoot(Dependencies{In: strings.NewReader(input), Out: &out, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
		root.SetArgs(append([]string{"--config", configPath}, args...))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	candidateOutput := run("", "manager", "model", "candidates")
	if !strings.Contains(candidateOutput, candidate.ID) || !strings.Contains(candidateOutput, `"default": null`) {
		t.Fatalf("candidate output=%s", candidateOutput)
	}
	if repeated := run("", "manager", "model", "candidates"); repeated != candidateOutput {
		t.Fatalf("candidate output changed across repeated runs:\nfirst=%s\nsecond=%s", candidateOutput, repeated)
	}
	discovery := run("", "manager", "model", "discover", "--endpoint", server.URL)
	if !strings.Contains(discovery, "sha256:"+digest) || !strings.Contains(discovery, `"no_download": true`) {
		t.Fatalf("discovery output=%s", discovery)
	}
	declined := run("no\n", "manager", "model", "configure", candidate.ID, "--endpoint", server.URL)
	if !strings.Contains(declined, "declined; no writes") || !bytes.Equal(original, mustCommandRead(t, configPath)) {
		t.Fatalf("decline changed config or output=%s", declined)
	}
	configured := run("yes\n", "manager", "model", "configure", candidate.ID, "--endpoint", server.URL)
	if !strings.Contains(configured, `"activated": false`) || !strings.Contains(configured, `"downloaded": false`) || unexpected {
		t.Fatalf("configured output=%s unexpected_request=%v", configured, unexpected)
	}
	loaded, err := config.Load(configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manager.Inference.Endpoint != server.URL || loaded.Manager.Inference.ModelDigest != "sha256:"+digest || loaded.Manager.Inference.Mode != "external-local" {
		t.Fatalf("configured route=%+v", loaded.Manager.Inference)
	}
	degraded := run("exit\n")
	if !strings.Contains(degraded, managerdomain.ReasonAuthorityUnavailable) {
		t.Fatalf("configured model without authority had inexact readiness reason: %s", degraded)
	}
}

func mustCommandRead(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
