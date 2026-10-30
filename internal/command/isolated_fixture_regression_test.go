package command

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/berryhill/aegis/internal/testprocess"
	"strings"
	"testing"
	"time"
)

func TestIsolatedFixtureRootRegistrationPrecedesSerialTests(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := testprocess.CombinedOutput(exec.Command(executable, "-test.list=."), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Fields(string(output))
	want := []string{"TestAuthorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline", "TestInstalledJourneyProtectedRecovery"}
	if runtime.GOOS == "linux" {
		want = append([]string{"TestInstalledUpgradeFullCommand", "TestRecoveryCLIApprovalRejections"}, want...)
	}
	want = append(want, isolatedFixtureNativeRoots...)
	if len(names) < len(want) {
		t.Fatal("missing isolated fixture roots")
	}
	for i, name := range want {
		if names[i] != name {
			t.Fatalf("fixture root %s must precede serial tests; position %d contains %s", name, i, names[i])
		}
		count := 0
		for _, actual := range names {
			if actual == name {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("fixture %s registered %d times", name, count)
		}
	}
}

func TestIsolatedFixtureFailureLocationsWithholdPrivateOutput(t *testing.T) {
	const canary = "private-fixture-diagnostic-canary"
	output := []byte("--- FAIL: TestInstalledJourneyProtectedRecovery\n    update_protected_journey_test.go:157: " + canary + "\n")
	_, err := isolatedFixtureResult("TestInstalledJourneyProtectedRecovery", output, errors.New("exit status 1"))
	if err == nil || !strings.Contains(err.Error(), "update_protected_journey_test.go:157") || strings.Contains(err.Error(), canary) {
		t.Fatal("failure location missing or private child diagnostics exposed")
	}
}

func TestIsolatedFixtureForeignChildFailsWithoutRecursion(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"foreign", ""} {
		cmd := exec.Command(executable, "-test.run=^TestInstalledJourneyProtectedRecovery$", "-test.count=1", "-test.v", "-test.timeout=5s")
		cmd.Env, err = isolatedFixtureEnvironment(os.Environ(), t.TempDir(), "/unused/cache", "/unused/modules", marker)
		if err != nil {
			t.Fatal(err)
		}
		output, runErr := testprocess.CombinedOutput(cmd, 5*time.Second)
		if runErr == nil || !strings.Contains(string(output), "fixture child marker") || strings.Contains(string(output), "=== PAUSE") || strings.Contains(string(output), "/healthy") {
			t.Fatal("malformed child did not fail before scheduling/body effects")
		}
	}
}

func TestIsolatedFixtureSlotsAndCleanup(t *testing.T) {
	if cap(isolatedFixtureSlots) != 2 {
		t.Fatal("fixture lane is not two slots")
	}
	slots := make(chan struct{}, 2)
	var cleanups []func()
	register := func(f func()) { cleanups = append(cleanups, f) }
	for i := 0; i < 2; i++ {
		if err := isolatedFixtureAcquire(slots, time.Now().Add(isolatedFixtureCleanupReserve+time.Second), register); err != nil {
			t.Fatal(err)
		}
	}
	if err := isolatedFixtureAcquire(slots, time.Now().Add(isolatedFixtureCleanupReserve+10*time.Millisecond), register); err == nil {
		t.Fatal("third fixture admitted")
	}
	if err := isolatedFixtureAcquire(make(chan struct{}, 2), time.Now().Add(-time.Second), register); err == nil {
		t.Fatal("expired fixture admitted")
	}
	if len(cleanups) != 2 {
		t.Fatal("denied fixture registered cleanup")
	}
	register(func() {
		if len(slots) != 2 {
			t.Error("slot released before private HOME cleanup")
		}
	})
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	if len(slots) != 0 {
		t.Fatal("slot not released")
	}
}

func TestIsolatedFixtureSelection(t *testing.T) {
	root := "TestInstalledJourneyProtectedRecovery"
	for _, tt := range []struct {
		root, marker string
		child, bad   bool
	}{
		{root, "", false, false}, {root, root, true, false}, {root, root + "/healthy", false, true},
		{root, "foreign", false, true}, {root + "/healthy", "", false, true}, {"TestOther", "", false, true},
	} {
		child, err := isolatedFixtureMode(tt.root, tt.marker)
		if child != tt.child || (err != nil) != tt.bad {
			t.Fatalf("selection %q %q: %v %v", tt.root, tt.marker, child, err)
		}
	}
}

func TestIsolatedFixtureExplicitSelectionStaysInline(t *testing.T) {
	for _, tc := range []struct {
		run, skip, cpu, coverage string
		short                    bool
		fork                     bool
	}{
		{fork: true},
		{run: "^(TestInstalledUpgradeFullCommand|TestInstalledJourneyProtectedRecovery)$", fork: true},
		{run: "^TestInstalledUpgradeFullCommand$/cancelled-auth"},
		{run: "TestInstalledUpgradeFullCommand[/]cancelled-auth"},
		{skip: "cancelled-auth"},
		{cpu: "1,2"},
		{short: true},
		{coverage: "atomic"},
	} {
		if got := isolatedFixtureMayFork(tc.run, tc.skip, tc.cpu, tc.short, tc.coverage); got != tc.fork {
			t.Fatalf("explicit selection policy changed: %+v got fork=%t", tc, got)
		}
	}
}

func TestIsolatedFixtureTargetedSubtestPreservesGoSelection(t *testing.T) {
	root := "TestInstalledJourneyProtectedRecovery"
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, skip := range []bool{false, true} {
		deadline, ok := t.Deadline()
		if !ok {
			t.Fatal("selection regression requires a parent deadline")
		}
		budget, err := isolatedFixtureRemaining(deadline)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"-test.run=^" + root + "$/config-drift", "-test.count=1", "-test.v", "-test.timeout=" + budget.String()}
		if skip {
			args = append(args, "-test.skip=^"+root+"$/config-drift")
		}
		cmd := exec.Command(executable, args...)
		env, err := isolatedFixtureEnvironment(os.Environ(), t.TempDir(), "/unused/cache", "/unused/modules", root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range env {
			if !strings.HasPrefix(entry, isolatedFixtureMarker+"=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		output, runErr := testprocess.CombinedOutput(cmd, budget)
		selected := strings.Contains(string(output), "=== RUN   "+root+"/config-drift")
		if runErr != nil || selected == skip || strings.Contains(string(output), "=== RUN   "+root+"/healthy") || strings.Contains(string(output), "=== PAUSE") {
			t.Fatalf("targeted/skip selection changed (skip=%t, selected=%t, err=%v)", skip, selected, runErr)
		}
	}
}

func TestIsolatedFixtureEnvironment(t *testing.T) {
	home := t.TempDir()
	env, err := isolatedFixtureEnvironment([]string{"PATH=/bin", "OPENAI_API_KEY=must-not-copy", "HERMES_HOME=/ambient", "HOME=/ambient", "TMPDIR=/deep", "GOFLAGS=-p=1"}, home, "/original/cache", "/original/mod", "TestInstalledUpgradeFullCommand")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		values[k] = v
	}
	for _, key := range []string{"OPENAI_API_KEY", "HERMES_HOME", "TMPDIR"} {
		if _, ok := values[key]; ok {
			t.Fatalf("ambient %s copied", key)
		}
	}
	if values["HOME"] != home || values["GOCACHE"] != "/original/cache" || values["GOMODCACHE"] != "/original/mod" {
		t.Fatal("home/cache lost")
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		info, err := os.Stat(values[key])
		if err != nil || info.Mode().Perm() != 0700 || !strings.HasPrefix(values[key], home) {
			t.Fatalf("private %s: %v", key, err)
		}
	}
	if filepath.IsAbs(values["TMPDIR"]) {
		t.Fatal("TMPDIR unexpectedly set")
	}
}

func TestIsolatedFixtureResult(t *testing.T) {
	root := "TestInstalledUpgradeFullCommand"
	for _, tt := range []struct {
		output string
		runErr error
		want   string
	}{
		{"=== RUN   " + root + "\n--- PASS: " + root + " (1.00s)\nPASS\n", nil, "PASS"},
		{"=== RUN   " + root + "\n--- SKIP: " + root + " (0.00s)\nPASS\n", nil, "SKIP"},
		{"PASS\n", nil, ""}, {"=== RUN   " + root + "\n--- FAIL: " + root + " (0.00s)\nFAIL\n", errors.New("exit"), ""},
		{"=== RUN   " + root + "Other\n--- PASS: " + root + "Other (0.00s)\nPASS\n", nil, ""},
	} {
		got, err := isolatedFixtureResult(root, []byte(tt.output), tt.runErr)
		if got != tt.want || (err != nil) != (tt.want == "") {
			t.Fatalf("result %q %v", got, err)
		}
	}
}

func TestIsolatedFixtureRemainingDeadline(t *testing.T) {
	now := time.Now()
	deadline := now.Add(isolatedFixtureCleanupReserve + time.Second)
	budget, err := isolatedFixtureBudget(deadline, now)
	if err != nil || budget != time.Second {
		t.Fatalf("budget %v %v", budget, err)
	}
	if _, err := isolatedFixtureBudget(now.Add(isolatedFixtureCleanupReserve), now); err == nil {
		t.Fatal("cleanup reserve consumed by helper work")
	}
	if _, err := isolatedFixtureRemaining(time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired deadline admitted")
	}
	if _, err := isolatedFixtureRemaining(time.Time{}); err == nil {
		t.Fatal("missing deadline admitted")
	}
}

// A serial sibling cannot finish until real child work and private cleanup have
// completed while its parallel sibling remains paused at testing's barrier.
func TestIsolatedFixtureStartsBeforeSerialBarrier(t *testing.T) {
	base := t.TempDir()
	slots := make(chan struct{}, 2)
	deadline := time.Now().Add(isolatedFixtureCleanupReserve + 5*time.Second)
	var done <-chan isolatedFixtureOutcome
	var home string
	t.Run("parallel", func(t *testing.T) {
		done = isolatedFixtureStart(slots, deadline, base, func(dir string, budget time.Duration) isolatedFixtureOutcome {
			home = dir
			cmd := exec.Command("/bin/sh", "-c", "printf child-progress")
			cmd.Env = []string{"HOME=" + dir, "PATH=/bin"}
			output, err := testprocess.CombinedOutput(cmd, budget)
			if string(output) != "child-progress" && err == nil {
				err = errors.New("child made no progress")
			}
			return isolatedFixtureOutcome{err: err}
		})
		t.Parallel()
	})
	t.Run("serial", func(t *testing.T) {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatal(result.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("child waited for serial-test barrier")
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatal("private HOME survived result")
		}
		if len(slots) != 0 {
			t.Fatal("slot held until parallel resume")
		}
	})
}

func TestIsolatedFixtureWorkerFailureTimeoutCleanup(t *testing.T) {
	for _, mode := range []string{"failure", "timeout", "expired", "directory-failure"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			slots := make(chan struct{}, 2)
			deadline := time.Now().Add(isolatedFixtureCleanupReserve + time.Second)
			if mode == "expired" {
				deadline = time.Now()
			}
			if mode == "directory-failure" {
				base = filepath.Join(base, "missing", "parent")
			}
			var home string
			done := isolatedFixtureStart(slots, deadline, base, func(dir string, budget time.Duration) isolatedFixtureOutcome {
				home = dir
				cmd := exec.Command("/bin/sh", "-c", "exit 7")
				if mode == "timeout" {
					cmd = exec.Command("/bin/sh", "-c", "sleep 30 & wait")
					budget = 20 * time.Millisecond
				}
				cmd.Env = []string{"HOME=" + dir, "PATH=/bin"}
				_, err := testprocess.CombinedOutput(cmd, budget)
				return isolatedFixtureOutcome{err: err}
			})
			result := <-done
			if result.err == nil {
				t.Fatal("failure accepted")
			}
			if home != "" {
				if _, err := os.Stat(home); !os.IsNotExist(err) {
					t.Fatal("failed worker leaked HOME")
				}
			}
			if len(slots) != 0 {
				t.Fatal("failed worker leaked slot")
			}
		})
	}
}

