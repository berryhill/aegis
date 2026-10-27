package managergateway

import (
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

// Open is a trusted transport admission API, not an authentication port. Its
// session map cannot establish provenance for a caller-constructed Subject.
// Password subjects are validated separately against the owning live Console.
// No other existing authentication registry is available to this adapter, so
// deny rather than promoting a manager session into new delivery authority.
func (s *Service) validateContributionRequestSubject(_ core.Subject) error {
	return app.ErrDenied
}
