package orchestration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/loop"
)

// Synthetic protocol tier: executes the real managed runner, controller writes,
// selected-byte verifier, durable checkpoints and aggregate. No live provider.
func configureSyntheticManagedChain(t *testing.T, w *QueueWorker, p, c loop.LoopRevision, bad bool, calls *rawChainAdapter) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!%s
import sys,json,base64

def emit(v):
 print(json.dumps(v),flush=True)
def event(kind,payload):
 emit({"jsonrpc":"2.0","method":"event","params":{"type":kind,"session_id":"queue-runtime-session","payload":payload}})
def evidence(v):
 if isinstance(v,str):
  try:
   d=json.loads(v)
   if isinstance(d,dict) and 'evidence' in d:return d['evidence']
  except Exception:pass
 if isinstance(v,dict):
  for x in v.values():
   d=evidence(x)
   if d is not None:return d
 if isinstance(v,list):
  for x in v:
   d=evidence(x)
   if d is not None:return d
 return None
emit({"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}})
json.loads(sys.stdin.readline())
emit({"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}})
json.loads(sys.stdin.readline())
emit({"jsonrpc":"2.0","id":"create","result":{"session_id":"queue-runtime-session"}})
r=json.loads(sys.stdin.readline())
d=evidence(r)
emit({"jsonrpc":"2.0","id":"prompt","result":{"accepted":True}})
event("message.start",{})
text="Recorded outcome"
if isinstance(d,dict) and 'source_type' in d:
 assert 'expected_text' not in d
 if d['source_type']=='string':
  data=d['source_text'].encode()
  if %s:data+=b'\n'
 else:
  assert d['source_provenance']['ArtifactID']
  data=b'confirmed:'+base64.b64decode(d['source_bytes'])
 text=json.dumps({"report":"Derived selected bytes from actual typed source", "edits":[{"path":d['writable_files'][0],"content":base64.b64encode(data).decode()}]})
event("message.complete",{"status":"complete","text":text})
for line in sys.stdin:pass
`, python, map[bool]string{true: "True", false: "False"}[bad])
	adapter := routedHermesTestAdapter(t, t.TempDir(), script)
	a, _ := p.ManagedDoer.Doer.Digest()
	b, _ := c.ManagedDoer.Doer.Digest()
	if err := w.ConfigureImplementation(config.Implementation{GoBinary: python, AuthorizedContracts: []string{a, b}}, t.TempDir(), adapter.hermes); err != nil {
		t.Fatal(err)
	}
	w.implementation.decision = NewLayaDecisionAdapter(fakeLayaProcess(func(_ context.Context, input []byte) ([]byte, error) {
		if strings.Contains(string(input), `"kind":"verdict"`) {
			calls.calls = append(calls.calls, "managed")
			return []byte(`{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"yes","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}`), nil
		}
		return []byte(`{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}`), nil
	}))
}
