package contribution

import (
	"fmt"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestDeliveryReadinessClosedReservation(t *testing.T) {
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate", Digest: Digest([]byte("candidate"))}
	role := RoleAuthority{Role: "controller", Epoch: 7, SessionID: "actual-controller", RuntimeSessionID: "native-runtime", MandateID: "mandate", Authority: ref, OriginalRuntimeBinding: ref}
	state := Continuity{Sequence: 3, Candidate: "candidate", CandidateEvidence: ref, RoleAuthorities: map[string]RoleAuthority{"controller": role}, RoleEpochs: map[string]uint64{"controller": 7}, Limits: Counters{InfrastructureRechecks: 2}}
	event := func(st Continuity) ContinuityEvent {
		r := role
		return ContinuityEvent{Kind: "delivery-readiness-reserved", SessionID: role.SessionID, RoleAuthority: &r, Counter: "infrastructure-recheck", Candidate: st.Candidate, Evidence: st.CandidateEvidence, Stage: "checks", OperationID: fmt.Sprintf("readiness/%s/%d", strings.TrimPrefix(Digest([]byte("intent")), "sha256:"), st.Used.InfrastructureRechecks+1)}
	}
	next := state
	if err := applyDeliveryReadiness(state, event(state), &next); err != nil || next.Used.InfrastructureRechecks != 1 {
		t.Fatalf("positive: %+v %v", next, err)
	}
	for _, name := range []string{"missing-role", "foreign-role", "stale-epoch", "session", "binding", "runtime", "mandate", "original", "candidate", "evidence", "counter", "stage", "malformed", "short-digest", "upper-digest", "nonnext", "duplicate", "pending-stop", "stopped", "missing-current", "zero-epoch", "exhausted", "uninitialized", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			st := state
			e := event(st)
			r := *e.RoleAuthority
			e.RoleAuthority = &r
			switch name {
			case "missing-role":
				e.RoleAuthority = nil
			case "foreign-role":
				r.Role = "builder"
			case "stale-epoch":
				r.Epoch--
			case "session":
				e.SessionID = "intake"
			case "binding":
				r.Binding.Runtime.Runtime = "foreign"
			case "runtime":
				r.RuntimeSessionID = "foreign"
			case "mandate":
				r.MandateID = "foreign"
			case "original":
				r.OriginalRuntimeBinding = reference.DigestRef{}
			case "candidate":
				e.Candidate = "foreign"
			case "evidence":
				e.Evidence = reference.DigestRef{}
			case "counter":
				e.Counter = "runtime-turn"
			case "stage":
				e.Stage = "publish"
			case "malformed":
				e.OperationID = "readiness/bad/1"
			case "short-digest":
				e.OperationID = "readiness/abcdef/1"
			case "upper-digest":
				e.OperationID = strings.ToUpper(e.OperationID)
			case "nonnext":
				e.OperationID = strings.TrimSuffix(e.OperationID, "1") + "2"
			case "duplicate":
				st.Reservations = map[string]string{e.OperationID: e.Counter}
			case "pending-stop":
				st.RoleStops = map[string]RoleStop{"controller": {Authority: role}}
			case "stopped":
				st.RoleStops = map[string]RoleStop{"controller": {Authority: role, Acknowledged: true}}
			case "missing-current":
				st.RoleAuthorities = nil
			case "zero-epoch":
				r.Epoch = 0
			case "exhausted":
				st.Used.InfrastructureRechecks = 2
			case "uninitialized":
				st.Sequence = 0
			case "cancelled":
				st.CancelRequested = true
			}
			n := st
			if err := applyDeliveryReadiness(st, e, &n); err == nil {
				t.Fatal("admitted")
			}
			if n.Used != st.Used {
				t.Fatal("charged denial")
			}
		})
	}
}
