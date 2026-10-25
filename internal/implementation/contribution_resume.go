package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// Caller holds the existing workspace custody lock. This is continuation, not
// authority renewal or reconciliation of unknown effects. Noncooperating host
// writers remain outside the advisory-lock guarantee.
func (e *ManagedContribution) resumeBoundary(ctx context.Context, r ContributionRecord, run, contract, attempt, base string, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy) error {
	bad := errors.New("exact quiescent contribution phase boundary required; reservation held")
	identity, _ := json.Marshal(c)
	owner, err := e.DB.Get([]byte("contribution/workspace/" + digest([]byte(c.Workspace))))
	if err != nil || string(owner) != run || r.RunID != run || r.Contract != contract || r.QueueAttempt != attempt || r.Base != base || r.Workspace != c.Workspace || r.Implementation != digest(identity) || contributionPolicyDigest(r.Policy) != contributionPolicyDigest(p) || r.State != "phase_boundary" || r.Expected == nil || r.Baseline.Outcome != "failed" || int(r.Consumed) != len(r.Passes) || r.Consumed >= c.MaxPasses {
		return bad
	}
	if err = e.admit(ctx, "worktree-resume"); err != nil {
		return err
	}
	origin, err := e.git(ctx, p, p.RepositoryPath, "remote", "get-url", "origin")
	if err != nil || e.Repository == "" || strings.TrimSpace(string(origin)) != "https://github.com/"+e.Repository+".git" {
		return bad
	}
	source, err := Inventory(ctx, c.Workspace)
	if err != nil || source.Digest != r.BoundarySource {
		return bad
	}
	index, err := e.git(ctx, p, c.Workspace, "ls-files", "--stage", "-z")
	if err != nil || digest(index) != r.BoundaryIndex {
		return bad
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"rev-parse", "HEAD"}, base},
		{[]string{"symbolic-ref", "--short", "HEAD"}, "contribution-" + strings.TrimPrefix(digest([]byte(run)), "sha256:")},
		{[]string{"rev-parse", "--path-format=absolute", "--git-common-dir"}, ""},
	} {
		actual, err := e.git(ctx, p, c.Workspace, check.args...)
		if err != nil {
			return bad
		}
		want := check.want
		if want == "" {
			b, x := e.git(ctx, p, p.RepositoryPath, check.args...)
			if x != nil {
				return bad
			}
			want = strings.TrimSpace(string(b))
		}
		if strings.TrimSpace(string(actual)) != want {
			return bad
		}
	}
	if len(r.Passes) > 0 {
		return e.revalidate(ctx, c, p, r.Passes[len(r.Passes)-1].Candidate)
	}
	return verifyContext(ctx, c, r.Expected)
}

// TesterEvidence deliberately excludes source bytes and path names, which are
// private controller inputs, not an implicitly admitted model context. Native
// checks still execute the complete frozen inventory inside controller custody.
func (v ContributionCandidate) TesterEvidence() ContributionCandidate {
	v.Source = SourceInventory{Schema: v.Source.Schema, Digest: v.Source.Digest}
	return v
}
