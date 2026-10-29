package managergateway

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// Trusted immutable controller-policy fixture, never runtime PASS or #377 proof.
type nativeDeliveryPolicies struct {
	policy app.ContributionDeliveryPolicy
}

func (r *nativeDeliveryPolicies) ResolveContributionDeliveryPolicy(context.Context, reference.DigestRef) (app.ContributionDeliveryPolicy, error) {
	return r.policy, nil
}

// Reuses the actual authenticated manager/Queue/#376 accepted journey, rather
// than constructing another candidate or invoking the kernel directly.
func contributionDeliveryNativeBridge(t *testing.T, ctx context.Context, s *app.Service, subject core.Subject, key, controllerSession string, v orchestration.ContributionCandidate) {
	t.Helper()
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		t.Fatal(err)
	}
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: v.Digest}
	p := app.ContributionDeliveryPolicy{
		ControllerSession: controllerSession,
		Binding:           fleetbadger.ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: subject.PrincipalID, Key: key, Reference: ref, FenceEpoch: 1, RoleBinding: contribution.Digest([]byte("synthetic exact role binding")), Base: "main"},
		Loop:              c.Loop, Workspace: c.Worktree, Source: v.Source.Digest, Tree: v.CandidateTree, BaseHead: c.Base, CandidatePolicy: v.Policy, Toolchain: v.Toolchain, QueueAttempt: v.QueueAttempt,
		Actor: "fixture", Branch: "delivery/reviewed", Timestamp: "2026-10-25T00:00:00Z", Message: "Implement reviewed behavior\n",
		Author: delivery.Identity{Name: "Reviewed Author", Email: "author@example.invalid"}, Committer: delivery.Identity{Name: "Reviewed Controller", Email: "controller@example.invalid"}, AllowedFiles: []string{"value.go", "offset.go"},
		ProtectionPolicy: contribution.Digest([]byte("synthetic protection")), RequiredChecks: []string{"native"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash", PRTitle: "Reviewed fixture", PRBody: "Controlled local fixture", PR: 1,
	}
	resolver := &nativeDeliveryPolicies{policy: p}
	policyRef := func(p app.ContributionDeliveryPolicy) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed-delivery", Digest: p.Digest()}
	}
	gateCalls := 0
	// SYNTHETIC quiescence: exact real candidate checked, no claim of native Wait,
	// live claim admission, stop acknowledgement or productive #377 integration.
	gate := func(_ context.Context, got orchestration.ContributionCandidate) error {
		gateCalls++
		if !reflect.DeepEqual(got, v) {
			return delivery.ErrDenied
		}
		return nil
	}
	sourceFor := func(actor core.Subject, policy app.ContributionDeliveryPolicy) *app.ContributionDeliverySource {
		t.Helper()
		resolver.policy = policy
		src, e := app.NewQualificationContributionDeliverySource(s, actor, key, policyRef(policy), resolver, gate)
		if e != nil {
			t.Fatal(e)
		}
		return src
	}
	git := func(args ...string) string {
		t.Helper()
		b, e := exec.Command("git", append([]string{"-C", c.Worktree}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	beforeTree := git("ls-tree", "-r", v.CandidateTree)
	beforeHead := git("rev-parse", "HEAD")
	indexPath := git("rev-parse", "--path-format=absolute", "--git-path", "index")
	beforeIndex, indexErr := os.ReadFile(indexPath)
	if indexErr != nil {
		t.Fatal(indexErr)
	}
	beforeObjects := git("cat-file", "--batch-all-objects", "--batch-check=%(objectname)")
	beforeRefs := git("show-ref")
	called := 0
	var retained delivery.Plan
	canonicalDigest := p.Digest()
	src := sourceFor(subject, p)
	err = src.WithAccepted(ctx, ref, func(effectCtx context.Context, got delivery.Plan) error {
		called++
		if gateCalls != 1 {
			t.Fatalf("callback not between custody gates: %d", gateCalls)
		}
		if got.Reference != ref || !reflect.DeepEqual(got.Intake, c) || got.FenceEpoch != p.Binding.FenceEpoch || got.RoleBinding != p.Binding.RoleBinding || got.Policy != p.Digest() || got.Delivery != c.Effect || got.Repository != c.Repository || got.RepositoryID != c.Source.RepositoryID || got.Base != p.Binding.Base || got.BaseHead != c.Base || got.Branch != p.Branch || got.Actor != p.Actor || got.ExpectedOld != p.ExpectedOld || got.ProtectionPolicy != p.ProtectionPolicy || !reflect.DeepEqual(got.RequiredChecks, p.RequiredChecks) || !reflect.DeepEqual(got.RequiredReviewers, p.RequiredReviewers) || !reflect.DeepEqual(got.PostMergeChecks, p.PostMergeChecks) || got.MergeMethod != p.MergeMethod || got.PR != p.PR || got.PRTitle != p.PRTitle || got.PRBody != p.PRBody {
			t.Fatalf("full application projection drift: %+v", got)
		}
		if got.Commit.Tree != v.CandidateTree || got.Commit.Parent != v.Base || got.Head != got.Commit.Commit || got.Head == v.CandidateCommit || len(got.Objects) == 0 {
			t.Fatal("publication identity drift")
		}
		obj := got.Objects[len(got.Objects)-1]
		cmd := exec.Command("git", "-C", c.Worktree, "hash-object", "-t", "commit", "--stdin")
		cmd.Stdin = strings.NewReader(string(obj.Raw))
		id, hashErr := cmd.Output()
		wantMetadata := "tree " + v.CandidateTree + "\nparent " + v.Base + "\nauthor Reviewed Author <author@example.invalid> 1792886400 +0000\ncommitter Reviewed Controller <controller@example.invalid> 1792886400 +0000\n\n" + p.Message
		if hashErr != nil || strings.TrimSpace(string(id)) != got.Head || obj.OID != got.Head || string(obj.Raw) != wantMetadata || git("ls-tree", "-r", got.Commit.Tree) != beforeTree {
			t.Fatal("pure publication graph/metadata drift")
		}
		if exec.Command("git", "-C", c.Worktree, "cat-file", "-e", got.Head).Run() == nil {
			t.Fatal("unadmitted publication object exists")
		}
		for _, o := range got.Objects {
			if o.OID == v.CandidateCommit {
				t.Fatal("synthetic freeze commit published")
			}
		}
		// Reentrant application handoff cannot obtain this kernel-owned workspace.
		nested := false
		if e := src.WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error { nested = true; return nil }); e == nil || nested {
			t.Fatal("kernel custody absent during callback")
		}
		retained = got
		got.RequiredChecks[0] = "callback-check"
		got.RequiredReviewers[0] = "callback-reviewer"
		got.PostMergeChecks[0] = "callback-post"
		if resolver.policy.Digest() != canonicalDigest {
			t.Fatal("synchronous callback mutated resolver policy")
		}
		return nil
	})
	if err != nil || called != 1 || gateCalls != 2 {
		t.Fatalf("positive app callback called=%d gates=%d: %v", called, gateCalls, err)
	}
	done := make(chan struct{})
	go func() {
		retained.RequiredChecks[0] = "retained-check"
		retained.RequiredReviewers[0] = "retained-reviewer"
		retained.PostMergeChecks[0] = "retained-post"
		close(done)
	}()
	<-done
	if resolver.policy.Digest() != canonicalDigest {
		t.Fatal("retained Plan mutated resolver policy after return")
	}
	for range 2 {
		if e := src.WithAccepted(ctx, ref, func(_ context.Context, again delivery.Plan) error {
			if again.Head != retained.Head {
				t.Fatal("repeated projection drift")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
	afterIndex, indexErr := os.ReadFile(indexPath)
	if indexErr != nil || !reflect.DeepEqual(beforeIndex, afterIndex) || git("rev-parse", "HEAD") != beforeHead || git("ls-tree", "-r", v.CandidateTree) != beforeTree || git("show-ref") != beforeRefs || git("cat-file", "--batch-all-objects", "--batch-check=%(objectname)") != beforeObjects {
		t.Fatal("repeated projection modified objects/refs/source HEAD/index/tree")
	}
	for _, kind := range []string{"tree", "source", "queue-attempt", "candidate-policy", "owner", "loop", "workspace", "base", "epoch", "role"} {
		t.Run("delivery-deny-"+kind, func(t *testing.T) {
			bad := p
			switch kind {
			case "tree":
				bad.Tree = strings.Repeat("a", 40)
			case "source":
				bad.Source = contribution.Digest([]byte("wrong source"))
			case "queue-attempt":
				bad.QueueAttempt = "wrong-attempt"
			case "candidate-policy":
				bad.CandidatePolicy = contribution.Digest([]byte("wrong policy"))
			case "owner":
				bad.Binding.OwnerID = "other-owner"
			case "loop":
				bad.Loop.Digest = contribution.Digest([]byte("wrong loop"))
			case "workspace":
				bad.Workspace = filepath.Join(c.Worktree, "other")
			case "base":
				bad.BaseHead = strings.Repeat("b", 40)
			case "epoch":
				bad.Binding.FenceEpoch = 0
			case "role":
				bad.Binding.RoleBinding = ""
			}
			before := gateCalls
			invoked := false
			e := sourceFor(subject, bad).WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error { invoked = true; return nil })
			if !errors.Is(e, delivery.ErrDenied) || invoked || gateCalls <= before {
				t.Fatalf("%s did not exercise project denial gates=%d/%d callback=%v err=%v", kind, before, gateCalls, invoked, e)
			}
		})
	}
	resolver.policy = p
	if _, e := app.NewQualificationContributionDeliverySource(s, subject, key, policyRef(p), resolver, nil); !errors.Is(e, app.ErrContributionDeliveryUnavailable) {
		t.Fatal("nil gate accepted", e)
	}
	wrong := subject
	wrong.PrincipalID = "other-owner"
	if e := sourceFor(wrong, p).WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error { t.Fatal("wrong subject callback"); return nil }); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("wrong subject accepted", e)
	}
	src = sourceFor(subject, p)
	old, e := os.ReadFile(filepath.Join(c.Worktree, "value.go"))
	if e != nil {
		t.Fatal(e)
	}
	driftCalled := false
	e = src.WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error {
		driftCalled = true
		return os.WriteFile(filepath.Join(c.Worktree, "value.go"), []byte("drift"), 0600)
	})
	if x := os.WriteFile(filepath.Join(c.Worktree, "value.go"), old, 0600); x != nil {
		t.Fatal(x)
	}
	if !errors.Is(e, delivery.ErrDenied) || !driftCalled {
		t.Fatal("callback source drift missed", e)
	}
	src = sourceFor(subject, p)
	driftCalled = false
	e = src.WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error {
		driftCalled = true
		resolver.policy.Actor = "policy drift"
		return nil
	})
	if !errors.Is(e, delivery.ErrDenied) || !driftCalled {
		t.Fatal("callback policy drift missed", e)
	}
	src = sourceFor(subject, p)
	driftCalled = false
	e = src.WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error {
		driftCalled = true
		return s.ControlContributionAs(ctx, subject, "delivery-fixture", key, true, "local-only")
	})
	if !errors.Is(e, delivery.ErrDenied) || !driftCalled {
		t.Fatal("current control drift missed", e)
	}
	if e = src.WithAccepted(ctx, ref, func(context.Context, delivery.Plan) error { t.Fatal("stopped callback"); return nil }); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("current stop accepted", e)
	}
	t.Log("actual app Source.WithAccepted full Plan under kernel custody; trusted immutable policy + SYNTHETIC exact-candidate quiescence only; NOT real #377/provider/remote/conductor acceptance")
}
