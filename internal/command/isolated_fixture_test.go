package command

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/testprocess"
)

const isolatedFixtureMarker = "AEGIS_COMMAND_FIXTURE_CHILD"
const isolatedFixtureCleanupReserve = 3 * time.Second

// Only these process-global-environment fixtures may enter the two-slot lane.
func isolatedFixtureMode(root, marker string) (bool, error) {
	switch root {
	case "TestInstalledJourneyProtectedRecovery", "TestAuthorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline", "TestRecoveryCLIApprovalRejections", "TestInstalledUpgradeFullCommand":
	default:
		return false, errors.New("fixture root is not allowlisted")
	}
	if marker == "" {
		return false, nil
	}
	if marker != root {
		return false, errors.New("foreign or malformed fixture child marker")
	}
	return true, nil
}

var isolatedFixtureSlots = make(chan struct{}, 2)
var isolatedFixtureCaches struct {
	sync.Once
	build, modules string
	err            error
}

func isolatedFixtureAcquire(slots chan struct{}, deadline time.Time, cleanup func(func())) error {
	budget, err := isolatedFixtureRemaining(deadline)
	if err != nil {
		return err
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case slots <- struct{}{}:
		cleanup(func() { <-slots })
		return nil
	case <-timer.C:
		return errors.New("parent deadline expired waiting for fixture slot")
	}
}

func isolatedFixtureRemaining(deadline time.Time) (time.Duration, error) {
	return isolatedFixtureBudget(deadline, time.Now())
}

func isolatedFixtureBudget(deadline, now time.Time) (time.Duration, error) {
	if deadline.IsZero() {
		return 0, errors.New("fixture requires the parent test deadline")
	}
	remaining := deadline.Sub(now) - isolatedFixtureCleanupReserve
	if remaining <= 0 {
		return 0, errors.New("fixture parent deadline expired before launch")
	}
	return remaining, nil
}

// Preserve Go's own subtest, skip, CPU, short and coverage semantics by keeping
// those explicitly selected runs inline. Only ordinary whole-root runs fork.
func isolatedFixtureMayFork(run, skip, cpu string, short bool, coverage string) bool {
	// Go matches top-level alternatives independently. A whole-expression
	// regexp could carry an inline flag into another alternative and invent a
	// selected native root. Preserve Go's own matching for these explicit modes.
	for _, prefix := range []string{"(?i", "(?m", "(?s", "(?U", "(?-"} {
		if strings.Contains(run, prefix) {
			return false
		}
	}
	return !strings.Contains(run, "/") && skip == "" && cpu == "" && !short && coverage == ""
}

func isolatedFixtureForkEnabled() bool {
	// Failfast may suppress a selected native root entirely. Repeated runs must
	// not reuse a completed one-shot native phase. Keep both on Go's inline path.
	return flag.Lookup("test.failfast").Value.String() != "true" && flag.Lookup("test.count").Value.String() == "1" && isolatedFixtureMayFork(flag.Lookup("test.run").Value.String(), flag.Lookup("test.skip").Value.String(), flag.Lookup("test.cpu").Value.String(), testing.Short(), testing.CoverMode())
}

var isolatedFixtureNativeRoots = []string{
	"TestPublicRootAuthenticatedContributionJourney",
	"TestPublicRootAuthenticatedContributionFinalJourney",
	"TestPublicRootAuthenticatedContributionFinalDisconnectResumeJourney",
	"TestPublicRootContributionDeclaredOperationApprovalHeld",
	"TestPublicRootContributionMissingHostPreflight",
	"TestPublicRootContributionMissingRemotePreflight",
}

// Track selected roots, not registration order: shuffle and native-first runs
// must retain completed cleanup before any private worker is registered.
type isolatedFixtureNativePhase struct {
	mu      sync.Mutex
	pending map[string]bool
	done    chan struct{}
}

