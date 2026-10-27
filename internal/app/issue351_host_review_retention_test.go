package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
)

func TestIssue351HostReviewRunCanonicalRetention(t *testing.T) {
	s, sub, base, _ := controllerParticipantFixture(t)
	c := base.Contract
	c.ExactBytes = false
	revision, _, err := loop.NewDoerReusableRevision("controller-shared", 1, "", loop.DoerReusableContract{MaxAttempts: c.MaxAttempts})
	if err != nil {
		t.Fatal(err)
	}
	encode := func(value any) json.RawMessage {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	run := QueueLoopInput{Agent: base.Agent, Loop: loopRevisionRef(revision), IdempotencyKey: "same-intent", Activate: true, Inputs: []graph.NormalizedInput{
		{PortID: "writable_files", Type: graph.TypeArray, Value: json.RawMessage(`[ "result.txt" ]`)},
		{PortID: "verify_file", Type: graph.TypeString, Value: encode(c.VerifyFile)},
		{PortID: "workspace", Type: graph.TypeString, Value: encode(c.Workspace)},
		{PortID: "expected_text", Type: graph.TypeString, Value: encode(*c.ExpectedText)},
		{PortID: "task", Type: graph.TypeString, Value: encode(c.Task)},
	}}
	input := DoerDraftInput{CreationKey: "host-canonical", Agent: base.Agent, LoopID: revision.LoopID, Revision: 1, Contract: c, HostRun: &run}
	draft, err := s.SaveDoerDraftAs(context.Background(), sub, input)
	if err != nil {
		t.Fatal(err)
	}
	readback, err := s.ReadDoerDraftAs(context.Background(), sub, draft.ID)
	if err != nil || !reflect.DeepEqual(draft, readback) {
		t.Fatal("pretty-store host scope drift", err)
	}
	wire, err := json.MarshalIndent(doerDraftSet{PrincipalID: sub.PrincipalID, Drafts: []DoerDraft{draft}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeDoerDraftSet(wire, sub.PrincipalID)
	if err != nil || !reflect.DeepEqual(decoded.Drafts[0], draft) {
		t.Fatal("protected review compact/pretty scope drift", err)
	}
	valid := DoerDraftInput{Agent: draft.Agent, LoopID: draft.LoopID, Revision: draft.Revision, Contract: draft.Contract, HostRun: draft.HostRun}
	validWire, err := json.Marshal(valid)
	if err != nil || validateDoerDraftInput(valid) != nil {
		t.Fatal("valid exact host-review shape rejected", err)
	}
	for name, mutate := range map[string]func(*DoerDraftInput){
		"empty-key":           func(v *DoerDraftInput) { v.HostRun.IdempotencyKey = "" },
		"oversized-key":       func(v *DoerDraftInput) { v.HostRun.IdempotencyKey = strings.Repeat("k", 257) },
		"wrong-agent":         func(v *DoerDraftInput) { v.HostRun.Agent.Revision++ },
		"wrong-loop-digest":   func(v *DoerDraftInput) { v.HostRun.Loop.Digest = "sha256:" + strings.Repeat("a", 64) },
		"missing-loop-digest": func(v *DoerDraftInput) { v.HostRun.Loop.Digest = "" },
		"recovery-item":       func(v *DoerDraftInput) { v.HostRun.QueueItemID = "other-queue" },
		"mixed-exact-bytes":   func(v *DoerDraftInput) { v.Contract.ExactBytes = true },
		"changed-budget":      func(v *DoerDraftInput) { v.Contract.MaxAttempts++ },
		"missing-inputs":      func(v *DoerDraftInput) { v.HostRun.Inputs = nil },
		"duplicate-input":     func(v *DoerDraftInput) { v.HostRun.Inputs = append(v.HostRun.Inputs, v.HostRun.Inputs[0]) },
		"changed-task":        func(v *DoerDraftInput) { v.Contract.Task = "Unreviewed different task" },
	} {
		t.Run(name, func(t *testing.T) {
			var bad DoerDraftInput
			if err := json.Unmarshal(validWire, &bad); err != nil {
				t.Fatal(err)
			}
			mutate(&bad)
			if validateDoerDraftInput(bad) == nil {
				t.Fatal("inexact or malformed retained host scope accepted")
			}
		})
	}
	// Equivalent ordering/whitespace retain the exact reviewed version and keys.
	for i, j := 0, len(run.Inputs)-1; i < j; i, j = i+1, j-1 {
		run.Inputs[i], run.Inputs[j] = run.Inputs[j], run.Inputs[i]
	}
	replay, err := s.SaveDoerDraftAs(context.Background(), sub, input)
	if err != nil || !reflect.DeepEqual(replay, draft) {
		t.Fatal("canonical same-intent replay changed draft", err)
	}
	run.IdempotencyKey = "different-intent"
	if _, err := s.SaveDoerDraftAs(context.Background(), sub, input); !errors.Is(err, ErrConflict) {
		t.Fatal("same creation identity accepted changed Run", err)
	}
	if draft.HostRun.IdempotencyKey != "same-intent" {
		t.Fatal("caller pointer changed retained scope")
	}
	if _, err := s.ValidateDoerDraftPublicationAs(context.Background(), sub, draft.ID, draft.Version); err == nil {
		t.Fatal("host-only scope gained publication authority")
	}
}
