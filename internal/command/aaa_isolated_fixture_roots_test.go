package command

import "testing"

// Register isolated roots before the serial fixtures. Their existing bodies
// start bounded private child work, then yield at Go's parallel-test barrier.
// Names and bodies remain unchanged for inventory and explicit selection.
func TestAuthorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline(t *testing.T) {
	authorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline(t)
}

func TestInstalledJourneyProtectedRecovery(t *testing.T) {
	installedJourneyProtectedRecovery(t)
}
