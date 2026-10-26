package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// ErrContributionInstructionsHeld is a closed scope denial, never upstream prose
// or owner approval/cancellation. A changed source requires owner review elsewhere.
var ErrContributionInstructionsHeld = errors.New("contribution_instructions_held")

type contributionInstructionAuthority struct {
	service *Service
	subject core.Subject
	key     string
	ref     delivery.Reference
	base    delivery.Authority
}

func (a *contributionInstructionAuthority) Admit(ctx context.Context, i delivery.Intent, writing bool) error {
	if a == nil || a.base == nil {
		return delivery.ErrDenied
	}
	// Read-only UNKNOWN reconciliation belongs solely to the actual read authority.
	// In particular our own issue close must not invalidate its post-write readback.
	if !writing {
		return a.base.Admit(ctx, i, false)
	}
	if err := a.base.Admit(ctx, i, true); err != nil {
		return delivery.ErrDenied
	}
	held := func() error { return ErrContributionInstructionsHeld }
	if a.service == nil || i.Reference != a.ref {
		return held()
	}
	c, _, err := a.service.ReadContributionAs(ctx, a.subject, a.key)
	if err != nil || c.Digest != a.ref.Contract || c.RunID != a.ref.Run || c.Key != a.key || c.OwnerID != a.subject.PrincipalID {
		return held()
	}
	if _, err = contribution.Marshal(c); err != nil {
		return held()
	}
	control, err := a.service.ReadContributionControlAs(ctx, a.subject, a.key)
	if err != nil || control.Stop || !a.service.Now().Before(c.Deadline) {
		return held()
	}
	revision, err := a.service.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest || revision.Contribution == nil {
		return held()
	}
	profile := revision.Contribution
	issue, err := contribution.ParseIssueURL(c.Source.URL.URL)
	if err != nil || issue != c.Source.URL || profile.ReadAction != contribution.ReadIssueAction || profile.Repository != c.Repository || profile.RepositoryID != c.Source.RepositoryID || c.Repository != issue.Owner+"/"+issue.Repository || i.Repository != c.Repository || i.RepositoryID != c.Source.RepositoryID {
		return held()
	}
	reader := a.service.ContributionReader
	if reader == nil {
		reader = contribution.Reader{}
	}
	fresh, err := reader.Read(ctx, issue)
	if err != nil || !sameContributionInstructions(c.Source, fresh) {
		return held()
	}
	// Recheck real authority after the network wait; never cache actor/native rights.
	if err := a.base.Admit(ctx, i, true); err != nil {
		return delivery.ErrDenied
	}
	control, err = a.service.ReadContributionControlAs(ctx, a.subject, a.key)
	if err != nil || control.Stop {
		return held()
	}
	return nil
}

func sameContributionInstructions(admitted, fresh contribution.Source) bool {
	if fresh.DiscussionTruncated || fresh.Discussion == nil || len(fresh.Discussion) > contribution.MaxDiscussion || fresh.State != "open" {
		return false
	}
	// Wire hashes include observation metadata, not semantic instruction identity.
	normalize := func(s contribution.Source) contribution.Source {
		s.ObservedAt = time.Time{}
		s.UpdatedAt = time.Time{}
		s.Revision = ""
		s.ContentDigest = ""
		return s
	}
	return reflect.DeepEqual(normalize(admitted), normalize(fresh))
}
