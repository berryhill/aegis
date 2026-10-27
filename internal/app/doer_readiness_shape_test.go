package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestDoerReadinessSchemaActions(t *testing.T) {
	for _, reason := range []string{"doer_host_approval_required", "doer_host_approval_absent", "doer_host_approval_invalid", "doer_host_approval_expired"} {
		for _, schema := range []string{"", loop.DoerRevisionSchemaVersion, loop.DoerReusableSchemaVersion} {
			want := "review_exact_host_write_contract"
			if schema == loop.DoerReusableSchemaVersion {
				want = "review_exact_reusable_run_host_write"
			}
			r := newDoerReadiness()
			r.definitionSchema = schema
			r.block(&r.Controller, reason)
			q := QueueLoopResult{Readiness: &r}
			q.blockedDoer(reason)
			if r.RequiredAction != want || q.RequiredAction != want {
				t.Fatalf("%s %s: %+v %+v", schema, reason, r, q)
			}
			legacy := QueueLoopResult{}
			legacy.blockedDoer(reason)
			if legacy.RequiredAction != "review_exact_host_write_contract" {
				t.Fatal(legacy)
			}
		}
	}
}

func TestDoerReadinessRejectsMixedShapesBeforeEffects(t *testing.T) {
	candidate, _, err := loop.NewDoerRevision("shape", 1, "", loop.DoerContract{Task: "Write result", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := json.Marshal(DoerReadinessInput{Candidate: candidate})
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(base, &fields); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, key, value string }{
		{"partial-schema", "loop", `{"schema_version":"aegis.revision-ref.v1"}`},
		{"partial-revision", "loop", `{"revision":1}`},
		{"partial-digest", "loop", `{"digest":"sha256:` + strings.Repeat("a", 64) + `"}`},
		{"partial-id", "loop", `{"id":"partial"}`},
		{"inputs", "inputs", `[]`}, {"activate-false", "activate", `false`},
		{"key-empty", "idempotency_key", `""`}, {"null-loop", "loop", `null`},
		{"null-candidate", "candidate", `null`}, {"extra", "unknown", `true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := map[string]json.RawMessage{}
			for k, v := range fields {
				copy[k] = v
			}
			copy[tc.key] = json.RawMessage(tc.value)
			b, _ := json.Marshal(copy)
			var input DoerReadinessInput
			if err := json.Unmarshal(b, &input); err == nil {
				t.Fatalf("accepted %s", b)
			}
		})
	}
	var compatibility DoerReadinessInput
	if err = json.Unmarshal(base, &compatibility); err != nil {
		t.Fatalf("typed v4 compatibility: %v", err)
	}
	// A nil service proves typed malformed requests cannot reach any effect/store.
	var s *Service
	for _, input := range []DoerReadinessInput{
		{Candidate: candidate, Loop: reference.RevisionRef{Revision: 1}},
		{Candidate: candidate, Activate: true},
		{Candidate: candidate, IdempotencyKey: strings.Repeat("x", 257)},
		{Candidate: loop.LoopRevision{LoopID: "hidden"}, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "exact", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}},
	} {
		if _, err := s.ReadDoerReadinessAs(context.Background(), core.Subject{}, input); err == nil {
			t.Fatal("typed mixed shape accepted")
		}
	}
}
