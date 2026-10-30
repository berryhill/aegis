package command

import (
	"flag"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestIsolatedFixtureRepeatedRunsStayInline(t *testing.T) {
	original := flag.Lookup("test.count").Value.String()
	t.Cleanup(func() {
		if err := flag.Set("test.count", original); err != nil {
			t.Error(err)
		}
	})
	if err := flag.Set("test.count", "2"); err != nil {
		t.Fatal(err)
	}
	if isolatedFixtureForkEnabled() {
		t.Fatal("repeated execution reused one-shot native phase")
	}
}

func TestIsolatedFixtureNativePhaseSelection(t *testing.T) {
	for _, tc := range []struct {
		run  string
		want int
	}{
		{"", 6}, {"^TestPublicRootContributionMissing(Host|Remote)Preflight$", 2},
		{"^TestInstalledJourneyProtectedRecovery$", 0},
		{"^TestPublicRootAuthenticatedContributionJourney$", 1},
	} {
		p := newIsolatedFixtureNativePhase(tc.run)
		if len(p.pending) != tc.want {
			t.Fatalf("selection %q: %d", tc.run, len(p.pending))
		}
	}
}

func TestIsolatedFixtureNativePhaseOrderAndSerialBarrier(t *testing.T) {
	for _, nativeFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "worker-first", true: "native-first"}[nativeFirst], func(t *testing.T) {
			roots := isolatedFixtureNativeRoots[:2]
			p := newIsolatedFixtureNativePhase("^(" + regexp.QuoteMeta(roots[0]) + "|" + regexp.QuoteMeta(roots[1]) + ")$")
			slots := make(chan struct{}, 2)
			base := t.TempDir()
			deadline := time.Now().Add(isolatedFixtureCleanupReserve + 5*time.Second)
			started := make(chan string, 1)
			var done <-chan isolatedFixtureOutcome
			checkBlocked := func(t *testing.T) {
				t.Helper()
				select {
				case <-p.done:
					t.Fatal("phase released before last native cleanup")
				default:
				}
				select {
				case <-started:
					t.Fatal("worker started before native cleanup")
				default:
				}
				if len(slots) != 0 {
					t.Fatal("waiting worker occupied a slot")
				}
				entries, err := os.ReadDir(base)
				if err != nil || len(entries) != 0 {
					t.Fatal("private HOME allocated during native phase", err)
				}
			}
			native := func(root string, last bool) {
				t.Run(root, func(t *testing.T) {
					p.track(root, t.Cleanup) // first registration must run last.
					t.Cleanup(func() { checkBlocked(t) })
					checkBlocked(t)
				})
				if !last {
					checkBlocked(t)
				}
			}
			start := func() {
				t.Run("parallel", func(t *testing.T) {
					done = isolatedFixtureStartAfter(p, slots, deadline, base, func(home string, _ time.Duration) isolatedFixtureOutcome {
						started <- home
						return isolatedFixtureOutcome{status: "PASS"}
					})
					t.Parallel()
					if result := <-done; result.err != nil {
						t.Error(result.err)
					}
				})
			}
			// Reverse native order to reject dependence on root declaration order.
			if !nativeFirst {
				start()
			}
			native(roots[1], false)
			native(roots[0], true)
			if nativeFirst {
				start()
			}
			t.Run("later-serial-barrier", func(t *testing.T) {
				// This serial sibling is still holding Go's barrier. A worker tied to
				// t.Parallel resumption rather than native cleanup would deadlock here.
				select {
				case home := <-started:
					select {
					case result := <-done:
						if result.err != nil {
							t.Fatal(result.err)
						}
						// Put the buffered result back for the parallel consumer.
						replay := make(chan isolatedFixtureOutcome, 1)
						replay <- result
						done = replay
					case <-time.After(5 * time.Second):
						t.Fatal("worker did not complete while serial barrier held")
					}
					if _, err := os.Stat(home); !os.IsNotExist(err) {
						t.Fatal("private HOME survived result", err)
					}
					if len(slots) != 0 {
						t.Fatal("slot survived result")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("worker did not progress while later serial barrier held")
				}
			})
		})
	}
}

func TestIsolatedFixtureNativePhaseTimeoutAndZeroNative(t *testing.T) {
	for _, selected := range []bool{false, true} {
		run := "^TestInstalledJourneyProtectedRecovery$"
		if selected {
			run = ""
		}
		p := newIsolatedFixtureNativePhase(run)
		slots := make(chan struct{}, 2)
		base := t.TempDir()
		called := false
		done := isolatedFixtureStartAfter(p, slots, time.Now().Add(isolatedFixtureCleanupReserve+20*time.Millisecond), base, func(string, time.Duration) isolatedFixtureOutcome {
			called = true
			return isolatedFixtureOutcome{status: "PASS"}
		})
		result := <-done
		if selected != (result.err != nil) || called == selected {
			t.Fatalf("selected=%t called=%t err=%v", selected, called, result.err)
		}
		entries, err := os.ReadDir(base)
		if err != nil || len(entries) != 0 || len(slots) != 0 {
			t.Fatal("phase wait leaked home/slot", err)
		}
	}
}
