package managergateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// configureJointCancellationDelivery supplies real, empty, isolated custody.
// It configures prerequisites only: no credential, grant, controller execution or
// remote permission is fabricated; the tracked policy below still holds/denies.
func configureJointCancellationDelivery(t *testing.T, ctx context.Context, s *app.Service) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	must(os.Chmod(root, 0700))
	key := filepath.Join(root, "key")
	must(credentials.CreateHostKey(key, "joint-cancellation"))
	custody, err := credentials.LoadFileCustodian(key)
	must(err)
	t.Cleanup(custody.Close)
	repository, err := credentialbolt.Open(ctx, filepath.Join(root, "credentials.db"), "joint-cancellation", custody)
	must(err)
	t.Cleanup(func() { must(repository.Close()) })
	s.CredentialAuthority = credentials.NewAuthority(repository, custody)
	s.Config.ContributionDelivery = &config.ContributionDelivery{Convention: "contribution-publication.v1", Actor: "fixture", Author: config.ContributionGitIdentity{Name: "Author", Email: "author@example.invalid"}, Committer: config.ContributionGitIdentity{Name: "Controller", Email: "controller@example.invalid"}, ProtectionPolicy: "exact-protection", RequiredChecks: []string{"native"}, RequiredReviewers: []string{"independent"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash"}
}

// No write authority is manufactured: the actual tracked typed entry is held
// before policy admission, then explicitly denied. Historical policies, custody,
// privacy and the canonical GitHub client must remain unused.
type jointCancelDeny struct{ historical, credentials, privacy, authority atomic.Int32 }

func (d *jointCancelDeny) ResolveContributionDeliveryPolicyDigest(context.Context, string) (app.ContributionDeliveryPolicy, error) {
	d.historical.Add(1)
	return app.ContributionDeliveryPolicy{}, delivery.ErrDenied
}
func (d *jointCancelDeny) Use(context.Context, delivery.Intent, func([]byte) error) error {
	d.credentials.Add(1)
	return delivery.ErrDenied
}
func (d *jointCancelDeny) Review(context.Context, delivery.Intent, delivery.Plan) error {
	d.privacy.Add(1)
	return delivery.ErrDenied
}
func (d *jointCancelDeny) Admit(context.Context, delivery.Intent, bool) error {
	d.authority.Add(1)
	return delivery.ErrDenied
}

type jointCancelHold struct {
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
}

func (h *jointCancelHold) ResolveContributionDeliveryPolicy(ctx context.Context, _ reference.DigestRef) (app.ContributionDeliveryPolicy, error) {
	h.calls.Add(1)
	h.entered <- ctx
	<-h.release
	return app.ContributionDeliveryPolicy{}, delivery.ErrDenied
}

func contributionDeliveryJointNativeCancellation(t *testing.T, ctx context.Context, s *app.Service, sub core.Subject, key, session string, v orchestration.ContributionCandidate) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	c, before, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	must(err)
	if c.Effect != "verified-protected-base-merge" || before.CandidateEvidence.Digest != v.Digest {
		t.Fatal("nonlocal sealed candidate missing")
	}
	for _, name := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(before, name) {
			t.Fatal("missing native Wait", name)
		}
	}
	_, rootBefore, err := s.ReadContributionAs(ctx, sub, key)
	must(err)
	authorities, err := s.Authority.ListAuthorityContexts(ctx)
	must(err)
	defer func() {
		for _, a := range authorities {
			_ = s.RevokeSession(context.Background(), a.SessionID, "joint cancellation cleanup")
		}
	}()
	deny := &jointCancelDeny{}
	github, err := delivery.NewGitHub("https://api.github.com", deny, deny)
	must(err)
	owner, err := app.InstallContributionDeliveryPublicationOwner(s, deny, github, s.CurrentContributionDeliverySubject)
	must(err)
	role := before.RoleAuthorities["controller"]
	binding := app.ContributionPublicationBinding{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: before.CandidateEvidence, Controller: role.Ref(), Epoch: role.Epoch}
	bad := binding
	bad.Epoch++
	if owner.QuiesceContributionPublication(ctx, bad) == nil {
		t.Fatal("invalid epoch accepted")
	}
	hold := &jointCancelHold{entered: make(chan context.Context, 1), release: make(chan struct{})}
	released := false
	defer func() {
		if !released {
			close(hold.release)
		}
	}()
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: v.Digest}
	policyRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "unadmitted-policy", Digest: contribution.Digest([]byte("explicitly denied policy"))}
	result := make(chan error, 1)
	go func() {
		_, e := s.RunContributionDeliveryAs(ctx, sub, key, ref, policyRef, hold, deny, github, nil, nil)
		result <- e
	}()
	var stopCtx context.Context
	select {
	case stopCtx = <-hold.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual tracked entry not reached")
	}
	// Preserve the live request binding supplied by the real Run entry; the
	// publication owner must not use a fixed-subject authentication callback.
	stopped := make(chan error, 1)
	go func() { stopped <- s.StopContributionInSessionAs(stopCtx, sub, session, key) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		control, e := s.ReadContributionControlAs(ctx, sub, key)
		must(e)
		if control.Stop {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("sticky stop missing")
		case <-time.After(time.Millisecond):
		}
	}
	_, pending, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	must(err)
	_, rootPending, err := s.ReadContributionAs(ctx, sub, key)
	must(err)
	if !pending.CancelRequested || pending.CancelAcknowledged || rootPending.Projection.State == queue.StateCancelled {
		t.Fatal("active publication prematurely cancelled Queue")
	}
	for _, kind := range []string{"epoch", "contract", "candidate", "controller", "run", "queue"} {
		wrong := binding
		switch kind {
		case "epoch":
			wrong.Epoch++
		case "contract":
			wrong.Contract.Digest = contribution.Digest([]byte("foreign contract"))
		case "candidate":
			wrong.Candidate.Digest = contribution.Digest([]byte("foreign candidate"))
		case "controller":
			wrong.Controller.Digest = contribution.Digest([]byte("foreign controller"))
		case "run":
			wrong.RunID = "foreign-run"
		case "queue":
			wrong.QueueItemID = "foreign-queue"
		}
		if owner.QuiesceContributionPublication(stopCtx, wrong) == nil {
			t.Fatal("invalid binding accepted", kind)
		}
	}
	select {
	case e := <-stopped:
		t.Fatalf("stop escaped active tracker: %v", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(hold.release)
	released = true
	if e := <-result; !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("pre-effect denial", e)
	}
	select {
	case e := <-stopped:
		must(e)
	case <-time.After(20 * time.Second):
		t.Fatal("stop did not join tracked exit")
	}
	_, after, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	must(err)
	_, rootAfter, err := s.ReadContributionAs(ctx, sub, key)
	must(err)
	if !after.CancelAcknowledged || rootAfter.Projection.State != queue.StateCancelled {
		t.Fatalf("real managed/publication join missing: %+v queue=%s", after, rootAfter.Projection.State)
	}
	if after.Used != before.Used || !reflect.DeepEqual(after.RoleAuthorities, before.RoleAuthorities) || !reflect.DeepEqual(after.RoleEpochs, before.RoleEpochs) || after.CandidateEvidence != before.CandidateEvidence || !reflect.DeepEqual(rootBefore.Attempts, rootAfter.Attempts) || !reflect.DeepEqual(rootBefore.Claims, rootAfter.Claims) || !reflect.DeepEqual(rootBefore.RuntimeAuthorityBinding, rootAfter.RuntimeAuthorityBinding) {
		t.Fatal("stop replaced original identity, claim, authority or budget")
	}
	absent, err := s.FleetRepository.(*fleetbadger.Store).ContributionDeliveryAuthoritativeAbsence(ctx, sub.PrincipalID, key)
	must(err)
	if !absent {
		t.Fatal("canonical absence missing")
	}
	finalAuthorities, err := s.Authority.ListAuthorityContexts(ctx)
	must(err)
	if len(finalAuthorities) != len(authorities) {
		t.Fatal("new authority minted")
	}
	if hold.calls.Load() != 1 || deny.historical.Load() != 0 || deny.credentials.Load() != 0 || deny.privacy.Load() != 0 || deny.authority.Load() != 0 {
		t.Fatal("unexpected admission, credential or historical policy use")
	}
	t.Log("NONLOCAL managed/native Wait + retained run.done + actual installed publication tracker/drain + canonical absence => CancelAcknowledged/Queue cancelled; no remote write admission or HTTP (credential callback unreachable)")
}
