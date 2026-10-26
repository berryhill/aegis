package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestRetainedPublicationExactHeadAndOptionalEffects(t *testing.T) {
	p := ContributionDeliveryPolicy{Timestamp: "2026-10-25T00:00:00Z"}
	p.Binding.Candidate = "candidate"
	spec := delivery.CommitSpec{Commit: "reviewed-head", Tree: "reviewed-tree"}
	commit := delivery.Record{Intent: delivery.Intent{Operation: delivery.Commit, Reference: delivery.Reference{Candidate: "candidate"}, Head: spec.Commit}, Outcome: delivery.Confirmed, Observation: delivery.Observation{Outcome: delivery.Confirmed, Head: spec.Commit, Tree: spec.Tree}}
	h := delivery.HistorySnapshot{Histories: [][]delivery.HistoryEnvelope{{{Record: commit}}}}
	if got := assessRetainedPublication(h, p, "local-only", spec, nil); got != "publication_assessed" {
		t.Fatal(got)
	}
	badSpec := spec
	badSpec.Commit = "unreviewed-head"
	if got := assessRetainedPublication(h, p, "local-only", badSpec, nil); got != "publication_invalid" {
		t.Fatal("arbitrary local publication head accepted", got)
	}
	for _, op := range []delivery.Operation{delivery.UpdatePullRequest, delivery.ReconcileIssue, delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.TransferGraph} {
		for _, outcome := range []delivery.Outcome{delivery.Unknown, delivery.Denied, delivery.Failed, delivery.Confirmed} {
			optional := delivery.Record{Intent: delivery.Intent{Operation: op, Reference: p.Binding.Reference}, Outcome: outcome, Observation: delivery.Observation{Outcome: outcome}}
			copy := h
			copy.Histories = append(append([][]delivery.HistoryEnvelope(nil), h.Histories...), []delivery.HistoryEnvelope{{Record: optional}})
			got := assessRetainedPublication(copy, p, "verified-protected-base-merge", spec, nil)
			if got == "publication_assessed" {
				t.Fatalf("optional %s/%s ignored", op, outcome)
			}
		}
	}
}
