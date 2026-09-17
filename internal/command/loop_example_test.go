package command

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"strings"
	"testing"
)

func TestLoopExampleStatesExecutionLimitations(t *testing.T) {
	var out, diagnostic bytes.Buffer
	cmd := NewRoot(Dependencies{Out: &out, Err: &diagnostic})
	cmd.SetArgs([]string{"loops", "example"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var input app.PublishLoopInput
	if err := json.Unmarshal(out.Bytes(), &input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loop.NewRevision(input.Revision); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"empty evidence requirements do not prove verification", "not executable instructions", "not an enforced corrective attempt", "Publication is not activation or execution"} {
		if !strings.Contains(diagnostic.String(), text) {
			t.Fatalf("missing limitation %q", text)
		}
	}
}
