package userservice

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestObserveProcessImageAt(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	process := filepath.Join(proc, "42")
	if err := os.MkdirAll(process, 0700); err != nil {
		t.Fatal(err)
	}
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[19] = "12345"
	if err := os.WriteFile(filepath.Join(process, "stat"), []byte("42 (aegis worker) "+strings.Join(fields, " ")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, "aegis")
	old := filepath.Join(root, "old")
	for _, path := range []string{installed, old} {
		if err := os.WriteFile(path, []byte(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	image := filepath.Join(process, "exe")
	setImage := func(path string) {
		t.Helper()
		_ = os.Remove(image)
		if err := os.Symlink(path, image); err != nil {
			t.Fatal(err)
		}
	}
	runner := &recordingRunner{mainPID: "42", activeState: "active"}
	setImage(installed)
	if got := observeProcessImageAt(context.Background(), runner, installed, proc); got != "running_current_image" {
		t.Fatalf("current image = %s", got)
	}
	setImage(old)
	if got := observeProcessImageAt(context.Background(), runner, installed, proc); got != "running_stale_image" {
		t.Fatalf("stale image = %s", got)
	}
	runner.nextMainPID = "43"
	runner.mainPIDReads = 0
	if got := observeProcessImageAt(context.Background(), runner, installed, proc); got != "unknown" {
		t.Fatalf("racing PID = %s", got)
	}
	runner.nextMainPID = ""
	_ = os.Remove(image)
	if got := observeProcessImageAt(context.Background(), runner, installed, proc); got != "unknown" {
		t.Fatalf("missing image = %s", got)
	}
}

func TestObserveProcessImageOfCurrentProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{mainPID: strconv.Itoa(os.Getpid()), activeState: "active"}
	if got := observeProcessImage(context.Background(), runner, executable); got != "running_current_image" {
		t.Fatalf("current process image = %s", got)
	}
}

func TestObserveExecutableImageRequiresExactLoadedUnit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, configPath := serviceFixture(t)
	ctx := context.Background()
	runner := &recordingRunner{activeState: "active", mainPID: "0"}
	if got := ObserveExecutableImage(ctx, runner, executable, configPath); got != "not_installed" {
		t.Fatalf("absent unit = %s", got)
	}
	plan, err := Preview(executable, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
		t.Fatal(err)
	}
	if got := ObserveExecutableImage(ctx, runner, executable, configPath); got != "unknown" {
		t.Fatalf("unverified loaded unit = %s", got)
	}
	runner.fragmentPath = plan.UnitPath
	runner.execStart = loadedExecStartFixture(executable, configPath)
	runner.activeState = "inactive"
	if got := ObserveExecutableImage(ctx, runner, executable, configPath); got != "stopped" {
		t.Fatalf("stopped = %s", got)
	}
	runner.activeState = "activating"
	if got := ObserveExecutableImage(ctx, runner, executable, configPath); got != "unknown" {
		t.Fatalf("transitional state = %s", got)
	}
	runner.activeState = "active"
	if got := ObserveExecutableImage(ctx, runner, executable, configPath); got != "unknown" {
		t.Fatalf("unobservable process = %s", got)
	}
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] != "show" {
			t.Fatalf("observation performed lifecycle mutation: %v", call)
		}
	}
}
