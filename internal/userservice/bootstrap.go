package userservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DiscoverInstalledExecutable treats the unit only as an untrusted locator.
// Full deterministic installed and loaded plan validation grants eligibility.
// No caller-supplied executable path is accepted.
func DiscoverInstalledExecutable(ctx context.Context, runner Runner, configPath string) (string, error) {
	dir, err := userUnitDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, UnitName)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return "", ErrForeignUnit
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var executable string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		if executable != "" {
			return "", ErrForeignUnit
		}
		token, _, ok := strings.Cut(strings.TrimPrefix(line, "ExecStart="), " serve --config ")
		if !ok {
			return "", ErrForeignUnit
		}
		executable, err = strconv.Unquote(token)
		if err != nil {
			return "", ErrForeignUnit
		}
	}
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return "", errors.New("exact installed gateway executable unavailable")
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil || resolved != executable {
		return "", ErrForeignUnit
	}
	plan, err := Preview(executable, configPath)
	if err != nil {
		return "", err
	}
	if err := PreflightStop(ctx, runner, plan); err != nil {
		return "", err
	}
	return executable, nil
}
