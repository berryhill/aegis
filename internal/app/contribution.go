package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// SelectContributionLoopAs resolves selection once; callers keep this exact
// immutable reference in the authenticated server-side manager session.
func (s *Service) SelectContributionLoopAs(ctx context.Context, subject core.Subject, id string) (reference.RevisionRef, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return reference.RevisionRef{}, err
	}
	views, err := s.ListLoopsAs(ctx, subject)
	if err != nil {
		return reference.RevisionRef{}, err
	}
	for _, v := range views {
		r := v.Revision
		if r.LoopID == id && r.SchemaVersion == loop.ContributionRevisionSchemaVersion && v.Lifecycle.State == loop.LifecycleActive && v.Lifecycle.ActiveDigest == r.Digest && v.Lifecycle.ActiveRevision == r.Revision {
			if err = s.validateContributionAgents(ctx, subject, r); err != nil {
				return reference.RevisionRef{}, err
			}
			return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: r.Revision, Digest: r.Digest}, nil
		}
	}
	return reference.RevisionRef{}, errors.New("active published contribution Loop required")
}
func (s *Service) validateContributionAgents(ctx context.Context, subject core.Subject, r loop.LoopRevision) error {
	if r.Contribution == nil || loop.ValidateRevision(r).Outcome != loop.ValidationValid {
		return ErrDenied
	}
	for i, ref := range []reference.RevisionRef{r.Contribution.Controller, r.Contribution.Builder, r.Contribution.Tester} {
		a, e := s.FleetRepository.LatestAgentRevision(ctx, ref.ID)
		if e != nil || a.Revision != ref.Revision || a.Digest != ref.Digest || a.Lifecycle != registry.LifecycleEnabled || a.Ownership.OwnerID != subject.PrincipalID {
			return ErrDenied
		}
		if ch, chErr := s.GetCharter(a.Charter.ID, a.Charter.Revision); chErr == nil && ch.Digest == a.Charter.Digest {
			roleName := []string{"controller", "builder", "tester"}[i]
			reqProvider, reqModel := r.Contribution.RoleLane(roleName)
			prepProvider, prepModel := r.Contribution.RoleLane("prepare")
			hasAuthorizedLane := false
			for _, st := range ch.Charter.Stanzas {
				if !st.Enabled {
					continue
				}
				if _, laneErr := st.Hermes.ResolveLane(reqProvider, reqModel); laneErr != nil {
					continue
				}
				if i == 1 && r.Contribution.Engineering != nil {
					if _, laneErr := st.Hermes.ResolveLane(prepProvider, prepModel); laneErr != nil {
						continue
					}
				}
				hasAuthorizedLane = true
				break
			}
			if !hasAuthorizedLane {
				return ErrDenied
			}
		}
	}
	return nil
}

type ContributionSourceReader interface {
	Read(context.Context, contribution.IssueURL) (contribution.Source, error)
}

var ErrContributionInputBound = contribution.ErrInputBound

func ContributionSourceBoundMessage(err error) string {
	for _, bound := range []error{contribution.ErrSourceResponseBound, contribution.ErrSourceIssueBound, contribution.ErrSourceDiscussionBound} {
		if errors.Is(err, bound) {
			return bound.Error()
		}
	}
	return ""
}

type contributionReader = ContributionSourceReader

func (s *Service) ReadContributionAs(ctx context.Context, subject core.Subject, key string) (contribution.Contract, QueueExecutionView, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return contribution.Contract{}, QueueExecutionView{}, err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionRepository)
	if !ok || !contribution.ValidKey(key) {
		return contribution.Contract{}, QueueExecutionView{}, ErrDenied
	}
	c, e := repo.GetContribution(ctx, subject.PrincipalID, key)
	if e != nil {
		return c, QueueExecutionView{}, e
	}
	view, e := s.GetQueueItemAs(ctx, subject, c.QueueItemID)
	if e == nil && (view.Item.GraphRunID != c.RunID || view.Item.Submission.ID != c.SubmissionID) {
		e = fleet.ErrCorrupt
	}
	return c, view, e
}
func (s *Service) StopContributionAs(ctx context.Context, subject core.Subject, key string) error {
	return ErrDenied // Manager controls require current authenticated session provenance.
}
func (s *Service) StopContributionInSessionAs(ctx context.Context, subject core.Subject, session, key string) error {
	return s.cancelContributionInSession(ctx, subject, session, key)
}

