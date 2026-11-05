package api

import (
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func loopDetailBranchFixture(t *testing.T) (loop.LoopRevision, loop.LoopValidationResult) {
	t.Helper()
	inputs := []loop.Port{{ID: "document", Type: loop.TypeString, Required: true}, {ID: "optional", Type: loop.TypeBoolean}}
	value := []loop.Port{{ID: "value", Type: loop.TypeString, Required: true}}
	result := []loop.Port{{ID: "result", Type: loop.TypeString, Required: true}}
	definition := loop.LoopRevision{LoopID: "branch-loop", Name: "Branching input and evidence fixture", Revision: 1, EntryStepID: "choose", Inputs: inputs, Outputs: result,
		Steps: []loop.Step{
			{ID: "choose", Kind: loop.StepGate, InputPorts: inputs, OutputPorts: value, Gate: &loop.GateDefinition{Mode: "exclusive"}, Retry: loop.RetryPolicy{MaxAttempts: 1}},
			{ID: "left", Kind: loop.StepAction, InputPorts: value, OutputPorts: result, Retry: loop.RetryPolicy{MaxAttempts: 1}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "record", MediaType: "text/plain", ExpectedDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", VerifierID: "definition-fixture", PolicyVersion: "1"}}},
			{ID: "right", Kind: loop.StepAction, InputPorts: value, OutputPorts: result, Retry: loop.RetryPolicy{MaxAttempts: 1}},
			{ID: "done", Kind: loop.StepTerminal, InputPorts: result, OutputPorts: result, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded, OutputMappings: []loop.PortMapping{{SourcePort: "result", TargetPort: "result"}}}},
		},
		Transitions: []loop.Transition{
			{ID: "choose-left", FromStepID: "choose", ToStepID: "left", Condition: "left", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "choose-right", FromStepID: "choose", ToStepID: "right", Condition: "right", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}},
			{ID: "left-done", FromStepID: "left", ToStepID: "done", Mappings: []loop.PortMapping{{SourcePort: "result", TargetPort: "result"}}},
			{ID: "right-done", FromStepID: "right", ToStepID: "done", Mappings: []loop.PortMapping{{SourcePort: "result", TargetPort: "result"}}},
		}, RequiredEvidence: []loop.EvidenceRequirement{{Claim: "record", ProducerStepID: "left"}},
	}
	revision, validation, err := loop.NewRevision(definition)
	if err != nil || validation.Outcome != loop.ValidationValid { t.Fatalf("branch fixture invalid: %v %+v", err, validation.Issues) }
	return revision, validation
}

func loopDetailCycleFixtures(t *testing.T) []loop.LoopRevision {
	t.Helper()
	one := loop.RetryPolicy{MaxAttempts: 1}
	terminal := loop.Step{ID: "done", Kind: loop.StepTerminal, Retry: one, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}
	gate := func(id string) loop.Step { return loop.Step{ID: id, Kind: loop.StepGate, Retry: one, Gate: &loop.GateDefinition{Mode: "exclusive"}} }
	inputs := []loop.LoopRevision{
		{LoopID: "self-loop", Name: "Bounded self-cycle fixture", Revision: 1, EntryStepID: "g", Steps: []loop.Step{gate("g"), terminal}, Transitions: []loop.Transition{
			{ID: "self", FromStepID: "g", ToStepID: "g", Condition: "retry", MaxTraversals: 2},
			{ID: "exit", FromStepID: "g", ToStepID: "done", Condition: "exit"},
		}},
		{LoopID: "multi-loop", Name: "Two cyclic components with separate bounds", Revision: 1, EntryStepID: "g", Steps: []loop.Step{gate("g"), gate("b"), {ID: "c", Kind: loop.StepAction, Retry: one}, terminal}, Transitions: []loop.Transition{
			{ID: "first-self", FromStepID: "g", ToStepID: "g", Condition: "repeat", MaxTraversals: 2},
			{ID: "advance", FromStepID: "g", ToStepID: "b", Condition: "advance", MaxTraversals: 999},
			{ID: "second-retry", FromStepID: "b", ToStepID: "c", Condition: "retry", MaxTraversals: 3},
			{ID: "second-return", FromStepID: "c", ToStepID: "b", MaxTraversals: 5},
			{ID: "finish", FromStepID: "b", ToStepID: "done", Condition: "finish"},
		}},
	}
	result := make([]loop.LoopRevision, 0, len(inputs))
	for _, input := range inputs {
		revision, validation, err := loop.NewRevision(input)
		if err != nil || validation.Outcome != loop.ValidationValid { t.Fatalf("cycle fixture invalid: %v %+v", err, validation.Issues) }
		result = append(result, revision)
	}
	return result
}
