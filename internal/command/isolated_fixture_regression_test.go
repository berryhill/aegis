package command

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/berryhill/aegis/internal/testprocess"
	"strings"
	"testing"
	"time"
)

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
