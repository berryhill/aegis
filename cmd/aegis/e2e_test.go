package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/buildinfo"
	"github.com/berryhill/aegis/internal/core"
)

func TestCLIEndToEndHermetic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX process-group script")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	hermes := filepath.Join(root, "hermes-fixture")
	fixture := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo 'Hermes Agent v0.18.2'
  echo 'Install directory: /isolated/hermes-fixture'
  exit 0
fi
if [ "${TEST_PROVIDER_KEY:-}" != "e2e-fixture-secret" ]; then
  exit 41
fi
if [ -n "${OPENAI_API_KEY:-}" ]; then
  exit 42
fi
sleep 60 &
wait
`
	if err := os.WriteFile(hermes, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid := current.Uid
	if _, err = strconv.ParseUint(uid, 10, 32); err != nil {
		t.Skipf("local OS identity does not expose a numeric UID: %q", uid)
	}

	configPath := filepath.Join(root, "aegis.yaml")
	configData := fmt.Sprintf(`state_dir: %s
runtime_default: hermes
hermes_executable: %s
principal:
  id: principal-1
  name: Principal Operator
  uid: %q
  user: %q
  auth_ttl: 5m
api:
  listen: 127.0.0.1:0
  token: test-transport-token
  read_timeout: 5s
  write_timeout: 5s
  shutdown_timeout: 5s
  max_body_bytes: 1048576
retention:
  design_homes: false
  session_homes: false
audit:
  checkpoint_dir: %s
credentials:
  references: {}
  provider_auth:
    test: {type: environment, source_env: AEGIS_E2E_PROVIDER_KEY, target_env: TEST_PROVIDER_KEY}
`, filepath.Join(root, "state"), hermes, uid, current.Username, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatal(err)
	}

	charter := core.Charter{
		SchemaVersion: core.SchemaVersion,
		AgentID:       "example-agent",
		Name:          "Example Agent",
		Revision:      1,
		Runtime: core.RuntimeConstraint{
			Adapter:           "hermes",
			Runtime:           "hermes-agent",
			VersionConstraint: ">=0.18.0,<0.19.0",
			Target:            "aegis-owned-ephemeral",
		},
		Stanzas: []core.TrustStanza{{
			ID:      "principal",
			Name:    "Principal",
			Enabled: true,
			Authentication: core.AuthenticationPolicy{
				Methods: []string{"local-os"},
				Selectors: []core.IdentitySelector{{
					Kinds:        []string{"human"},
					SubjectIDs:   []string{"local-uid:" + uid},
					PrincipalIDs: []string{"principal-1"},
					Issuers:      []string{"local-os"},
					Environments: []string{"local"},
				}},
				RequireFresh:  true,
				MaxAuthAgeSec: 300,
			},
			Grant:           core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}},
			Scopes:          core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}},
			Session:         core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true},
			Approval:        core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true},
			InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes:          core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"},
		}},
		CreatedBy: "principal-1",
		CreatedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	charterData, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	charterPath := filepath.Join(root, "charter.json")
	if err = os.WriteFile(charterPath, charterData, 0600); err != nil {
		t.Fatal(err)
	}

	run := func(arguments ...string) map[string]any {
		t.Helper()
		args := append([]string{"--config", configPath}, arguments...)
		command := exec.Command(binary, args...)
		command.Env = append(os.Environ(), "HOME="+filepath.Join(root, "home"), "AEGIS_E2E_PROVIDER_KEY=e2e-fixture-secret", "OPENAI_API_KEY=ambient-secret-must-not-pass")
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("aegis %s: %v\n%s", strings.Join(arguments, " "), runErr, output)
		}
		var result map[string]any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("decode aegis %s output: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return result
	}
	runDenied := func(arguments ...string) map[string]any {
		t.Helper()
		command := exec.Command(binary, append([]string{"--config", configPath}, arguments...)...)
		command.Env = append(os.Environ(), "HOME="+filepath.Join(root, "home"), "AEGIS_E2E_PROVIDER_KEY=e2e-fixture-secret")
		var stdout, stderr strings.Builder
		command.Stdout, command.Stderr = &stdout, &stderr
		if runErr := command.Run(); runErr == nil {
			t.Fatalf("aegis %s unexpectedly allowed", strings.Join(arguments, " "))
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
			t.Fatalf("decode denied aegis %s output: %v stdout=%s stderr=%s", strings.Join(arguments, " "), err, stdout.String(), stderr.String())
		}
		return result
	}
	id := func(value any, field string) string {
		t.Helper()
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s parent has type %T", field, value)
		}
		identifier, ok := object[field].(string)
		if !ok || identifier == "" {
			t.Fatalf("missing %s in %#v", field, object)
		}
		return identifier
	}

	versionOutput, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), buildinfo.Version) {
		t.Fatalf("CLI version is not %s: %v %s", buildinfo.Version, err, versionOutput)
	}
	runtimeResult := run("runtime")
	resolved := runtimeResult["resolved_runtime"].(map[string]any)
	if resolved["adapter_version"] != buildinfo.Version {
		t.Fatalf("CLI/adapter version mismatch: %#v", runtimeResult)
	}
	run("charter", "validate", charterPath)
	run("charter", "import", charterPath)
	effective := run("charter", "effective", "example-agent", "1", "--stanza", "principal")
	authority := effective["authority"].(map[string]any)
	if authority["stanza_id"] != "principal" || len(authority["tools"].([]any)) != 1 {
		t.Fatalf("effective authority was not exactly one stanza: %#v", effective)
	}
	denied := runDenied("charter", "explain", "example-agent", "1", "--stanza", "model-requested-admin")
	if denied["reason"] != "requested_stanza_unauthorized" || denied["allowed"] != false {
		t.Fatalf("CLI stanza flag broadened authority: %#v", denied)
	}
	denied = runDenied("charter", "explain", "example-agent", "1", "--stanza", "principal", "--environment", "production")
	if denied["reason"] != "invalid_environment" || denied["allowed"] != false {
		t.Fatalf("CLI environment flag broadened authority: %#v", denied)
	}
	review := run("plan", "preview", "example-agent", "--revision", "1")
	planID := id(review["plan"], "id")
	approval := run("approval", "request", planID, "--ttl", "1m")
	approvalID := id(approval, "id")
	run("approval", "approve", approvalID)
	run("provision", planID, approvalID)
	preview := run("session", "preview", "example-agent", "--revision", "1", "--stanza", "principal")
	mandateID := id(preview["mandate"], "id")
	session := run("session", "start", mandateID)
	sessionID := id(session, "id")
	defer func() {
		command := exec.Command(binary, "--config", configPath, "session", "revoke", sessionID, "--reason", "test_cleanup")
		command.Env = append(os.Environ(), "HOME="+filepath.Join(root, "home"), "AEGIS_E2E_PROVIDER_KEY=e2e-fixture-secret")
		_ = command.Run()
	}()
	run("session", "show", sessionID)
	run("session", "revoke", sessionID, "--reason", "e2e_complete")
	run("audit", "verify")
}
