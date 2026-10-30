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
	return !strings.Contains(run, "/") && skip == "" && cpu == "" && !short && coverage == ""
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
		return "", fmt.Errorf("isolated fixture process: %w", runErr)
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
	if !isolatedFixtureMayFork(flag.Lookup("test.run").Value.String(), flag.Lookup("test.skip").Value.String(), flag.Lookup("test.cpu").Value.String(), testing.Short(), testing.CoverMode()) {
		return false
	}
	t.Parallel()
	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("isolated fixture requires a parent deadline")
	}
	if err := isolatedFixtureAcquire(isolatedFixtureSlots, deadline, t.Cleanup); err != nil {
		t.Fatal(err)
	}
	// Registered first: LIFO cleanup holds the slot through child Wait and HOME removal.
	home := t.TempDir()
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
		output, e := testprocess.CombinedOutput(exec.Command("go", "env", "GOCACHE", "GOMODCACHE"), budget)
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
	env, err := isolatedFixtureEnvironment(os.Environ(), home, isolatedFixtureCaches.build, isolatedFixtureCaches.modules, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := isolatedFixtureRemaining(deadline)
	if err != nil {
		t.Fatal(err)
	}
	// Every budget calculation, including slot waiting and cache discovery,
	// already reserves the bounded helper WaitDelay and cleanup headroom.
	cmd := exec.Command(executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.v", "-test.timeout="+remaining.String())
	cmd.Env = env
	output, runErr := testprocess.CombinedOutput(cmd, remaining)
	status, err := isolatedFixtureResult(t.Name(), output, runErr)
	if err != nil {
		// Do not echo child output: even fixture failure logs may contain secret inputs.
		t.Fatalf("%v (captured %d bytes; private output withheld)", err, len(output))
	}
	if status == "SKIP" {
		t.Skip("isolated fixture child skipped")
	}
	return true
}
