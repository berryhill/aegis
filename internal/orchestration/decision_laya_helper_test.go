package orchestration

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLayaPythonHelperProtocolWithoutModel(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	// Execute the exact embedded helper as a module with a fake Router. No model
	// imports, network access, privileged state, or live Laya inference.
	wrapper := `import sys,json
ns={"__name__":"protocol_test"}
exec(sys.stdin.read(),ns)
class Router:
 def predict(self,state,schema):
  assert state == "bounded task"
  assert set(schema)=={"specified","result_defined"}
  assert all(x["type"]=="choice" for x in schema.values())
  return {"answers":{key:{"choice":"yes","answer_confidence":0.6} for key in schema}}
assert ns["decide"]({"version":1,"kind":"gate","state":"bounded task"},Router)=={"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.6},"result_defined":{"choice":"yes","answer_confidence":0.6}}}
for answer in ({"choice":"maybe","answer_confidence":0.9},{"choice":"yes","answer_confidence":0.59},{"choice":"yes","answer_confidence":True}):
 class BadRouter:
  def predict(self,state,schema): return {"answers":{key:answer for key in schema}}
 try: ns["decide"]({"version":1,"kind":"gate","state":"bounded task"},BadRouter)
 except ValueError: pass
 else: raise AssertionError("unsafe answer accepted")
try: json.loads('{"version":1,"version":1}',object_pairs_hook=ns["unique_object"])
except ValueError: pass
else: raise AssertionError("duplicate accepted")
print("helper protocol ok")`
	cmd := exec.Command(python, "-I", "-c", wrapper)
	cmd.Stdin = strings.NewReader(layaHelperSource)
	output, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "helper protocol ok" {
		t.Fatalf("protocol=%s err=%v", output, err)
	}
}