func newIsolatedFixtureNativePhase(run string) *isolatedFixtureNativePhase {
	match := regexp.MustCompile(run) // testing already validated test.run.
	p := &isolatedFixtureNativePhase{pending: make(map[string]bool), done: make(chan struct{})}
	for _, root := range isolatedFixtureNativeRoots {
		if match.MatchString(root) {
			p.pending[root] = true
		}
	}
	if len(p.pending) == 0 {
		close(p.done)
	}
	return p
}

var isolatedFixtureNativeOnce sync.Once
var isolatedFixtureNativeSelected *isolatedFixtureNativePhase

func isolatedFixtureSelectedNativePhase() *isolatedFixtureNativePhase {
	isolatedFixtureNativeOnce.Do(func() {
		isolatedFixtureNativeSelected = newIsolatedFixtureNativePhase(flag.Lookup("test.run").Value.String())
	})
	return isolatedFixtureNativeSelected
}

func (p *isolatedFixtureNativePhase) track(root string, cleanup func(func())) {
	cleanup(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.pending[root] {
			delete(p.pending, root)
			if len(p.pending) == 0 {
				close(p.done)
			}
		}
	})
}

func (p *isolatedFixtureNativePhase) wait(deadline time.Time) error {
	if p == nil {
		return nil
	}
	budget, err := isolatedFixtureRemaining(deadline)
	if err != nil {
		return err
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-timer.C:
		return errors.New("parent deadline expired waiting for selected native fixture cleanup")
	}
}

func isolatedFixtureEnvironment(parent []string, home, buildCache, moduleCache, root string) ([]string, error) {
	env := []string{}
	for _, entry := range parent {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "PATH", "USER", "LOGNAME", "LANG", "LC_ALL", "GOTOOLCHAIN", "GOMAXPROCS", "GOFLAGS", "GOMEMLIMIT":
			env = append(env, entry)
		}
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		path := home
		if key != "HOME" {
			path = filepath.Join(home, key)
		}
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, 0700); err != nil {
			return nil, err
		}
		env = append(env, key+"="+path)
	}
	// Keep the parent's actual caches, not defaults recomputed under private HOME.
	env = append(env, "GOCACHE="+buildCache, "GOMODCACHE="+moduleCache, isolatedFixtureMarker+"="+root)
	return env, nil
}

