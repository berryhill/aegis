package badger

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	badgerdb "github.com/dgraph-io/badger/v4"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestContributionRoleAuthorityCASAndBindingFence(t *testing.T) {
	s, c := continuityStore(t)
	ctx := context.Background()
	state, err := s.AppendContributionContinuity(ctx, continuityEvent(t, c, contribution.Continuity{}, "initialize"))
	if err != nil {
		t.Fatal(err)
	}
	ref := func(id string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: contribution.Digest([]byte(id))}
	}
	binding, err := queue.NewRuntimeBinding(queue.RuntimeBinding{BindingID: "original", QueueItem: ref(c.QueueItemID), Submission: ref(c.SubmissionID), OwnerAgent: c.Controller, Authority: ref("root-authority"), MandateID: "root-mandate", Runtime: "hermes-agent", BoundAt: c.AdmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := queue.NewProjection(queue.Projection{QueueItemID: c.QueueItemID, State: queue.StateQueued, LastTransitionID: "ready", AvailableAt: c.AdmittedAt, UpdatedAt: c.AdmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		b, _ := queue.MarshalRuntimeBinding(binding)
		if e := create(txn, key(familyQueueRuntimeBinding, c.QueueItemID), b); e != nil {
			return e
		}
		b, _ = json.Marshal(projection)
		return txn.Set(key(familyQueueProjection, c.QueueItemID), b)
	})
	if err != nil {
		t.Fatal(err)
	}
	event := func(kind, session string, r *contribution.RoleAuthority) contribution.ContinuityEvent {
		e := continuityEvent(t, c, state, kind)
		e.SessionID = session
		e.RoleAuthority = r
		e, err = contribution.SealContinuityEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	for _, role := range []string{"controller", "builder", "tester"} {
		session := "fresh-" + role
		state, err = s.AppendContributionContinuity(ctx, event("observation", session, nil))
		if err != nil {
			t.Fatal(err)
		}
		sealed, _ := contribution.SealedRole(c, role)
		r := contribution.RoleAuthority{Epoch: 1, SessionID: session, RuntimeSessionID: "runtime-" + role, Role: role, Binding: sealed, Authority: ref("authority-" + role), MandateID: "mandate-" + role, OriginalRuntimeBinding: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: binding.BindingID, Digest: binding.Digest}}
		wrong := r
		wrong.OriginalRuntimeBinding = ref("substitute")
		if _, e := s.AppendContributionContinuity(ctx, event("role-admitted", session, &wrong)); e == nil {
			t.Fatal("binding substituted")
		}
		e := event("role-admitted", session, &r)
		var wg sync.WaitGroup
		out := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := s.AppendContributionContinuity(ctx, e); out <- e }()
		}
		wg.Wait()
		close(out)
		wins := 0
		for e := range out {
			if e == nil {
				wins++
			}
		}
		if wins != 1 {
			t.Fatalf("CAS wins %d", wins)
		}
		state, _, err = s.GetContributionContinuity(ctx, c.QueueItemID)
		if err != nil {
			t.Fatal(err)
		}
		if _, e := s.AppendContributionContinuity(ctx, event("role-admitted", session, &r)); !errors.Is(e, contribution.ErrContinuityFenced) {
			t.Fatalf("replacement %v", e)
		}
	}
	if len(state.RoleAuthorities) != 3 || state.Used != (contribution.Counters{}) {
		t.Fatalf("lost roles or reset %+v", state)
	}
	// Real owned native child + exact private Wait proof, persisted in this Store.
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "native-claim", QueueItem: ref(c.QueueItemID), AttemptID: "native-attempt", WorkerID: "worker", Authority: ref("claim-authority"), ClaimedAt: c.AdmittedAt, ExpiresAt: c.AdmittedAt.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	projection.State = queue.StateClaimed
	projection.ActiveClaimID = claim.ClaimID
	projection.Attempts = 1
	projection, err = queue.NewProjection(projection)
	if err != nil {
		t.Fatal(err)
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		wire, _ := queue.MarshalClaim(claim)
		if e := txn.Set(key(familyClaim, claim.ClaimID), wire); e != nil {
			return e
		}
		wire, _ = json.Marshal(projection)
		return txn.Set(key(familyQueueProjection, c.QueueItemID), wire)
	})
	if err != nil {
		t.Fatal(err)
	}
	cp := continuityEvent(t, c, state, "checkpoint")
	cp.Stage = "build"
	cp.Candidate = "native-candidate"
	cp.Evidence = ref("native-candidate")
	cp, _ = contribution.SealContinuityEvent(cp)
	state, err = s.AppendContributionContinuity(ctx, cp)
	if err != nil {
		t.Fatal(err)
	}
	nativeRole := state.RoleAuthorities["builder"]
	cmd := exec.Command("true")
	nb := contribution.NativeBinding{IntentID: "native-one", RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: claim.ClaimID, AttemptID: claim.AttemptID, Role: "builder", Epoch: 1, Authority: nativeRole.Ref(), Candidate: state.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: hermes.NativeCommandDigest(cmd)}
	appendNative := func(kind, status string) error {
		e := continuityEvent(t, c, state, kind)
		e.SessionID = nativeRole.SessionID
		e.NativeEffect = &contribution.NativeEffect{Binding: nb, State: status}
		e, _ = contribution.SealContinuityEvent(e)
		var err error
		state, err = s.AppendContributionContinuity(ctx, e)
		return err
	}
	err = hermes.RunContributionNative(ctx, nb, cmd, func(context.Context) error { return appendNative("native-reserved", "reserved") }, func(_ context.Context, p hermes.NativeCompletion) error {
		if !p.Joined() || p.Binding() != nb || cmd.ProcessState == nil {
			t.Fatal("not exact owned Wait")
		}
		return appendNative("native-completed", "completed")
	})
	if err != nil {
		t.Fatal(err)
	}
	retainedNative, _, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || retainedNative.NativeEffects[nb.IntentID].State != "completed" {
		t.Fatalf("native durable proof: %v", err)
	}
	if err = appendNative("native-reserved", "reserved"); err == nil {
		t.Fatal("native intent replay ran twice")
	}
	state, _, err = s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	// A start failure is unknown, never a fabricated completion. It fences
	// further commands even though no live PID can be found.
	failed := exec.Command("/aegis-fixture-missing-native-command")
	nb.IntentID = "native-start-failed"
	nb.CommandDigest = hermes.NativeCommandDigest(failed)
	err = hermes.RunContributionNative(ctx, nb, failed, func(context.Context) error { return appendNative("native-reserved", "reserved") }, func(_ context.Context, p hermes.NativeCompletion) error {
		if p.Joined() {
			t.Fatal("start failure minted proof")
		}
		return appendNative("native-unknown", "unknown")
	})
	if err == nil {
		t.Fatal("missing executable succeeded")
	}
	retainedNative, _, err = s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || retainedNative.NativeEffects[nb.IntentID].State != "unknown" {
		t.Fatal("unknown not durable", err)
	}
	nb.IntentID = "native-after-unknown"
	if err = appendNative("native-reserved", "reserved"); !errors.Is(err, contribution.ErrNativeQuiescenceUnavailable) {
		t.Fatalf("unknown did not fence successor command: %v", err)
	}
	state, _, err = s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	// A role-process acknowledgement is NOT the native source/check join.
	builder := state.RoleAuthorities["builder"]
	stop := contribution.RoleStop{Authority: builder, Epoch: state.RoleEpochs["builder"], PID: 42, ProcessStart: "exact-start"}
	for _, kind := range []string{"role-stop-requested", "role-stop-acknowledged"} {
		if kind == "role-stop-acknowledged" {
			stop.Acknowledged = true
			stop.BootID = "controller-boot"
		}
		e := continuityEvent(t, c, state, kind)
		e.SessionID = builder.SessionID
		e.RoleStop = &stop
		e, err = contribution.SealContinuityEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		state, err = s.AppendContributionContinuity(ctx, e)
		if err != nil {
			t.Fatal(err)
		}
	}
	successor := builder
	successor.Epoch = state.RoleEpochs["builder"] + 1
	if _, err = s.AppendContributionContinuity(ctx, event("role-admitted", builder.SessionID, &successor)); !errors.Is(err, contribution.ErrNativeQuiescenceUnavailable) {
		t.Fatalf("role ack released native custody: %v", err)
	}
	retainedStop, _, err := s.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || retainedStop.Digest != state.Digest || retainedStop.RoleEpochs["builder"] != 1 {
		t.Fatal("failed successor changed epoch", err)
	}
	root := s.root
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retained, _, err := reopened.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || retained.Digest != state.Digest || len(retained.RoleAuthorities) != 3 || retained.Used != state.Used || retained.RoleEpochs["builder"] != state.RoleEpochs["builder"] || retained.RoleStops["builder"] != state.RoleStops["builder"] {
		t.Fatal("reopen reset role history", err)
	}
	got, err := reopened.GetQueueRuntimeBinding(ctx, c.QueueItemID)
	if err != nil || got != binding {
		t.Fatal("original binding changed", err)
	}
}