func TestIsolatedFixtureWorkerTwoSlotsBeforeResultConsumption(t *testing.T) {
	base := t.TempDir()
	slots := make(chan struct{}, 2)
	entered := make(chan string, 3)
	release := make(chan struct{})
	deadline := time.Now().Add(isolatedFixtureCleanupReserve + 5*time.Second)
	run := func(home string, _ time.Duration) isolatedFixtureOutcome {
		entered <- home
		<-release
		return isolatedFixtureOutcome{}
	}
	done1 := isolatedFixtureStart(slots, deadline, base, run)
	done2 := isolatedFixtureStart(slots, deadline, base, run)
	home1, home2 := <-entered, <-entered
	third := isolatedFixtureStart(slots, time.Now().Add(isolatedFixtureCleanupReserve+20*time.Millisecond), base, func(string, time.Duration) isolatedFixtureOutcome {
		return isolatedFixtureOutcome{status: "unexpected admission"}
	})
	if result := <-third; result.err == nil {
		t.Error("third child admitted")
	}
	close(release)
	for _, done := range []<-chan isolatedFixtureOutcome{done1, done2} {
		if result := <-done; result.err != nil {
			t.Error(result.err)
		}
	}
	for _, home := range []string{home1, home2} {
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Error("HOME survived result")
		}
	}
	if len(slots) != 0 {
		t.Fatal("slots held awaiting parent result consumption")
	}
}
