package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/testprocess"
)

// Share only immutable executable bytes, never a path, config, store or service.
// Each client owns an independently writable/deletable copy in its TempDir.
var installedLoopQueueExecutable = sync.OnceValues(func() (string, error) {
	return buildInstalledLoopQueueExecutable(testprocess.CombinedOutput)
})

func buildInstalledLoopQueueExecutable(run func(*exec.Cmd, time.Duration) ([]byte, error)) (string, error) {
	root, err := os.MkdirTemp("", "aegis-loop-queue-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(root)
	binary := filepath.Join(root, "aegis")
	build := exec.Command("go", "build", "-p=1", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-loop-queue-fixture", "-o", binary, "./cmd/aegis")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, err := run(build, 2*time.Minute); err != nil {
		return "", fmt.Errorf("build: %w %s", err, out)
	}
	raw, err := os.ReadFile(binary)
	return string(raw), err
}

func installedLoopQueueBinary(root, candidate string, built func() (string, error)) (string, error) {
	// Explicit frozen candidates bypass the cache, including failed validation.
	// Never overwrite them or silently substitute a separately rebuilt candidate.
	if candidate != "" {
		info, err := os.Stat(candidate)
		if !filepath.IsAbs(candidate) || err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("invalid frozen test executable")
		}
		return candidate, nil
	}
	raw, err := built()
	if err != nil {
		return "", err
	}
	binary := filepath.Join(root, "aegis")
	if err := os.WriteFile(binary, []byte(raw), 0700); err != nil {
		return "", err
	}
	return binary, nil
}
