package userservice

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The image is identified by inode/device, not by the unchanged ExecStart
// pathname. A replaced executable remains reachable through /proc/PID/exe.
func observeProcessImage(ctx context.Context, runner Runner, executable string) string {
	return observeProcessImageAt(ctx, runner, executable, "/proc")
}

func observeProcessImageAt(ctx context.Context, runner Runner, executable, procRoot string) string {
	pid, err := mainPID(ctx, runner)
	if err != nil {
		return "unknown"
	}
	process := filepath.Join(procRoot, strconv.Itoa(pid))
	started, err := processStart(filepath.Join(process, "stat"))
	if err != nil {
		return "unknown"
	}
	installed, err := os.Stat(executable)
	if err != nil || !installed.Mode().IsRegular() {
		return "unknown"
	}
	running, err := os.Stat(filepath.Join(process, "exe"))
	if err != nil || !running.Mode().IsRegular() {
		return "unknown"
	}
	// Refuse a stale answer if the unit, process, or installed path changed
	// while evidence was being collected. A PID alone is reusable.
	pidAgain, err := mainPID(ctx, runner)
	if err != nil || pidAgain != pid {
		return "unknown"
	}
	startedAgain, err := processStart(filepath.Join(process, "stat"))
	if err != nil || startedAgain != started {
		return "unknown"
	}
	installedAgain, err := os.Stat(executable)
	if err != nil || !os.SameFile(installed, installedAgain) {
		return "unknown"
	}
	runningAgain, err := os.Stat(filepath.Join(process, "exe"))
	if err != nil || !os.SameFile(running, runningAgain) {
		return "unknown"
	}
	active, err := serviceState(ctx, runner, "ActiveState")
	if err != nil || !active {
		return "unknown"
	}
	if os.SameFile(installed, running) {
		return "running_current_image"
	}
	return "running_stale_image"
}

func mainPID(ctx context.Context, runner Runner) (int, error) {
	output, err := runner.Output(ctx, "show", UnitName, "--property", "MainPID", "--value")
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || pid <= 1 {
		return 0, os.ErrInvalid
	}
	return pid, nil
}

func processStart(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// Field 2 (comm) may contain spaces or parentheses. The final ") "
	// precedes field 3; zero-based index 19 is field 22 (starttime).
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", os.ErrInvalid
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) <= 19 {
		return "", os.ErrInvalid
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", err
	}
	return fields[19], nil
}
