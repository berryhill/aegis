package managergateway

import "os"

// Independently owned native journeys share a two-journey bound through cleanup.
// Restart children reopen a fixed root and stay serial; their scheduled parent
// holds the slot. Tagged delivery factories remain outside this opt-in audit.
var contributionNativeJourneySlots = make(chan struct{}, 2)

type contributionNativeJourneyTest interface {
	Helper()
	Parallel()
	Cleanup(func())
}

func scheduleContributionNativeJourney(t contributionNativeJourneyTest) {
	scheduleContributionNativeJourneyWithSlots(t, contributionNativeJourneySlots,
		os.Getenv("AEGIS_RESTART_ROOT"), os.Getenv("AEGIS_RESTART_PHASE"))
}

// A grouping parent owns no runtime or state and must not hold a leaf permit.
// Each restart leaf creates its own root and retains its slot across both child
// generations and all exact-identity cleanup. Fixed-root children stay serial.
func scheduleContributionNativeJourneyGroup(t contributionNativeJourneyTest) {
	scheduleContributionNativeJourneyGroupWithEnvironment(t,
		os.Getenv("AEGIS_RESTART_ROOT"), os.Getenv("AEGIS_RESTART_PHASE"))
}

func scheduleContributionNativeJourneyGroupWithEnvironment(t contributionNativeJourneyTest, root, phase string) {
	t.Helper()
	if root == "" && phase == "" {
		t.Parallel()
	}
}

func scheduleContributionNativeJourneyWithSlots(t contributionNativeJourneyTest, slots chan struct{}, restartRoot, restartPhase string) {
	t.Helper()
	// Restart children deliberately reopen a fixed root; never make them parallel.
	if restartRoot != "" || restartPhase != "" {
		return
	}
	t.Parallel()
	// Acquire before any fixture setup. Register release first so testing's LIFO
	// cleanup retains the slot through authority/process/store/TempDir cleanup.
	slots <- struct{}{}
	t.Cleanup(func() { <-slots })
}
