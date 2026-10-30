package api

import "os"

// Only audited, independently owned whole journeys opt in. This is not a
// package-wide concurrency bound. Fixed-port coding recovery, shared-store
// recovery children and externally driven browser variants remain serial.
var installedAPIJourneySlots = make(chan struct{}, 2)

type installedAPIJourneyTest interface {
	Helper()
	Parallel()
	Cleanup(func())
}

func scheduleInstalledAPIJourney(t installedAPIJourneyTest) {
	scheduleInstalledAPIJourneyWithSlots(t, installedAPIJourneySlots,
		os.Getenv("AEGIS_QUEUE_RESULT_BROWSER_TEST") == "1")
}

func scheduleInstalledAPIJourneyWithSlots(t installedAPIJourneyTest, slots chan struct{}, browser bool) {
	t.Helper()
	if browser {
		return
	}
	t.Parallel()
	// Acquire before fixture setup and register first: LIFO cleanup retains
	// custody until all processes, stores and private directories are gone.
	slots <- struct{}{}
	t.Cleanup(func() { <-slots })
}
