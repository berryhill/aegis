package command

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/testprocess"
)

func TestIsolatedFixtureCompiledNativeSelection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"^TestPublicRootContributionMissing(Host|Remote)Preflight$", "^TestInstalledJourneyProtectedRecovery$", "^TestPublicRootAuthenticatedContributionJourney$"} {
		output, err := testprocess.CombinedOutput(exec.Command(executable, "-test.list="+pattern), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		actual := map[string]bool{}
		for _, name := range strings.Fields(string(output)) {
			actual[name] = true
		}
		phase := newIsolatedFixtureNativePhase(pattern)
		for _, root := range isolatedFixtureNativeRoots {
			if phase.pending[root] != actual[root] {
				t.Fatalf("compiled selection differs for %s under %s", root, pattern)
			}
		}
	}
}

func TestIsolatedFixtureAdmissionInvocationOrder(t *testing.T) {
	// Keep a predecessor turn closed to admission while every worker is queued.
	// No scheduler sleeps: the serial caller reserves turns before workers run.
	phase := newIsolatedFixtureNativePhase("^" + regexp.QuoteMeta(isolatedFixtureNativeRoots[0]) + "$")
	_, unblock := phase.admissionTurn()
	slots := make(chan struct{}, 2)
	base := t.TempDir()
	deadline := time.Now().Add(isolatedFixtureCleanupReserve + 5*time.Second)
	entered := make(chan int, 4)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
	results := make([]<-chan isolatedFixtureOutcome, 4)
	for i := range results {
		results[i] = isolatedFixtureStartAfter(phase, slots, deadline, base, func(_ string, _ time.Duration) isolatedFixtureOutcome {
			entered <- i
			<-releases[i]
			return isolatedFixtureOutcome{status: "PASS"}
		})
	}
	var nativeCleanup func()
	phase.track(isolatedFixtureNativeRoots[0], func(f func()) { nativeCleanup = f })
	nativeCleanup()
	if len(slots) != 0 {
		t.Fatal("worker passed reserved predecessor")
	}
	unblock()
	first, second := <-entered, <-entered
	if first+second != 1 || first == second {
		t.Fatalf("first pair %d,%d; upgrade lost first admission", first, second)
	}
	close(releases[0])
	if next := <-entered; next != 2 {
		t.Fatalf("later worker overtook queued worker: %d", next)
	}
	close(releases[1])
	if next := <-entered; next != 3 {
		t.Fatalf("last admission: %d", next)
	}
	close(releases[2])
	close(releases[3])
	// No result is consumed until all admissions have progressed.
	for _, done := range results {
		if result := <-done; result.err != nil {
			t.Fatal(result.err)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 || len(slots) != 0 {
		t.Fatal("admission leaked private state/slots", err)
	}
}

func TestIsolatedFixtureExpiredTurnReleasesSuccessor(t *testing.T) {
	phase := newIsolatedFixtureNativePhase("^nonexistent$")
	slots := make(chan struct{}, 2)
	base := t.TempDir()
	expired := isolatedFixtureStartAfter(phase, slots, time.Now(), base, func(string, time.Duration) isolatedFixtureOutcome { panic("expired admitted") })
	next := isolatedFixtureStartAfter(phase, slots, time.Now().Add(isolatedFixtureCleanupReserve+time.Second), base, func(string, time.Duration) isolatedFixtureOutcome { return isolatedFixtureOutcome{status: "PASS"} })
	if result := <-expired; result.err == nil {
		t.Fatal("expired admission passed")
	}
	if result := <-next; result.err != nil {
		t.Fatal("expired predecessor stranded successor", result.err)
	}
	if len(slots) != 0 {
		t.Fatal("slot leaked")
	}
}
