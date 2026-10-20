package hermes

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute only the installed native outcome reduction and descriptor builder on
// synthetic failed results. No agent creation, provider inference, host auth
// lookup or model proposal; this is native protocol-shape proof, not live proof.
func TestInstalledFailureDescriptorProtocol(t *testing.T) {
	executable, root := os.Getenv("AEGIS_TEST_HERMES_EXECUTABLE"), os.Getenv("AEGIS_TEST_HERMES_PROBE_ROOT")
	if executable == "" || root == "" {
		t.Skip("explicit installed executable and private probe root required")
	}
	home, err := os.MkdirTemp(root, "native-failure-protocol-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "PATH" && key != "LANG" {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", home)
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d, err := a.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = qualifyProviderTransport(d); err != nil {
		t.Fatal(err)
	}
	script := `import ast, importlib.util, json, pathlib, sys
root=pathlib.Path(sys.argv[1])
# Isolate the two pure outcome functions instead of importing gateway startup.
tree=ast.parse((root/'tui_gateway/prompt_turn.py').read_text())
functions=[n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name in ('_result_status','_turn_outcome')]
assert len(functions)==2
ns={'Any':object,'INTERRUPT_WAITING_FOR_MODEL_PREFIX':'Operation interrupted: waiting for model response'}
exec(compile(ast.Module(body=functions,type_ignores=[]),'<installed-outcome>','exec'),ns)
spec=importlib.util.spec_from_file_location('native_error_surface',root/'agent/error_surface.py')
m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
rows=[]
for reason in ('auth_permanent','format_error','context_overflow','model_not_found'):
    result={'failed':True,'error':'synthetic failure','failure_reason':reason}
    _,status,_=ns['_turn_outcome'](result)
    assert status=='error'
    s=m.build_error_surface_from_result(result,provider='openai-codex',model='gpt-6.1-sol')
    rows.append({'status':status,'error_surface':{'layer':s['layer'],'code':s['code']}})
assert ns['_result_status']({})=='complete'
assert ns['_result_status']({'interrupted':True})=='interrupted'
print(json.dumps(rows))
`
	command := exec.CommandContext(ctx, gatewayPython(d), "-I", "-B", "-c", script, d.Installation)
	command.Dir = home
	command.Env = []string{"HOME=" + home, "HERMES_HOME=" + home, "PATH=" + filepath.Dir(gatewayPython(d)), "PYTHONDONTWRITEBYTECODE=1"}
	command.Stderr = io.Discard
	out, err := command.Output()
	if err != nil {
		t.Fatal("installed native protocol probe failed (raw diagnostics discarded)")
	}
	var rows []map[string]any
	if json.Unmarshal(out, &rows) != nil || len(rows) != 4 {
		t.Fatal("native protocol probe output invalid")
	}
	want := []string{"runtime_auth_auth_permanent", "runtime_provider_format_error", "runtime_provider_context_overflow", "runtime_provider_model_not_found"}
	for i, row := range rows {
		if runtimeFailureReason(row) != want[i] {
			t.Fatal("native descriptor not preserved", i)
		}
	}
	t.Log("installed outcome/error-surface functions executed; closed descriptors preserved; no inference or authentication lookup")
}
