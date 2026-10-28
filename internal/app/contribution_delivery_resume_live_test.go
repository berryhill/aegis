package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

// Forged readback-shaped data is never a substitute for the live canonical role
// gate. The positive case is exercised by the production Unix HTTP journey.
func TestAcceptedContributionControllerRejectsUnjoinedOrForgedReadback(t *testing.T) {
	s, _, _, lr, reader := contributionFixture(t)
	uid, err := strconv.ParseUint(s.Config.Principal.UID, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	ctx, sub, err := s.AuthenticateContributionUnixPeerRequest(context.Background(), uint32(uid))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.admitContributionAs(ctx, sub, "session", lr, reader.source.URL.URL, "live-readback-denials", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := contribution.Digest([]byte("forged readback, not accepted evidence"))
	record := orchestration.ContributionRecord{State: "candidate_accepted", RunID: c.RunID, Contract: c.Digest, Passes: []implementation.ContributionPass{{Candidate: implementation.ContributionCandidate{Digest: digest}, Check: implementation.ContributionCheck{Outcome: "passed"}}}}
	state := contribution.Continuity{ContractDigest: c.Digest, QueueItemID: c.QueueItemID, CandidateEvidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate", Digest: digest}, RoleAuthorities: map[string]contribution.RoleAuthority{}, RoleEpochs: map[string]uint64{}, RoleStops: map[string]contribution.RoleStop{}, NativeBoundaries: map[string]contribution.NativeBoundary{}, NativeEffects: map[string]contribution.NativeEffect{}}
	for _, name := range []string{"controller", "builder", "tester"} {
		role := contribution.RoleAuthority{Role: name, Epoch: 1, SessionID: "forged-" + name}
		state.RoleAuthorities[name] = role
		state.RoleEpochs[name] = 1
		if name == "controller" {
			continue
		}
		binding := contribution.NativeBinding{IntentID: "native-" + name, Role: name, Epoch: 1, Authority: role.Ref()}
		state.RoleStops[name] = contribution.RoleStop{Authority: role, Epoch: 1, Acknowledged: true}
		state.NativeBoundaries[name] = contribution.NativeBoundary{Role: name, Epoch: 1, Closed: true, Bindings: []contribution.NativeBinding{binding}}
		state.NativeEffects[binding.IntentID] = contribution.NativeEffect{Binding: binding, State: "completed"}
	}
	before, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*contribution.Continuity, *orchestration.ContributionRecord)
	}{
		{"forged live role", func(*contribution.Continuity, *orchestration.ContributionRecord) {}},
		{"not accepted", func(_ *contribution.Continuity, r *orchestration.ContributionRecord) { r.State = "phase_boundary" }},
		{"wrong run", func(_ *contribution.Continuity, r *orchestration.ContributionRecord) { r.RunID = "other" }},
		{"wrong contract", func(_ *contribution.Continuity, r *orchestration.ContributionRecord) { r.Contract = "other" }},
		{"missing pass", func(_ *contribution.Continuity, r *orchestration.ContributionRecord) { r.Passes = nil }},
		{"failed check", func(_ *contribution.Continuity, r *orchestration.ContributionRecord) {
			r.Passes[0].Check.Outcome = "failed"
		}},
		{"candidate mismatch", func(st *contribution.Continuity, _ *orchestration.ContributionRecord) {
			st.CandidateEvidence.Digest = contribution.Digest([]byte("other"))
		}},
		{"unknown native", func(st *contribution.Continuity, _ *orchestration.ContributionRecord) {
			n := st.NativeEffects["native-builder"]
			n.State = "unknown"
			st.NativeEffects["native-builder"] = n
		}},
		{"unacknowledged child", func(st *contribution.Continuity, _ *orchestration.ContributionRecord) {
			delete(st.RoleStops, "builder")
		}},
		{"open boundary", func(st *contribution.Continuity, _ *orchestration.ContributionRecord) {
			b := st.NativeBoundaries["tester"]
			b.Closed = false
			st.NativeBoundaries["tester"] = b
		}},
		{"stale controller epoch", func(st *contribution.Continuity, _ *orchestration.ContributionRecord) {
			st.RoleEpochs["controller"] = 2
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(state)
			var st contribution.Continuity
			if err := json.Unmarshal(raw, &st); err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(record)
			var rec orchestration.ContributionRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&st, &rec)
			if s.acceptedContributionControllerLive(ctx, sub, c, st, rec) {
				t.Fatal("untrusted readback became live authority")
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if s.acceptedContributionControllerLive(cancelled, sub, c, state, record) {
		t.Fatal("cancelled readback accepted")
	}
	after, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("readback minted or changed authority")
	}
}
