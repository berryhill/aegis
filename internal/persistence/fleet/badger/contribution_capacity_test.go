package badger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
	"testing"
)

func TestContributionContinuitySafetyCapacity(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	var state contribution.Continuity
	// Efficiently seed legitimate, fully sealed/reduced history, not a fake head.
	err := s.update(ctx, func(txn *badgerdb.Txn) error {
		for i := 0; i < contributionContinuityMax-contributionContinuitySafetyTail; i++ {
			kind := "checkpoint"
			if i == 0 {
				kind = "initialize"
			}
			e := continuityEvent(t, c, state, kind)
			if kind == "checkpoint" {
				e.Stage = "build"
				e.Candidate = "candidate-one"
				e.Evidence = reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate", Digest: contribution.Digest([]byte("candidate"))}
				e, _ = contribution.SealContinuityEvent(e)
			}
			if i >= 1 && i <= 6 {
				role := []string{"controller", "builder", "tester"}[(i-1)/2]
				e.Stage = ""
				e.Candidate = ""
				e.Evidence = reference.DigestRef{}
				e.SessionID = "session-" + role
				e.Kind = "observation"
				if i%2 == 0 {
					binding, _ := contribution.SealedRole(c, role)
					ref := func(id string) reference.DigestRef {
						return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: contribution.Digest([]byte(id))}
					}
					e.Kind = "role-admitted"
					e.RoleAuthority = &contribution.RoleAuthority{Epoch: 1, SessionID: e.SessionID, RuntimeSessionID: "runtime-" + role, Role: role, Binding: binding, Authority: ref("auth-" + role), MandateID: "mandate-" + role, OriginalRuntimeBinding: ref("root")}
				}
				e, _ = contribution.SealContinuityEvent(e)
			}
			if i == 16 {
				r := state.RoleAuthorities["builder"]
				e.Kind = "native-reserved"
				e.Stage = ""
				e.Candidate = ""
				e.Evidence = reference.DigestRef{}
				e.SessionID = r.SessionID
				e.NativeEffect = &contribution.NativeEffect{State: "reserved", Binding: contribution.NativeBinding{IntentID: "pending-native", RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: "claim", AttemptID: "attempt", Role: "builder", Epoch: r.Epoch, Authority: r.Ref(), Candidate: state.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: contribution.Digest([]byte("command"))}}
				e, _ = contribution.SealContinuityEvent(e)
			}
			if i == 9 || (i >= 10 && i <= 15) {
				role := "builder"
				if i >= 10 && i <= 12 {
					role = "controller"
				}
				if i >= 13 {
					role = "tester"
				}
				r := state.RoleAuthorities[role]
				e.Kind, e.Stage, e.Candidate, e.Evidence, e.SessionID = "native-reserved", "", "", reference.DigestRef{}, r.SessionID
				binding := contribution.NativeBinding{IntentID: "native-" + role, RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: "claim", AttemptID: "attempt", Role: role, Epoch: r.Epoch, Authority: r.Ref(), Candidate: state.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: contribution.Digest([]byte("command"))}
				if i == 9 {
					binding.IntentID = "pending-native"
				}
				if i == 11 || i == 14 {
					e.Kind = "native-completed"
				}
				if i == 9 || i == 12 || i == 15 {
					e.Kind = "native-boundary-open"
					e.NativeBoundary = &contribution.NativeBoundary{Role: role, Epoch: r.Epoch, Bindings: []contribution.NativeBinding{binding}}
				} else {
					e.NativeEffect = &contribution.NativeEffect{State: "reserved", Binding: binding}
					if e.Kind == "native-completed" {
						e.NativeEffect.State = "completed"
					}
				}
				e, _ = contribution.SealContinuityEvent(e)
			}
			var err error
			state, err = contribution.ApplyContinuity(c, state, e)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(e)
			if err = txn.Set(key(familyContributionContinuity, c.QueueItemID, fmt.Sprint(e.Sequence)), b); err != nil {
				return err
			}
			if err = txn.Set(key(familyContributionContinuity, c.QueueItemID, "head"), b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e := continuityEvent(t, c, state, "checkpoint")
	e.Stage = "review"
	e.Evidence = reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate", Digest: contribution.Digest([]byte("candidate"))}
	e, _ = contribution.SealContinuityEvent(e)
	if _, err = s.AppendContributionContinuity(ctx, e); !errors.Is(err, contribution.ErrContinuityExhausted) {
		t.Fatalf("ordinary consumed safety tail: %v", err)
	}
	pending := state.NativeEffects["pending-native"]
	pending.State = "completed"
	completed := continuityEvent(t, c, state, "native-completed")
	completed.SessionID = state.RoleAuthorities["builder"].SessionID
	completed.NativeEffect = &pending
	completed, _ = contribution.SealContinuityEvent(completed)
	state, err = s.AppendContributionContinuity(ctx, completed)
	if err != nil {
		t.Fatal("pending completion consumed safety", err)
	}
	state, err = s.AppendContributionContinuity(ctx, continuityEvent(t, c, state, "cancel-requested"))
	if err != nil {
		t.Fatal(err)
	}
	got, events, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || !got.CancelRequested || len(events) != contributionContinuityMax-contributionContinuitySafetyTail+2 {
		t.Fatalf("cancel reconstruction %+v %v", got, err)
	}
	for _, role := range []string{"controller", "builder", "tester"} {
		boundary := state.NativeBoundaries[role]
		boundary.Closed = true
		closed := continuityEvent(t, c, state, "native-boundary-closed")
		closed.SessionID = state.RoleAuthorities[role].SessionID
		closed.NativeBoundary = &boundary
		closed, _ = contribution.SealContinuityEvent(closed)
		state, err = s.AppendContributionContinuity(ctx, closed)
		if err != nil {
			t.Fatal("boundary close safety", err)
		}
		stop := contribution.RoleStop{Authority: state.RoleAuthorities[role], Epoch: 1, PID: 42, ProcessStart: "exact-start"}
		for _, kind := range []string{"role-stop-requested", "role-stop-acknowledged"} {
			if kind == "role-stop-acknowledged" {
				stop.Acknowledged = true
				stop.BootID = "boot"
			}
			e := continuityEvent(t, c, state, kind)
			e.SessionID = stop.Authority.SessionID
			e.RoleStop = &stop
			e, _ = contribution.SealContinuityEvent(e)
			state, err = s.AppendContributionContinuity(ctx, e)
			if err != nil {
				t.Fatal(role, kind, err)
			}
		}
	}
	state, err = s.AppendContributionContinuity(ctx, continuityEvent(t, c, state, "cancel-acknowledged"))
	if err != nil || !state.CancelAcknowledged {
		t.Fatal("global safety ack", err)
	}
	got, events, err = s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || len(events) != 4096 || len(got.RoleStops) != 3 || got.Used != state.Used {
		t.Fatal("safety history", err)
	}
	if _, err = s.AppendContributionContinuity(ctx, continuityEvent(t, c, state, "cancel-requested")); !errors.Is(err, contribution.ErrContinuityExhausted) {
		t.Fatal("hard max", err)
	}
	control, err := s.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || !control.Stop {
		t.Fatal("cancel lost control", err)
	}
}
