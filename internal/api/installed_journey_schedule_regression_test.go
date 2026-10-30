package api

import (
	"context"
	"reflect"
	"testing"
)

// Match testing.T's LIFO cleanup without starting an installed journey.
type installedAPIJourneyProbe struct {
	parallel func()
	cleanups []func()
}

func (*installedAPIJourneyProbe) Helper() {}
func (p *installedAPIJourneyProbe) Parallel() {
	if p.parallel != nil {
		p.parallel()
	}
}
func (p *installedAPIJourneyProbe) Cleanup(f func()) { p.cleanups = append(p.cleanups, f) }
func (p *installedAPIJourneyProbe) cleanup() {
	for i := len(p.cleanups) - 1; i >= 0; i-- {
		p.cleanups[i]()
	}
}

func TestInstalledAPIJourneySchedulingBoundAndCleanup(t *testing.T) {
	if cap(installedAPIJourneySlots) != 2 {
		t.Fatal("installed whole-journey concurrency must remain two")
	}
	slots := make(chan struct{}, 2)
	first, second := &installedAPIJourneyProbe{}, &installedAPIJourneyProbe{}
	scheduleInstalledAPIJourneyWithSlots(first, slots, false)
	scheduleInstalledAPIJourneyWithSlots(second, slots, false)
	if len(slots) != 2 {
		t.Fatal("both journeys must hold a slot before setup")
	}

	thirdParallel, thirdSetup := make(chan struct{}), make(chan struct{})
	thirdDone, thirdCleaned := make(chan struct{}), make(chan struct{})
	third := &installedAPIJourneyProbe{parallel: func() { close(thirdParallel) }}
	go func() {
		scheduleInstalledAPIJourneyWithSlots(third, slots, false)
		close(thirdSetup)
		<-thirdDone
		third.cleanup()
		close(thirdCleaned)
	}()
	<-thirdParallel
	// No sleeps: both permits are still held by the first two journeys.
	select {
	case <-thirdSetup:
		t.Error("third journey entered setup before a slot was released")
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { <-ctx.Done(); close(workerDone) }()
	var order []string
	first.Cleanup(func() {
		order = append(order, "store/home cleanup")
		if len(slots) != 2 {
			t.Error("slot released before store/home cleanup")
		}
	})
	first.Cleanup(func() {
		cancel()
		<-workerDone
		order = append(order, "cancel/wait")
		if len(slots) != 2 {
			t.Error("slot released before cancellation/wait completed")
		}
	})
	first.cleanup()
	<-thirdSetup
	if !reflect.DeepEqual(order, []string{"cancel/wait", "store/home cleanup"}) {
		t.Errorf("cleanup order: %v", order)
	}
	second.cleanup()
	close(thirdDone)
	<-thirdCleaned
	if len(slots) != 0 {
		t.Fatal("journey slots leaked after cleanup")
	}
}

func TestInstalledAPIJourneyBrowserRemainsSerial(t *testing.T) {
	// An accidental acquisition panics rather than hanging this regression.
	slots := make(chan struct{})
	close(slots)
	probe := &installedAPIJourneyProbe{parallel: func() { t.Error("external browser journey made parallel") }}
	scheduleInstalledAPIJourneyWithSlots(probe, slots, true)
	if len(probe.cleanups) != 0 {
		t.Fatal("serial browser journey registered a slot cleanup")
	}
}

func TestInstalledAPIJourneyReleaseAfterFatalCleanup(t *testing.T) {
	// Testing invokes Cleanup after Fatal/Goexit as well as normal return.
	// Use an expected skip to exercise Goexit without failing the parent.
	slots := make(chan struct{}, 2)
	t.Run("goexit", func(t *testing.T) {
		probe := &installedAPIJourneyProbe{}
		scheduleInstalledAPIJourneyWithSlots(probe, slots, false)
		t.Cleanup(probe.cleanup)
		t.Skip("exercise cleanup on Goexit")
	})
	if len(slots) != 0 {
		t.Fatal("journey slot leaked on Goexit")
	}
}
