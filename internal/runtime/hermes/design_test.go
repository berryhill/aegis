package hermes

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesignProposalUsesGatewayAndCleansHome(t *testing.T) {
	root := t.TempDir()
	installation := filepath.Join(root, "install")
	if err := os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	hermesExecutable := filepath.Join(root, "hermes")
	hermesScript := "#!/bin/sh\necho 'Hermes Agent v0.18.2'\necho 'Install directory: " + installation + "'\n"
	if err := os.WriteFile(hermesExecutable, []byte(hermesScript), 0700); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(installation, "venv", "bin", "python")
	gatewayScript := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"design-1"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"<aegis-charter>{}"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"</aegis-charter>"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","payload":{}}}'
printf '%s' $$ > "$HERMES_HOME/gateway.pid"
while read rest; do :; done
`
	if err := os.WriteFile(python, []byte(gatewayScript), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := New(hermesExecutable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proposal, home, err := adapter.DesignProposal(ctx, filepath.Join(root, "state"), "make an agent", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposal != `{}` {
		t.Fatalf("proposal = %q", proposal)
	}
	if !strings.Contains(home, filepath.Join(root, "state", "runtime")) {
		t.Fatalf("design home escaped state root: %s", home)
	}
	if _, err = os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("design home retained: %v", err)
	}
}

func TestExtractCharterRejectsUnstructuredOutput(t *testing.T) {
	if _, err := extractCharter("```json\n{}\n```"); err == nil {
		t.Fatal("accepted output without protocol envelope")
	}
	proposal, err := extractCharter("noise<aegis-charter>{}</aegis-charter>noise")
	if err != nil || proposal != "{}" {
		t.Fatalf("proposal=%q err=%v", proposal, err)
	}
}

func TestDiscoveryDisablesVersionCheck(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "hermes")
	script := `#!/bin/sh
if [ "$HERMES_SKIP_VERSION_CHECK" != "1" ] || [ "$HERMES_ENABLE_PROJECT_PLUGINS" != "false" ]; then
  exit 9
fi
echo 'Hermes Agent v0.18.2'
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERMES_SKIP_VERSION_CHECK", "0")
	adapter := New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := adapter.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMinimalEnvDisablesPythonBytecode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-secret-must-not-pass")
	environment := minimalEnv(t.TempDir(), []Credential{{Reference: "provider:test", TargetEnv: "TEST_PROVIDER_KEY", Value: "selected-secret"}})
	found := false
	selected := false
	for _, value := range environment {
		if value == "PYTHONDONTWRITEBYTECODE=1" {
			found = true
		}
		if value == "TEST_PROVIDER_KEY=selected-secret" {
			selected = true
		}
		if strings.HasPrefix(value, "OPENAI_API_KEY=") {
			t.Fatal("ambient provider credential leaked into Hermes environment")
		}
	}
	if !found {
		t.Fatal("minimal Hermes environment permits writes to the installed Python tree")
	}
	if !selected {
		t.Fatal("explicitly resolved credential was not injected")
	}
}

func TestBrokerBridgeConfigExposesOnlyAegisToolset(t *testing.T) {
	home := t.TempDir()
	executable := filepath.Join(home, "aegis")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeBrokerBridgeConfig(home, BrokerBridge{Enabled: true, Executable: executable, Timeout: 7 * time.Second}); err != nil {
		t.Fatal(err)
	}
	configuration, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(configuration)
	for _, required := range []string{"mcp_servers:", "aegis:", "credential-bridge", "github_get_repository", "resources: false", "prompts: false"} {
		if !strings.Contains(text, required) {
			t.Fatalf("bridge config missing %q:\n%s", required, text)
		}
	}
	if strings.Contains(text, "capability:") || strings.Contains(text, "credential:") || strings.Contains(text, "terminal") || strings.Contains(text, "file:") {
		t.Fatalf("bridge config contains forbidden authority: %s", text)
	}
	info, err := os.Stat(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("bridge config mode=%v", info.Mode())
	}
	resolved, err := ResolveTools([]string{"aegis"})
	if err != nil || len(resolved) != 1 || resolved[0] != "aegis" {
		t.Fatalf("Aegis toolset resolution=%v err=%v", resolved, err)
	}
}

func TestVerifyBrokerGatewayRequiresExactSingleTool(t *testing.T) {
	result := func(total float64, names ...string) map[string]any {
		tools := make([]any, 0, len(names))
		for _, name := range names {
			tools = append(tools, map[string]any{"name": name})
		}
		return map[string]any{
			"total": total,
			"sections": []any{map[string]any{
				"name":  "mcp-aegis",
				"tools": tools,
			}},
		}
	}
	tests := []struct {
		name    string
		result  map[string]any
		wantErr bool
	}{
		{name: "exact", result: result(1, "mcp__aegis__github_get_repository")},
		{name: "extra tool", result: result(2, "mcp__aegis__github_get_repository", "unexpected"), wantErr: true},
		{name: "renamed", result: result(1, "unexpected"), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := make(chan gatewayMessage, 2)
			failures := make(chan error, 1)
			ready := gatewayMessage{Method: "event"}
			ready.Params.Type = "gateway.ready"
			messages <- ready
			messages <- gatewayMessage{ID: "bridge-tools-0", Result: test.result}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := verifyBrokerGateway(ctx, io.Discard, messages, failures, "", "")
			if (err != nil) != test.wantErr {
				t.Fatalf("verify error=%v want_error=%t", err, test.wantErr)
			}
		})
	}
}
