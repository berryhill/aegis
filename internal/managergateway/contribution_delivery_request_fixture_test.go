package managergateway

import (
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/app"
)

// Controlled in-process fixture only: real HTTP authentication must obtain its
// UID from the admitted Unix transport's SO_PEERCRED, not configuration.
func contributionDeliveryConfiguredUID(t *testing.T, s *app.Service) uint32 {
	t.Helper()
	uid, err := strconv.ParseUint(s.Config.Principal.UID, 10, 32)
	if err != nil {
		t.Fatalf("parse configured fixture principal UID: %v", err)
	}
	return uint32(uid)
}
