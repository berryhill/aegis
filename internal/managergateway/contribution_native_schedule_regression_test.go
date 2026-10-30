package managergateway

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Record the same LIFO cleanup contract as testing.T without running a native
// journey. Parallel is an explicit barrier notification, never a timing sleep.
type nativeJourneyScheduleProbe struct {
	parallel func()
	cleanups []func()
}

func (*nativeJourneyScheduleProbe) Helper() {}
func (p *nativeJourneyScheduleProbe) Parallel() {
	if p.parallel != nil {
		p.parallel()
	}
}
func (p *nativeJourneyScheduleProbe) Cleanup(f func()) { p.cleanups = append(p.cleanups, f) }
func (p *nativeJourneyScheduleProbe) cleanup() {
	for i := len(p.cleanups) - 1; i >= 0; i-- {
		p.cleanups[i]()
	}
}

func TestContributionNativeJourneySchedulingBoundAndCleanup(t *testing.T) {
	if cap(contributionNativeJourneySlots) != 2 {
		t.Fatal("native whole-journey concurrency must remain two")
	}
	slots := make(chan struct{}, 2)
	first, second := &nativeJourneyScheduleProbe{}, &nativeJourneyScheduleProbe{}
	scheduleContributionNativeJourneyWithSlots(first, slots, "", "")
	scheduleContributionNativeJourneyWithSlots(second, slots, "", "")
	if len(slots) != 2 {
		t.Fatal("both independent journeys must hold a slot before setup")
	}

	thirdParallel, thirdSetup, thirdDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	thirdCleaned := make(chan struct{})
	third := &nativeJourneyScheduleProbe{parallel: func() { close(thirdParallel) }}
	go func() {
		scheduleContributionNativeJourneyWithSlots(third, slots, "", "")
		close(thirdSetup)
		<-thirdDone
		third.cleanup()
		close(thirdCleaned)
	}()
	<-thirdParallel
	// Both slots are held, so the third cannot cross into fixture setup.
	select {
	case <-thirdSetup:
		t.Error("third journey entered setup before a whole-journey slot was released")
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
		<-workerDone // cancellation must finish/Wait before releasing custody
		order = append(order, "cancel/wait")
		if len(slots) != 2 {
			t.Error("slot released before cancellation completed")
		}
	})
	first.cleanup()
	<-thirdSetup
	if !reflect.DeepEqual(order, []string{"cancel/wait", "store/home cleanup"}) {
		t.Fatalf("cleanup order: %v", order)
	}
	second.cleanup()
	close(thirdDone)
	<-thirdCleaned
	if len(slots) != 0 {
		t.Fatal("journey slots leaked after cleanup")
	}
}

func TestContributionNativeJourneyGroupDoesNotHoldLeafPermit(t *testing.T) {
	for _, environment := range []struct{ root, phase string }{
		{}, {root: "/fixed/owned-restart"}, {phase: "initial"},
	} {
		calls := 0
		probe := &nativeJourneyScheduleProbe{parallel: func() { calls++ }}
		scheduleContributionNativeJourneyGroupWithEnvironment(probe, environment.root, environment.phase)
		want := 0
		if environment.root == "" && environment.phase == "" {
			want = 1
		}
		if calls != want || len(probe.cleanups) != 0 {
			t.Fatal("group scheduling acquired a leaf permit or parallelized a fixed-root child")
		}
	}
}

func TestContributionNativeJourneyRestartStaysSerial(t *testing.T) {
	for _, restart := range []struct{ root, phase string }{
		{root: "/fixed/owned-restart"},
		{phase: "initial"},
		{root: "/fixed/owned-restart", phase: "resume"},
	} {
		// A full channel makes any accidental attempt to acquire a permit fail
		// immediately (closed send), rather than hanging this regression.
		slots := make(chan struct{})
		close(slots)
		probe := &nativeJourneyScheduleProbe{parallel: func() { t.Error("restart child made parallel") }}
		scheduleContributionNativeJourneyWithSlots(probe, slots, restart.root, restart.phase)
		if len(probe.cleanups) != 0 {
			t.Fatal("serial restart child registered a slot cleanup")
		}
	}
}

func TestContributionNativeJourneyPrivateRootIsolation(t *testing.T) {
	slots := make(chan struct{}, 2)
	parent := t.TempDir()
	roots := make([]string, 2)
	probes := []*nativeJourneyScheduleProbe{{}, {}}
	for i, probe := range probes {
		scheduleContributionNativeJourneyWithSlots(probe, slots, "", "")
		root, err := os.MkdirTemp(parent, "journey-")
		if err != nil {
			t.Fatal(err)
		}
		roots[i] = root
		probe.Cleanup(func() {
			if err := os.RemoveAll(root); err != nil {
				t.Error(err)
			}
		})
		if err := os.WriteFile(filepath.Join(root, "state"), []byte{byte(i)}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if roots[0] == roots[1] {
		t.Fatal("journeys share a fixture root")
	}
	probes[0].cleanup()
	if _, err := os.Stat(roots[0]); !os.IsNotExist(err) {
		t.Fatalf("first fixture root survived cleanup: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(roots[1], "state"))
	if err != nil || !reflect.DeepEqual(b, []byte{1}) {
		t.Fatalf("first cleanup altered second private state: %v %v", b, err)
	}
	probes[1].cleanup()
	if len(slots) != 0 {
		t.Fatal("journey slots leaked after isolated root cleanup")
	}
}
