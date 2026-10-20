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

func TestProviderFormatProbeClosedMetadata(t *testing.T) {
	p := map[string]any{"error_surface": map[string]any{"layer": "provider", "code": "format_error", "request_parameter": "temperature", "request_code": "unsupported_parameter", "message": "secret-canary"}}
	d := closedProviderFormatDiagnostic(p)
	if d.Parameter != "temperature" || d.Code != "unsupported_parameter" {
		t.Fatal(d)
	}
	p["error_surface"].(map[string]any)["request_parameter"] = "temperature secret-canary"
	p["error_surface"].(map[string]any)["request_code"] = "token-shaped-canary"
	if closedProviderFormatDiagnostic(p) != (ProviderFormatDiagnostic{}) {
		t.Fatal("unreviewed metadata survived")
	}
	r := providerAttempt(t.TempDir())
	r.Launch.AuthorityContext.Authority.Capabilities = nil
	r.ProviderFormatProbe = true
	r.Input = ProviderFormatPrompt
	r.Bounds.Duration = time.Minute
	r.Bounds.OutputBytes = 4096
	if !validProviderFormatProbe(r) {
		t.Fatal("fixed diagnostic rejected")
	}
	r.Input = "create result.txt"
	if validProviderFormatProbe(r) {
		t.Fatal("task prompt accepted")
	}
}

func TestProviderFormatProbeHTTPStatusClassification(t *testing.T) {
	code := `import sys
scope={'__name__':'fixture'}
exec(sys.stdin.read(),scope)
for status,expected in [(400,('provider','format_error')),(401,('auth','auth')),(403,('auth','auth')),(429,('provider','rate_limit')),(500,('provider','server_error'))]:
    result=scope['closed_status'](status)
    assert (result['layer'],result['code'])==expected
assert scope['closed_details']({'error':{'param':'secret-canary','code':'unreviewed-canary','message':'raw-canary'}})=={}
`
	command := exec.Command("python3", "-I", "-B", "-c", code)
	command.Stdin = strings.NewReader(providerFormatGateway)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("closed diagnostic classification failed: %v %s", err, out)
	}
}

