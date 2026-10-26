package orchestration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
)

func TestDoerLegacyTerminalDoesNotPropagateInputs(t *testing.T) {
	for _, managed := range []bool{false, true} {
		e := &doerStepExecutor{roles: doerRoles{Admit: func(context.Context, string) error { return nil }}}
		if managed {
			e.managedResult = func(context.Context) (json.RawMessage, error) { return nil, nil }
		}
		result, err := e.Execute(context.Background(), looprun.StepRequest{Step: loop.Step{Kind: loop.StepTerminal}, Inputs: looprun.Values{"result": json.RawMessage(`"artifact"`)}})
		if err != nil {
			t.Fatal(err)
		}
		if managed && len(result.Outputs) != 1 {
			t.Fatal("managed terminal lost selected result")
		}
		if !managed && len(result.Outputs) != 0 {
			t.Fatal("legacy terminal propagated inputs")
		}
	}
}