// AdmitContributionAs is called only with the authenticated gateway's held
// session and selected revision. IDs, workspace and actor refs are server-owned.
func (s *Service) AdmitContributionAs(ctx context.Context, subject core.Subject, session string, selected reference.RevisionRef, url, key, scope string) (contribution.Contract, error) {
	reader := s.ContributionReader
	if reader == nil {
		reader = contribution.Reader{}
	}
	return s.admitContributionAs(ctx, subject, session, selected, url, key, scope, reader)
}
func (s *Service) admitContributionAs(ctx context.Context, subject core.Subject, session string, selected reference.RevisionRef, url, key, scope string, reader contributionReader) (contribution.Contract, error) {
	s.loopRunMu.Lock()
	defer s.loopRunMu.Unlock()
	if err := s.requireFleetPrincipal(subject); err != nil {
		return contribution.Contract{}, err
	}
	if session == "" || selected.Validate() != nil || !contribution.ValidKey(key) || len(scope) > 4096 {
		return contribution.Contract{}, ErrDenied
	}
	issue, err := contribution.ParseIssueURL(url)
	if err != nil {
		return contribution.Contract{}, err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionRepository)
	if !ok {
		return contribution.Contract{}, errors.New("contribution custody unavailable")
	}
	payload := contribution.Payload(selected, url, scope)
	// Reconcile the durable original before network reads or fresh ID allocation.
	if prior, e := repo.GetContribution(ctx, subject.PrincipalID, key); e == nil {
		if prior.PayloadDigest != payload {
			return contribution.Contract{}, fleet.ErrConflict
		}
		item, e := s.FleetRepository.GetQueueItem(ctx, prior.QueueItemID)
		if e != nil || item.GraphRunID != prior.RunID || item.Submission.ID != prior.SubmissionID {
			return contribution.Contract{}, fleet.ErrCorrupt
		}
		sub, e := s.FleetRepository.GetSubmission(ctx, prior.SubmissionID)
		if e != nil || sub.OwnerID != prior.OwnerID {
			return contribution.Contract{}, fleet.ErrCorrupt
		}
		// This is readback of the original invocation, not re-admission or restart.
		if e := s.carryEngineeringPreparation(ctx, prior); e != nil {
			return contribution.Contract{}, e
		}
		return prior, nil
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return contribution.Contract{}, e
	}
	view, err := s.GetLoopViewAs(ctx, subject, selected.ID, selected.Revision)
	if err != nil || view.Revision.Digest != selected.Digest || view.Lifecycle.State != loop.LifecycleActive || view.Lifecycle.ActiveDigest != selected.Digest {
		return contribution.Contract{}, ErrDenied
	}
	r := view.Revision
	if err = s.validateContributionAgents(ctx, subject, r); err != nil {
		return contribution.Contract{}, err
	}
	p := r.Contribution
	effect, err := contribution.RestrictDelivery(p.Effect, scope)
	if err != nil {
		return contribution.Contract{}, err
	}
	roles := []contribution.RoleBinding{}
	for _, ref := range []reference.RevisionRef{p.Controller, p.Builder, p.Tester} {
		a, e := s.FleetRepository.LatestAgentRevision(ctx, ref.ID)
		if e != nil || a.Revision != ref.Revision || a.Digest != ref.Digest || a.Lifecycle != registry.LifecycleEnabled {
			return contribution.Contract{}, ErrDenied
		}
		roles = append(roles, contribution.RoleBinding{Agent: ref, Charter: a.Charter, Runtime: a.Runtime})
	}
	if p.ReadAction != contribution.ReadIssueAction || p.Repository != issue.Owner+"/"+issue.Repository {
		return contribution.Contract{}, ErrDenied
	}
	// Same-intent preparation resolves original source and identity before another read.
	var retainedIntent contributionEngineeringIntent
	if p.Engineering != nil {
		if e := s.Store.Load("contribution-engineering-intents", engineeringIntentID(subject.PrincipalID, key), &retainedIntent); e == nil {
			if retainedIntent.Contract.PayloadDigest != payload {
				return contribution.Contract{}, ErrConflict
			}
			reader = retainedEngineeringReader{retainedIntent.Contract.Source}
		} else if !errors.Is(e, os.ErrNotExist) {
			return contribution.Contract{}, e
		}
	}
	source, err := reader.Read(ctx, issue)
	if err != nil {
		return contribution.Contract{}, err
	}
	if source.RepositoryID != p.RepositoryID || source.URL != issue || source.DiscussionTruncated {
		return contribution.Contract{}, ErrDenied
	}
	var template loop.VerifiedImplementation
	for _, step := range r.Steps {
		if step.Implementation != nil {
			template = *step.Implementation
		}
	}
	templateDigest, err := template.Digest()
	if err != nil {
		return contribution.Contract{}, err
	}
	material := make([]byte, 16)
	if _, err = rand.Read(material); err != nil {
		return contribution.Contract{}, err
	}
	id := hex.EncodeToString(material)
	now := s.Now()
	pending := contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: subject.PrincipalID, SessionID: session, Key: key, PayloadDigest: payload, QueueItemID: "contribution-item-" + id, SubmissionID: "contribution-sub-" + id, RunID: "contribution-run-" + id, Loop: selected, Controller: p.Controller, Builder: p.Builder, Tester: p.Tester, Source: source, NarrowerScope: scope, Outcome: template.Task, Acceptance: template.Acceptance, Invariants: p.Invariants, Exclusions: p.Exclusions, Repository: p.Repository, Base: p.Base, Worktree: template.Workspace, Effect: effect, Roles: roles, TemplateDigest: templateDigest, CandidateBudget: template.MaxPasses, AdmittedAt: now, Deadline: now.Add(time.Duration(p.DeadlineSeconds) * time.Second)}
	if p.Engineering != nil {
		pending, err = s.prepareContributionEngineering(ctx, subject, pending, r, template)
		if err != nil {
			return contribution.Contract{}, err
		}
		id = strings.TrimPrefix(pending.RunID, "contribution-run-")
		// Serialize the final control read through Queue custody. A stop racing
		// this boundary must land either here or in the ordinary Queue controls.
		s.contributionEngineeringMu.Lock()
		defer s.contributionEngineeringMu.Unlock()
		var intent contributionEngineeringIntent
		eid := engineeringIntentID(subject.PrincipalID, key)
		if e := s.Store.Load("contribution-engineering-intents", eid, &intent); e != nil {
			return contribution.Contract{}, e
		}
		status, e := s.engineeringStatus(eid, intent)
		if e != nil || status.Control.Stop {
			return contribution.Contract{}, ErrDenied
		}
		pending, e = s.applyEngineeringDelivery(pending, status.Control.Delivery)
		if e != nil {
			return contribution.Contract{}, e
		}
	}
	c, err := contribution.Seal(pending)
	if err != nil {
		return contribution.Contract{}, err
	}
	bound, err := contribution.BindLoop(r, c)
	if err != nil {
		return contribution.Contract{}, err
	}
	if err = orchestration.ValidateContributionIntakeInput(ctx, bound, c, r.Contribution.Managed); err != nil {
		return contribution.Contract{}, err
	}
	inputs := []graph.NormalizedInput{}
	if len(r.Inputs) != 0 {
		wire, _ := json.Marshal(url)
		inputs = append(inputs, graph.NormalizedInput{PortID: "github_issue_url", Type: "string", Value: wire})
	}
	request := orchestration.SubmitLoopRequest{SubmitGraphRequest: orchestration.SubmitGraphRequest{Contribution: &c, Inputs: inputs, SubmissionID: c.SubmissionID, IdempotencyKey: "contribution-" + id, SnapshotID: "contribution-snapshot-" + id, QueueItemID: c.QueueItemID, GraphRunID: c.RunID, TransitionID: "contribution-transition-" + id, RejectionID: "contribution-reject-" + id, MaxAttempts: 1}, Participant: p.Controller, Loop: selected, NodeID: "contribution"}
	decision, err := s.submitLoopAs(ctx, subject, request)
	if err != nil {
		return contribution.Contract{}, err
	}
	if decision.Accepted == nil {
		return contribution.Contract{}, ErrDenied
	}
	stored, err := repo.GetContribution(ctx, subject.PrincipalID, key)
	if err != nil || stored.Digest != c.Digest {
		return contribution.Contract{}, fleet.ErrCorrupt
	}
	if e := s.carryEngineeringPreparation(ctx, stored); e != nil {
		return contribution.Contract{}, e
	}
	return stored, nil
}

// ContributionLoopReference is the exact selected immutable application reference.
type ContributionLoopReference = reference.RevisionRef

// ContributionIntentKey preserves the sealed canonical payload key for adapters.
func ContributionIntentKey(selected ContributionLoopReference, url, scope string) string {
	return strings.TrimPrefix(contribution.Payload(selected, url, scope), "sha256:")
}
