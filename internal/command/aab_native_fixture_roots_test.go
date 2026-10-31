package command

import "testing"

// Run selected native roots immediately after isolated worker registration,
// before unrelated serial fixtures spend the workers' absolute deadline.
func TestPublicRootAuthenticatedContributionJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, false)
}

func TestPublicRootAuthenticatedContributionFinalJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, true)
}

func TestPublicRootAuthenticatedContributionFinalDisconnectResumeJourney(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, true, true)
}

func TestPublicRootContributionDeclaredOperationApprovalHeld(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, false, false, false, false, false, true)
}

func TestPublicRootContributionMissingHostPreflight(t *testing.T) {
	// Deny before effects, supply only the isolated host prerequisite, then
	// disconnect the SAME retained Resume while its builder is in flight.
	// The admitted journey must finish without another Continue/Resume.
	publicRootAuthenticatedContributionJourney(t, true, true, false, false, false, false, true)
}

func TestPublicRootContributionMissingRemotePreflight(t *testing.T) {
	publicRootAuthenticatedContributionJourney(t, false, false, false, false, false, false, false, true)
}
