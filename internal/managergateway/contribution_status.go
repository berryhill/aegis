package managergateway

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/slash"
)

// Identity-only navigation remains metadata-only. The source snapshot, host
// scope and authenticated control provenance require the existing fresh read
// admission; discovering a retained invocation never renews effect authority.
func (s *Service) contributionContractStatus(ctx context.Context, entry session, expected app.ContributionNavigationContract, result *slash.Result) error {
	if s.app.RequirePrincipal(entry.subject) != nil {
		return nil
	}
	c, _, err := s.app.ReadContributionAs(ctx, entry.subject, expected.Key)
	if err != nil {
		return err
	}
	if c.Digest != expected.Digest || c.RunID != expected.RunID {
		return app.ErrDenied
	}
	control, err := s.app.ReadContributionControlAs(ctx, entry.subject, c.Key)
	if err != nil {
		return err
	}
	target := c.Effect
	if control.Sequence > 0 {
		target = control.Delivery
	}
	image, err := s.app.ReadContributionRunProvenanceAs(ctx, entry.subject, c.Key)
	if err != nil {
		return err
	}
	result.Data["provenance"] = image
	result.Data["observing_service"] = app.CurrentContributionServiceIdentity()
	result.Data["contract"] = c
	result.Data["roles"] = c.Roles
	result.Data["effect"] = target
	result.Data["control"] = control
	result.Data["control_sequence"] = control.Sequence
	result.Data["stop_requested"] = control.Stop
	return nil
}
