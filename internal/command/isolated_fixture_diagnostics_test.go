package command

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/testprocess"
)

func TestIsolatedFixtureCancellationCause(t *testing.T) {
	// Identical OS errors do not distinguish a live from an expired owned
	// context. The helper's typed snapshot is metadata, not exclusive causation.
	killed := errors.New("signal: killed")
	for _, tc := range []struct {
		state testprocess.OwnedContextState
		want  isolatedFixtureCause
	}{
		{testprocess.OwnedContextDeadlineExceeded, isolatedFixtureDeadline},
		{testprocess.OwnedContextCanceled, isolatedFixtureCanceled},
		{testprocess.OwnedContextLive, isolatedFixtureContextLive},
		{testprocess.OwnedContextUnobserved, isolatedFixtureContextUnknown},
		{testprocess.OwnedContextState(255), isolatedFixtureContextUnknown},
	} {
		result := testprocess.ObservedResult{Err: killed, Output: []byte("private-output-canary"), OwnedContext: tc.state}
		got := isolatedFixtureCancellationCause(result.OwnedContext)
		if got != tc.want {
			t.Fatalf("same OS error %v: cause=%s want=%s", killed, got, tc.want)
		}
		if strings.Contains(string(got), "canary") {
			t.Fatal("private context error leaked")
		}
	}
}

func TestIsolatedFixtureChildTimingsWithholdPrivateOutput(t *testing.T) {
	const safe = "AEGIS_FIXTURE_TIMING_V1 build_old enter 123 456"
	output := []byte("private-output-canary /private/credential\n" + safe + "\n" +
		"AEGIS_FIXTURE_TIMING_V1 private-path-canary enter 123 456\n" +
		"AEGIS_FIXTURE_TIMING_V1 healthy finish 123 456 secret-canary\n" +
		"AEGIS_FIXTURE_TIMING_V1 healthy finish -1 456\n" +
		"AEGIS_FIXTURE_TIMING_V1 healthy finish 123 99999999999999999999999999999999\n")
	got := isolatedFixtureChildTimings(output)
	if len(got) != 1 || got[0] != safe {
		t.Fatalf("unsafe/invalid diagnostic accepted: %q", got)
	}
	for _, line := range got {
		if strings.Contains(line, "canary") || strings.Contains(line, "/private") {
			t.Fatal("private output exposed")
		}
	}
}

func TestIsolatedFixturePhaseEmission(t *testing.T) {
	origin := time.Unix(1000, 0)
	var output bytes.Buffer
	isolatedFixtureWritePhase(&output, "build_old", "enter", origin, origin.Add(time.Second), origin.Add(8*time.Minute))
	if got := output.String(); got != "AEGIS_FIXTURE_TIMING_V1 build_old enter 1000000000 479000000000\n" {
		t.Fatalf("phase timing: %q", got)
	}
	before := output.String()
	for _, invalid := range []string{"private-path-canary", "healthy\nprivate-output-canary"} {
		isolatedFixtureWritePhase(&output, invalid, "enter", origin, origin, origin)
	}
	isolatedFixtureWritePhase(&output, "healthy", "private-state-canary", origin, origin, origin)
	if output.String() != before {
		t.Fatal("invalid phase/state emitted private content")
	}
}

func TestIsolatedFixtureAdmissionTimingKeepsOriginalBudget(t *testing.T) {
	phase := newIsolatedFixtureNativePhase("^" + regexp.QuoteMeta(isolatedFixtureNativeRoots[0]) + "$")
	slots := make(chan struct{}, 2)
	base := t.TempDir()
	origin := time.Now()
	deadline := origin.Add(8 * time.Minute)
	offsets := []time.Duration{0, 100 * time.Second, 120 * time.Second, 122 * time.Second, 125 * time.Second}
	calls := 0
	timing := newIsolatedFixtureTiming(origin, deadline, func() time.Time {
		now := origin.Add(offsets[calls])
		calls++
		return now
	})
	waiting := make(chan struct{})
	timing.observe = func(event isolatedFixtureObservation) {
		if event.phase == isolatedFixtureAdmissionWait {
			close(waiting)
		}
	}
	entered := make(chan time.Duration, 1)
	var home string
	done := isolatedFixtureStartAfterTimed(phase, slots, deadline, base, timing, func(dir string, budget time.Duration) isolatedFixtureOutcome {
		home = dir
		entered <- budget
		return isolatedFixtureOutcome{status: "PASS"}
	})
	<-waiting
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 || len(slots) != 0 || len(entered) != 0 {
		t.Error("worker allocated private state/slot before native cleanup", err)
	}
	var nativeCleanup func()
	phase.track(isolatedFixtureNativeRoots[0], func(f func()) { nativeCleanup = f })
	nativeCleanup()
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if budget := <-entered; budget != 355*time.Second {
		t.Fatalf("admission reset original budget: %s", budget)
	}
	if len(result.timing) != len(offsets) {
		t.Fatalf("missing timing boundaries: %+v", result.timing)
	}
	for i, event := range result.timing {
		if event.elapsed != offsets[i] || event.remaining != 477*time.Second-offsets[i] {
			t.Fatalf("boundary %d changed deadline: %+v", i, event)
		}
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("HOME survived result publication")
	}
	if len(slots) != 0 {
		t.Fatal("slot survived result publication")
	}
}

func TestIsolatedFixtureTimingOriginalBudget(t *testing.T) {
	origin := time.Unix(1000, 0)
	deadline := origin.Add(8 * time.Minute)
	now := origin
	timing := newIsolatedFixtureTiming(origin, deadline, func() time.Time { return now })
	for _, tc := range []struct {
		phase             isolatedFixturePhase
		offset, remaining time.Duration
	}{
		{isolatedFixtureAdmissionWait, 0, 477 * time.Second},
		{isolatedFixtureNativeReady, 100 * time.Second, 377 * time.Second},
		{isolatedFixtureAdmitted, 120 * time.Second, 357 * time.Second},
		{isolatedFixtureWorkerLaunch, 122 * time.Second, 355 * time.Second},
		{isolatedFixtureWorkerFinish, 477 * time.Second, 0},
	} {
		now = origin.Add(tc.offset)
		got := timing.mark(tc.phase)
		if got.elapsed != tc.offset || got.remaining != tc.remaining {
			t.Fatalf("%s: %+v", tc.phase, got)
		}
	}
}
