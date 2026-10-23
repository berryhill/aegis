package app

import (
	"context"
	"net/url"
	"reflect"

	"github.com/berryhill/aegis/internal/core"
)

// ExecuteDoerTaskInput creates an idempotently retained task, or resumes an exact
// retained version. A resume carries no replacement contract or execution keys.
type ExecuteDoerTaskInput struct {
	Create          *DoerDraftInput `json:"create,omitempty"`
	ID              string          `json:"id,omitempty"`
	ExpectedVersion uint64          `json:"expected_version,omitempty"`
}

type ExecuteDoerTaskResult struct {
	Task            DoerDraft              `json:"task"`
	Status          string                 `json:"status"`
	ConfirmationURL string                 `json:"confirmation_url,omitempty"`
	Readiness       DoerCandidateReadiness `json:"readiness"`
	Publication     *PublishedLoop         `json:"publication,omitempty"`
	Run             *QueueLoopResult       `json:"run,omitempty"`
}

// ExecuteDoerTaskAs is a foreground owning-service operation, not a scheduler or
// authority source. The original transport identity is used for every effect.
// Publication and Queue retain their existing durable conflict/recovery fences.
func (s *Service) ExecuteDoerTaskAs(ctx context.Context, subject core.Subject, in ExecuteDoerTaskInput) (out ExecuteDoerTaskResult, err error) {
	s.doerTaskMu.Lock()
	defer s.doerTaskMu.Unlock()
	if err = s.requirePrincipal(subject); err != nil {
		return
	}
	if in.Create != nil {
		if in.ID != "" || in.ExpectedVersion != 0 || in.Create.ID != "" || in.Create.CreationKey == "" {
			return out, ErrDoerDraftInvalid
		}
		out.Task, err = s.SaveDoerDraftAs(ctx, subject, *in.Create)
	} else {
		if in.ID == "" || in.ExpectedVersion == 0 {
			return out, ErrDoerDraftInvalid
		}
		out.Task, err = s.ReadDoerDraftAs(ctx, subject, in.ID)
		if err == nil && out.Task.Version != in.ExpectedVersion {
			err = ErrConflict
		}
	}
	if err != nil {
		return
	}
	d, err := s.ValidateDoerDraftPublicationAs(ctx, subject, out.Task.ID, out.Task.Version)
	if err != nil {
		return out, err
	}
	candidate, _, err := NewDoerLoopRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
	if err != nil {
		return out, err
	}
	// Check an existing immutable publication before freezing execution intent.
	if _, e := s.retainedDoerPublication(ctx, subject, d); e != nil {
		return out, e
	}
	input := QueueLoopInput{Agent: d.Agent, Loop: RevisionReference(candidate.LoopID, candidate.Revision, candidate.Digest), IdempotencyKey: d.PublicationKey + "-run", Activate: true}
	// Reject an already conflicting reservation without mutating the draft.
	_, err = s.exactDoerRunContext(ctx, subject, input)
	if err != nil {
		return out, err
	}
	out.Readiness, err = s.doerCandidateReadinessWithSetupAs(ctx, subject, DoerCandidateReadinessInput{Agent: d.Agent, Candidate: candidate}, false, true)
	if err != nil {
		return out, err
	}
	if out.Readiness.Controller.State != "ready" {
		out.Status = "independent_setup_required"
		out.ConfirmationURL = "/console/loops/doer/setup?draft_id=" + url.QueryEscape(d.ID)
		return out, nil
	}
	// Freeze under the owning Store CAS BEFORE reserving or preparing host scope.
	// A crash between these writes leaves a frozen, replayable task, never an
	// editable draft with a durable incompatible reservation. Setup-only blockers
	// above remain editable and may still bind an independently approved successor.
	err = s.Store.Update(doerDraftKind, doerDraftSlot(subject.PrincipalID), func(raw []byte) (any, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if e := s.requirePrincipal(subject); e != nil {
			return nil, e
		}
		set, e := decodeDoerDraftSet(raw, subject.PrincipalID)
		if e != nil {
			return nil, e
		}
		for i, current := range set.Drafts {
			if current.ID != d.ID {
				continue
			}
			if !reflect.DeepEqual(current, d) {
				return nil, ErrConflict
			}
			if !s.Now().Before(current.ExpiresAt) {
				return nil, ErrExpired
			}
			if current.ExecutionDigest != "" && current.ExecutionDigest != candidate.Digest {
				return nil, ErrConflict
			}
			if _, e := s.ValidateDoerDraftPublicationAs(ctx, subject, d.ID, d.Version); e != nil {
				return nil, e
			}
			current.ExecutionDigest = candidate.Digest
			set.Drafts[i] = current
			out.Task = current
			return set, nil
		}
		return nil, ErrDenied
	})
	if err != nil {
		return out, err
	}
	ctx, err = s.reserveExactDoerRun(ctx, subject, input)
	if err != nil {
		return out, err
	}
	// This is execution admission, not the ten-second public inspection probe.
	// Reuse the same real checks as Queue preflight, under the reserved exact Run.
	out.Readiness, err = s.doerCandidateReadinessWithSetupAs(ctx, subject, DoerCandidateReadinessInput{Agent: d.Agent, Candidate: candidate}, true, false)
	if err != nil {
		return out, err
	}
	if !out.Readiness.CanExecute {
		out.Status = "independent_setup_required"
		out.ConfirmationURL = "/console/loops/doer/setup?draft_id=" + url.QueryEscape(d.ID)
		return out, nil
	}
	published, err := s.retainedDoerPublication(ctx, subject, d)
	if err != nil {
		return out, err
	}
	if published == nil {
		created, e := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: d.Agent.ID, Revision: candidate, ExpectedPreviousDigest: d.PreviousDigest, IdempotencyKey: d.PublicationKey})
		if e != nil {
			return out, e
		}
		published = &created
	}
	out.Publication = published
	// Read the exact immutable definition back before execution. Unknown write
	// outcomes are reconciled by replaying these same keys, never minting a Run.
	exact, err := s.GetLoopAs(ctx, subject, d.LoopID, d.Revision)
	if err != nil {
		return out, err
	}
	if exact.Digest != candidate.Digest {
		return out, ErrConflict
	}
	run, err := s.QueueLoopAs(ctx, subject, input)
	out.Run = &run
	out.Status = "execution_observed"
	return out, err
}
