package managergateway

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"testing"
)

type provenanceHistoryView struct {
	*fleetbadger.Store
	mode string
}

func (v *provenanceHistoryView) GetContributionDeliveryHistory(ctx context.Context, owner, key string) (delivery.HistorySnapshot, error) {
	h, err := v.Store.GetContributionDeliveryHistory(ctx, owner, key)
	if err != nil {
		return h, err
	}
	out := make([][]delivery.HistoryEnvelope, 0, len(h.Histories))
	for _, chain := range h.Histories {
		if chain[0].Record.Intent.Operation != delivery.PullRequest {
			out = append(out, chain)
			continue
		}
		if v.mode == "missing" {
			continue
		}
		chain = append([]delivery.HistoryEnvelope(nil), chain...)
		previous := ""
		for n := range chain {
			e := &chain[n]
			e.Record.Intent.Candidate = contribution.Digest([]byte("foreign-candidate"))
			if e.Record.ProofJSON != "" {
				var proof delivery.ReadbackProof
				if json.Unmarshal([]byte(e.Record.ProofJSON), &proof) != nil {
					panic("fixture proof")
				}
				proof.Intent = e.Record.Intent
				wire, _ := json.Marshal(proof)
				e.Record.ProofJSON, e.Record.ProofDigest = string(wire), contribution.Digest(wire)
			}
			e.PreviousDigest, e.Digest = previous, ""
			wire, _ := json.Marshal(e)
			e.Digest = contribution.Digest(wire)
			previous = e.Digest
		}
		out = append(out, chain)
	}
	h.Histories = out
	return h, nil
}

// Injects independently well-formed negative readback at the real final verifier,
// not merely the PR resolver. The original store remains untouched.
type provenanceNegativeCompletion struct {
	delivery.CompletionOwner
	t        *testing.T
	s        *app.Service
	store    *fleetbadger.Store
	verifier *app.ContributionFinalVerifier
	subject  core.Subject
	key      string
}

// Wrapping preclosure verification must retain post-issue finalization.
func (v provenanceNegativeCompletion) FinalizeContributionCompletion(ctx context.Context, ref delivery.Reference) error {
	owner, ok := v.CompletionOwner.(delivery.FinalCompletionOwner)
	if !ok {
		return errors.New("fixture completion owner lacks finalization")
	}
	return owner.FinalizeContributionCompletion(ctx, ref)
}

func (v provenanceNegativeCompletion) VerifyContributionCompletion(ctx context.Context, ref delivery.Reference) (string, error) {
	original := v.s.FleetRepository
	defer func() { v.s.FleetRepository = original }()
	for _, mode := range []string{"missing", "foreign"} {
		view := &provenanceHistoryView{Store: v.store, mode: mode}
		h, err := view.GetContributionDeliveryHistory(ctx, v.subject.PrincipalID, v.key)
		if err != nil || delivery.ValidateHistorySnapshot(h) != nil {
			v.t.Fatalf("%s negative fixture invalid: %v", mode, err)
		}
		v.s.FleetRepository = view
		_, report, err := v.verifier.VerifyPreClosureAs(ctx, v.subject, v.key)
		if err == nil || (report.Code != "pr_invalid" && report.Code != "delivery_partial" && report.Code != "publication_invalid") {
			v.t.Fatalf("complete verifier did not reject %s creation at provenance boundary: %+v err=%v", mode, report, err)
		}
		v.s.FleetRepository = original
	}
	return v.CompletionOwner.VerifyContributionCompletion(ctx, ref)
}
