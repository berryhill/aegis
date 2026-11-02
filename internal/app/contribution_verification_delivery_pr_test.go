package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
)

// Synthetic semantic fixture only. Real ParseReadback validates the encoded
// proof; this deliberately does not claim a full canonical or remote campaign.
func retainedUpdateFixture(t *testing.T, reviewed, canonical, update uint64) (delivery.HistorySnapshot, ContributionDeliveryPolicy, delivery.CommitSpec) {
	t.Helper()
	p := ContributionDeliveryPolicy{Timestamp: "2026-10-25T00:00:00Z", Actor: "actor", Branch: "feature/reviewed", BaseHead: strings.Repeat("b", 40), PR: reviewed}
	p.Binding.Candidate, p.Binding.Base = "candidate", "main"
	spec := delivery.CommitSpec{Commit: strings.Repeat("a", 40), Tree: strings.Repeat("c", 40)}
	i := delivery.Intent{Reference: p.Binding.Reference, Operation: delivery.UpdatePullRequest, Actor: p.Actor, Repository: "fixture/controlled", RepositoryID: 7, Head: spec.Commit, BaseHead: p.BaseHead, Branch: p.Branch, Base: "main", PR: update}
	head := &delivery.ReadbackBranch{Ref: p.Branch, SHA: spec.Commit}
	head.Repo.ID, head.Repo.FullName = 7, i.Repository
	base := &delivery.ReadbackBranch{Ref: "main", SHA: p.BaseHead}
	base.Repo.ID, base.Repo.FullName = 7, i.Repository
	d := contribution.Digest([]byte("fixture response"))
	o := delivery.Observation{Outcome: delivery.Confirmed, Head: spec.Commit, PR: update}
	proof := delivery.ReadbackProof{Schema: delivery.ReadbackSchema, Intent: i, Result: o, ObservedResult: o, ObservedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), Facts: []delivery.ReadbackFact{
		{Kind: "actor", HTTPStatus: 200, ID: 1, Login: p.Actor, ResponseDigest: d},
		{Kind: "repository", HTTPStatus: 200, ID: 7, FullName: i.Repository, Target: delivery.ReadbackTarget{Repository: i.Repository}, Permissions: &delivery.ReadbackPermissions{Push: true}, ResponseDigest: d},
		{Kind: "pull", HTTPStatus: 200, Number: update, HTMLURL: fmt.Sprintf("https://github.com/fixture/controlled/pull/%d", update), Target: delivery.ReadbackTarget{Repository: i.Repository, PR: update}, User: &delivery.ReadbackUser{ID: 1, Login: p.Actor}, Head: head, Base: base, ResponseDigest: d},
	}}
	wire, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	o.ProofJSON, o.ProofDigest = string(wire), contribution.Digest(wire)
	if _, err := delivery.ParseReadback(i, o); err != nil {
		t.Fatal("valid proof prerequisite", err)
	}
	commit := delivery.Record{Intent: delivery.Intent{Operation: delivery.Commit, Reference: p.Binding.Reference, Head: spec.Commit}, Outcome: delivery.Confirmed, Observation: delivery.Observation{Outcome: delivery.Confirmed, Head: spec.Commit, Tree: spec.Tree}}
	h := delivery.HistorySnapshot{Histories: [][]delivery.HistoryEnvelope{{{Record: commit}}}}
	if canonical != 0 {
		createIntent := i
		createIntent.Operation, createIntent.PR = delivery.PullRequest, 0
		createResult := delivery.Observation{Outcome: delivery.Confirmed, Head: spec.Commit, PR: canonical}
		createProof := proof
		createProof.Intent, createProof.Result, createProof.ObservedResult = createIntent, createResult, createResult
		createProof.Facts = append([]delivery.ReadbackFact(nil), proof.Facts...)
		createProof.Facts[2].Number = canonical
		createProof.Facts[2].Target.PR = canonical
		createProof.Facts[2].HTMLURL = fmt.Sprintf("https://github.com/fixture/controlled/pull/%d", canonical)
		createWire, err := json.Marshal(createProof)
		if err != nil {
			t.Fatal(err)
		}
		createResult.ProofJSON, createResult.ProofDigest = string(createWire), contribution.Digest(createWire)
		if _, err := delivery.ParseReadback(createIntent, createResult); err != nil {
			t.Fatal("valid creation proof prerequisite", err)
		}
		create := delivery.Record{Intent: createIntent, Outcome: delivery.Confirmed, Observation: createResult}
		h.Histories = append(h.Histories, []delivery.HistoryEnvelope{{Record: create}})
	}
	h.Histories = append(h.Histories, []delivery.HistoryEnvelope{{Record: delivery.Record{Intent: i, Outcome: delivery.Confirmed, Observation: o}}})
	return h, p, spec
}

func TestRetainedOptionalUpdateRejectsForeignReviewedPR(t *testing.T) {
	h, p, spec := retainedUpdateFixture(t, 7, 0, 99)
	controlHistory, control, controlSpec := retainedUpdateFixture(t, 99, 99, 99)
	if got := assessRetainedPublication(controlHistory, control, "no-merge", controlSpec, nil); got != "publication_assessed" {
		t.Fatal("matching control", got)
	}
	if got := assessRetainedPublication(h, p, "no-merge", spec, nil); got == "publication_assessed" {
		t.Fatal("foreign optional PR accepted")
	}
}

func TestRetainedCanonicalPRResolution(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		reviewed, canonical, update uint64
		ok                          bool
	}{
		{"canonical-without-pin", 0, 7, 7, true},
		{"matching-reviewed-and-canonical", 7, 7, 7, true},
		{"foreign-without-pin", 0, 7, 99, false},
		{"contradicting-reviewed-and-canonical", 99, 7, 99, false},
		{"no-independent-PR", 0, 0, 99, false},
		{"pin-without-independent-creation", 99, 0, 99, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, p, _ := retainedUpdateFixture(t, tc.reviewed, tc.canonical, tc.update)
			pr, err := retainedContributionPR(h, p)
			if tc.ok {
				if err != nil || pr != tc.update {
					t.Fatalf("resolved=%d err=%v", pr, err)
				}
			} else if err == nil && pr == tc.update {
				t.Fatalf("foreign PR resolved: %d", pr)
			}
		})
	}
	h, p, _ := retainedUpdateFixture(t, 7, 7, 7)
	h.Histories = append(h.Histories, h.Histories[1])
	if _, err := retainedContributionPR(h, p); err == nil {
		t.Fatal("duplicate PR creation accepted")
	}
}

func TestRetainedPRProofTargetAndObservationMustMatch(t *testing.T) {
	h, p, _ := retainedUpdateFixture(t, 7, 0, 7)
	r := h.Histories[len(h.Histories)-1][0].Record
	proof, err := delivery.ParseReadback(r.Intent, r.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if !retainedDeliveryPublication(proof, p, 7) {
		t.Fatal("matching PR proof rejected")
	}
	for _, mutation := range []string{"intent", "result", "request-target", "number", "url"} {
		t.Run(mutation, func(t *testing.T) {
			copy := proof
			copy.Facts = append([]delivery.ReadbackFact(nil), proof.Facts...)
			switch mutation {
			case "intent":
				copy.Intent.PR = 99
			case "result":
				copy.Result.PR = 99
			case "request-target":
				copy.Facts[2].Target.PR = 99
			case "number":
				copy.Facts[2].Number = 99
			case "url":
				copy.Facts[2].HTMLURL = "https://github.com/fixture/controlled/pull/99"
			}
			if retainedDeliveryPublication(copy, p, 7) {
				t.Fatal("cross-PR proof accepted")
			}
		})
	}
}