func isolatedFixtureResult(root string, output []byte, runErr error) (string, error) {
	if runErr != nil {
		// Report only source locations, never arbitrary child diagnostics: those
		// can contain protected fixture inputs. This still identifies the exact
		// failed assertion when an isolated root fails under whole-lane load.
		locations := regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+\.go:[0-9]+):`).FindAllSubmatch(output, 8)
		where := make([]string, 0, len(locations))
		for _, location := range locations {
			where = append(where, string(location[1]))
		}
		return "", fmt.Errorf("isolated fixture process: %w (assertion locations: %s)", runErr, strings.Join(where, ", "))
	}
	ran, terminal := false, ""
	for _, line := range strings.Split(string(output), "\n") {
		if line == "=== RUN   "+root {
			ran = true
		}
		for _, status := range []string{"PASS", "SKIP"} {
			if strings.HasPrefix(line, "--- "+status+": "+root+" (") {
				terminal = status
			}
		}
	}
	if !ran || terminal == "" {
		return "", errors.New("isolated fixture did not report the exact selected root's terminal PASS or SKIP")
	}
	return terminal, nil
}

// isolateCommandFixture returns true only in the parent. Child bodies retain
// their original serial t.Setenv, real authentication and cleanup behavior.
func isolateCommandFixture(t *testing.T) bool {
	t.Helper()
	marker, present := os.LookupEnv(isolatedFixtureMarker)
	if present && marker == "" {
		t.Fatal("empty fixture child marker")
	}
	child, err := isolatedFixtureMode(t.Name(), marker)
	if err != nil {
		t.Fatal(err)
	}
	if child {
		return false
	}
	if !isolatedFixtureForkEnabled() {
		return false
	}
	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("isolated fixture requires a parent deadline")
	}
	// Snapshot ambient inputs before t.Parallel lets serial Setenv tests resume.
	parentEnv, tempBase := os.Environ(), os.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	isolatedFixtureCaches.Do(func() {
		budget, e := isolatedFixtureRemaining(deadline)
		if e != nil {
			isolatedFixtureCaches.err = e
			return
		}
		cmd := exec.Command("go", "env", "GOCACHE", "GOMODCACHE")
		cmd.Env = parentEnv
		output, e := testprocess.CombinedOutput(cmd, budget)
		if e != nil {
			isolatedFixtureCaches.err = e
			return
		}
		paths := strings.Fields(string(output))
		if len(paths) != 2 || !filepath.IsAbs(paths[0]) || !filepath.IsAbs(paths[1]) {
			isolatedFixtureCaches.err = errors.New("invalid parent Go cache paths")
			return
		}
		isolatedFixtureCaches.build, isolatedFixtureCaches.modules = paths[0], paths[1]
	})
	if isolatedFixtureCaches.err != nil {
		t.Fatal(isolatedFixtureCaches.err)
	}
	root := t.Name()
	phase := isolatedFixtureSelectedNativePhase()
	done := isolatedFixtureStartAfter(phase, isolatedFixtureSlots, deadline, tempBase, func(home string, remaining time.Duration) isolatedFixtureOutcome {
		env, err := isolatedFixtureEnvironment(parentEnv, home, isolatedFixtureCaches.build, isolatedFixtureCaches.modules, root)
		if err != nil {
			return isolatedFixtureOutcome{err: err}
		}
		remaining, err = isolatedFixtureRemaining(deadline)
		if err != nil {
			return isolatedFixtureOutcome{err: err}
		}
		// Resolve no executable or environment from the mutable parent here.
		cmd := exec.Command(executable, "-test.run=^"+regexp.QuoteMeta(root)+"$", "-test.count=1", "-test.v", "-test.timeout="+remaining.String())
		cmd.Env = env
		output, runErr := testprocess.CombinedOutput(cmd, remaining)
		status, err := isolatedFixtureResult(root, output, runErr)
		return isolatedFixtureOutcome{status: status, bytes: len(output), err: err}
	})
	// Admission and cleanup never depend on Go resuming parallel tests.
	t.Parallel()
	result := <-done
	if result.err != nil {
		t.Fatalf("%v (captured %d bytes; private output withheld)", result.err, result.bytes)
	}
	if result.status == "SKIP" {
		t.Skip("isolated fixture child skipped")
	}
	return true
}

type isolatedFixtureOutcome struct {
	status string
	bytes  int
	err    error
}

// The worker exclusively owns its private directory and slot. It never calls T
// methods. After CombinedOutput cancels/joins its process tree, remove the
// private directory, release the admission slot, and only then publish the result.
func isolatedFixtureStart(slots chan struct{}, deadline time.Time, tempBase string, run func(string, time.Duration) isolatedFixtureOutcome) <-chan isolatedFixtureOutcome {
	return isolatedFixtureStartAfter(nil, slots, deadline, tempBase, run)
}

func isolatedFixtureStartAfter(phase *isolatedFixtureNativePhase, slots chan struct{}, deadline time.Time, tempBase string, run func(string, time.Duration) isolatedFixtureOutcome) <-chan isolatedFixtureOutcome {
	done := make(chan isolatedFixtureOutcome, 1)
	go func() {
		result := func() (result isolatedFixtureOutcome) {
			if err := phase.wait(deadline); err != nil {
				return isolatedFixtureOutcome{err: err}
			}
			var release func()
			if err := isolatedFixtureAcquire(slots, deadline, func(f func()) { release = f }); err != nil {
				return isolatedFixtureOutcome{err: err}
			}
			defer release()
			home, err := os.MkdirTemp(tempBase, "aegis-command-fixture-")
			if err != nil {
				return isolatedFixtureOutcome{err: err}
			}
			defer func() { result.err = errors.Join(result.err, os.RemoveAll(home)) }()
			remaining, err := isolatedFixtureRemaining(deadline)
			if err != nil {
				return isolatedFixtureOutcome{err: err}
			}
			return run(home, remaining)
		}()
		done <- result
	}()
	return done
}