// Real installed gateway -> real native AIAgent request generation -> real SDK
// -> in-memory HTTP fixture. No live account, provider request or task execution.
func TestInstalledProviderFormatProbeProtocol(t *testing.T) {
	executable, root := os.Getenv("AEGIS_TEST_HERMES_EXECUTABLE"), os.Getenv("AEGIS_TEST_HERMES_PROBE_ROOT")
	if executable == "" || root == "" {
		t.Skip("explicit installed runtime/private root required")
	}
	home, e := os.MkdirTemp(root, "provider-format-native-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(home)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	a := New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d, e := a.Discover(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = qualifyProviderFormat(d); e != nil {
		t.Fatal(e)
	}
	token, e := NewProviderAccessToken("codex", syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = writeProviderAuthentication(home, token); e != nil {
		t.Fatal(e)
	}
	script := `import sys, pathlib, os, json, socket
sys.path.insert(0,sys.argv[1])
import httpx
# No socket connection can occur, even if a native auxiliary path escapes the fixture.
socket.socket.connect=lambda *a,**k: (_ for _ in ()).throw(RuntimeError('offline_fixture_network_denied'))
original=httpx.Client.__init__
count=0
def fixture(request):
 global count
 count+=1
 pathlib.Path(os.environ['HOME'],'fixture-count').write_text(str(count))
 return httpx.Response(400,json={'error':{'param':'temperature','code':'unsupported_parameter','message':'synthetic-provider-secret-canary'}},request=request)
def init(self,*a,**k):
 k['transport']=httpx.MockTransport(fixture)
 k['mounts']={}
 original(self,*a,**k)
httpx.Client.__init__=init
ns={'__name__':'aegis_probe_fixture'}
exec(sys.argv[2],ns)
original_install=ns['install_guard']
def instrument(*args):
 original_install(*args)
 guarded=httpx.Client.send
 def observed(*a,**k):
  pathlib.Path(os.environ['HOME'],'send-hit').write_text('1')
  request=a[1]
  hosts={'chatgpt.com','api.openai.com','openrouter.ai'}
  paths={'/backend-api/codex/responses','/v1/responses','/responses','/backend-api/codex/models','/api/v1/chat/completions','/v1/chat/completions'}
  host=request.url.host if request.url.host in hosts else 'other'
  path=request.url.path if request.url.path in paths else 'other'
  method=request.method if request.method in ('POST','GET') else 'other'
  flags={'method':method,'https':request.url.scheme=='https','port_default':request.url.port in (None,443),'query':bool(request.url.query),'userinfo':bool(request.url.username or request.url.password),'exact_url':str(request.url)=='https://chatgpt.com/backend-api/codex/responses'}
  try:
   body=json.loads(request.content)
   inputs=body.get('input')
   flags.update(model_match=body.get('model')==os.environ['HERMES_MODEL'],tools_empty=not body.get('tools'),store_false=body.get('store') is False,input_count=len(inputs) if isinstance(inputs,list) else -1)
   flags['exact_user_prompt']=any(m.get('role')=='user' and m.get('content')==[{'type':'input_text','text':ns['PROMPT']}] for m in inputs) if isinstance(inputs,list) else False
   flags['plain_user_prompt']=any(m.get('role')=='user' and m.get('content')==ns['PROMPT'] for m in inputs) if isinstance(inputs,list) else False
  except Exception:
   flags['body_valid']=False
  pathlib.Path(os.environ['HOME'],'route-code').write_text(host+' '+path+' '+json.dumps(flags))
  try:
   return guarded(*a,**k)
  except Exception as e:
   # Test-only reviewed guard rejection; do not retain arbitrary exceptions.
   text=str(e)
   known=('aegis_provider_probe_request_limit','aegis_provider_probe_destination_denied','aegis_provider_probe_contract_denied','aegis_provider_probe_input_denied')
   with pathlib.Path(os.environ['HOME'],'guard-code').open('a') as f:
    f.write((text if text in known else 'other')+'\n')
   raise
 httpx.Client.send=observed
ns['install_guard']=instrument
ns['main']()
`
	command := exec.CommandContext(ctx, gatewayPython(d), "-I", "-B", "-c", script, d.Installation, providerFormatGateway)
	command.Dir = home
	command.Env = append(providerAuthenticationEnv(minimalEnv(home, nil)), "HERMES_PYTHON_SRC_ROOT="+d.Installation, "HERMES_TUI_TOOLSETS="+emptyToolset, "HERMES_TUI_SKILLS=", "HERMES_DISABLE_AUTO_SKILLS=1", "HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1", "HERMES_TUI_PROVIDER=codex", "HERMES_MODEL=gpt-6.1-sol", "HERMES_TUI_MODEL=gpt-6.1-sol")
	stdin, e := command.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	stdout, e := command.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	command.Stderr = io.Discard
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = stdin.Close(); _ = command.Process.Kill(); _ = command.Wait() }()
	messages := make(chan gatewayMessage, 32)
	errs := make(chan error, 1)
	go readGateway(stdout, messages, errs)
	if _, e = waitGateway(ctx, messages, errs, func(m gatewayMessage) bool { return m.Params.Type == "gateway.ready" }); e != nil {
		t.Fatal("ready", e)
	}
	if e = verifyEmptyGateway(ctx, stdin, messages, errs); e != nil {
		t.Fatal("tools", e)
	}
	_ = writeGateway(stdin, "create", "session.create", map[string]any{"source": "aegis-attempt", "cols": 100})
	created, e := waitGateway(ctx, messages, errs, func(m gatewayMessage) bool { return m.ID == "create" })
	if e != nil || created.Error != nil {
		t.Fatal("create failed")
	}
	id, _ := created.Result["session_id"].(string)
	if id == "" {
		id, _ = created.Result["id"].(string)
	}
	if id == "" {
		t.Fatal("missing id")
	}
	_ = writeGateway(stdin, "prompt", "prompt.submit", map[string]any{"session_id": id, "text": ProviderFormatPrompt})
	terminal, e := waitGateway(ctx, messages, errs, func(m gatewayMessage) bool {
		return m.Params.Type == "aegis.provider-format-error" || (m.Params.SessionID == id && (m.Params.Type == "message.complete" || m.Params.Type == "error"))
	})
	if e != nil {
		t.Fatal("terminal", e)
	}
	// Do not print arbitrary native output even on fixture failure.
	diagnostic := closedProviderFormatDiagnostic(terminal.Params.Payload)
	if diagnostic.Parameter != "temperature" || diagnostic.Code != "unsupported_parameter" {
		b, _ := json.Marshal(diagnostic)
		hit, _ := os.ReadFile(filepath.Join(home, "send-hit"))
		count, _ := os.ReadFile(filepath.Join(home, "fixture-count"))
		guard, _ := os.ReadFile(filepath.Join(home, "guard-code"))
		route, _ := os.ReadFile(filepath.Join(home, "route-code"))
		t.Fatalf("closed diagnostic missing: %s; reason=%s hit=%s count=%s guard=%s route=%s", b, runtimeFailureReason(terminal.Params.Payload), hit, count, guard, route)
	}
	count, e := os.ReadFile(filepath.Join(home, "fixture-count"))
	if e != nil || string(count) != "1" {
		t.Fatal("expected exactly one native HTTP request")
	}
	t.Log("real installed gateway/native request/SDK fixture: one send, closed parameter/code, synthetic auth, no network")
}
