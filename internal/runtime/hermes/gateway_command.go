package hermes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

var errGatewayCommand = errors.New("Hermes installation runtime command unavailable or changed")

// This is a launch-protocol check, NOT provider transport qualification. Only
// the explicitly supported POSIX installation command shape is accepted. Never
// evaluate returned shell text or strip bootstrap/dependency leasing from argv.
type gatewayLaunch struct {
	argv       []string
	modern     bool
	sourceRoot string
	sourceHash string
	identities map[string]launchIdentity
}
type launchIdentity struct {
	info     os.FileInfo
	hash     [32]byte
	entry    os.FileInfo
	resolved string
}

// The supported launcher's Python literal uses repr. Restrict paths to this
// unambiguous subset rather than attempting to interpret arbitrary Python/shell.
var launchPath = regexp.MustCompile(`^/[A-Za-z0-9_./ +:@-]+$`)

func gatewayBootstrap(root string) string {
	return installedBootstrap(root, "runpy.run_module('tui_gateway.entry', run_name='__main__', alter_sys=True)")
}

func installedBootstrap(root, entry string) string {
	return "import os, sys, runpy; " +
		"os.environ.pop('PYTHONHOME', None); os.environ.pop('PYTHONPATH', None); " +
		"os.environ.pop('VIRTUAL_ENV', None); " +
		"sys.path.insert(0, '" + root + "'); " +
		"sys._hermes_installed_dependencies = True; sys.dont_write_bytecode = True; " +
		"import hermes_bootstrap; " + entry
}

// Python repr for fixed ASCII controller programs, not caller-owned code.
func controllerCodeLiteral(code string) string {
	quote := "'"
	if strings.Contains(code, quote) && !strings.Contains(code, `"`) {
		quote = `"`
	}
	const slash = `\`
	return quote + strings.NewReplacer(slash, slash+slash, quote, slash+quote,
		string(rune(10)), slash+"n", string(rune(13)), slash+"r", string(rune(9)), slash+"t").Replace(code) + quote
}

func readLaunchIdentity(path string) (launchIdentity, error) {
	entry, err := os.Lstat(path)
	if err != nil {
		return launchIdentity{}, errGatewayCommand
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return launchIdentity{}, errGatewayCommand
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return launchIdentity{}, errGatewayCommand
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return launchIdentity{}, errGatewayCommand
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, (128<<20)+1))
	after, statErr := os.Stat(path)
	entryAfter, entryErr := os.Lstat(path)
	resolvedAfter, resolveErr := filepath.EvalSymlinks(path)
	if err != nil || statErr != nil || entryErr != nil || resolveErr != nil || resolved != resolvedAfter || !os.SameFile(entry, entryAfter) || n > 128<<20 || !os.SameFile(info, after) || info.Size() != after.Size() || info.ModTime() != after.ModTime() {
		return launchIdentity{}, errGatewayCommand
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return launchIdentity{info: info, hash: digest, entry: entry, resolved: resolved}, nil
}

func (g gatewayLaunch) fresh() error {
	if g.sourceRoot != "" {
		hash, err := qualificationTree(g.sourceRoot, false, "")
		if err != nil || hash != g.sourceHash {
			return errGatewayCommand
		}
	}
	for path, previous := range g.identities {
		current, err := readLaunchIdentity(path)
		if err != nil || previous.resolved != current.resolved || !os.SameFile(previous.entry, current.entry) || !os.SameFile(previous.info, current.info) || previous.info.Mode() != current.info.Mode() || previous.info.ModTime() != current.info.ModTime() || previous.hash != current.hash {
			return errGatewayCommand
		}
	}
	return nil
}

// A modern source tree without its published launcher is unavailable, not a
// reason to execute a stale legacy venv. Older installations keep their exact
// legacy path. Dependency generation selection remains owned by PM bootstrap.
func readCommandFile(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errGatewayCommand
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errGatewayCommand
	}
	return data, nil
}

func discoverGatewayCommand(ctx context.Context, d core.RuntimeDescriptor, home string) (gatewayLaunch, error) {
	return discoverRuntimeCommand(ctx, d, home, "", nil)
}

func discoverRuntimeCommand(ctx context.Context, d core.RuntimeDescriptor, home, code string, args []string) (gatewayLaunch, error) {
	g := gatewayLaunch{identities: map[string]launchIdentity{}}
	marker := filepath.Join(d.Installation, "hermes_cli", "_launchers.py")
	markerBytes, markerErr := readCommandFile(marker, 2<<20)
	if errors.Is(markerErr, os.ErrNotExist) || (markerErr == nil && len(markerBytes) <= 2<<20 && !bytes.Contains(markerBytes, []byte("def print_runtime_command("))) {
		python := gatewayPython(d)
		if python == "" {
			return g, errGatewayCommand
		}
		g.argv = []string{python, "-m", "tui_gateway.entry"}
		if code != "" {
			g.argv = append([]string{python, "-I", "-B", "-c", code}, args...)
		}
		return g, nil
	} else if markerErr != nil || len(markerBytes) > 2<<20 {
		return g, errGatewayCommand
	}
	root, err := filepath.EvalSymlinks(d.Installation)
	if err != nil || !launchPath.MatchString(root) {
		return g, errGatewayCommand
	}
	launcher := filepath.Join(root, ".hermes", "bin", "hermes")
	// Reviewed launch/dependency selection sources and the exact declared
	// ACTIVATION_INPUTS/_SYNC_INPUTS. This is not recursive source/dependency
	// qualification or attestation of the generation selected at runtime.
	paths := []string{launcher, marker}
	for _, name := range []string{
		"hermes_bootstrap.py", "hermes_constants.py",
		"hermes_cli/venv_sync.py", "hermes_cli/runtime_state.py",
		"pm/__init__.py", "pm/environments.py", "pm/paths.py", "pm/filesystem.py",
		"pm/installed_activation.py", "hermes_cli/_early_recovery.py", "hermes_cli/update_lock.py",
		"pyproject.toml", "setup.py", "uv.lock", "pm/lock.json",
	} {
		paths = append(paths, filepath.Join(root, name))
	}
	for _, path := range paths {
		id, err := readLaunchIdentity(path)
		if err != nil {
			return g, err
		}
		g.identities[path] = id
	}
	data, err := readCommandFile(launcher, 64<<10)
	if err != nil || len(data) > 64<<10 {
		return g, errGatewayCommand
	}
	// The public POSIX launcher begins with an absolute store/developer Python.
	// Accept its canonical unquoted path or shlex's single-quoted space path only.
	first, _, _ := strings.Cut(strings.TrimPrefix(string(data), "#!/bin/sh\nexec "), " -I -c ")
	python := strings.Trim(first, "'")
	if !strings.HasPrefix(string(data), "#!/bin/sh\nexec ") || !launchPath.MatchString(python) || (first != python && first != "'"+python+"'") {
		return g, errGatewayCommand
	}
	id, err := readLaunchIdentity(python)
	if err != nil || id.info.Mode().Perm()&0111 == 0 {
		return g, errGatewayCommand
	}
	g.identities[python] = id
	g.sourceRoot = root
	g.sourceHash, err = qualificationTree(root, false, "")
	if err != nil {
		return g, errGatewayCommand
	}
	metadata, err := installedLaunchMetadata(root, python)
	if err != nil {
		return g, errGatewayCommand
	}
	for _, path := range metadata {
		id, err := readLaunchIdentity(path)
		if err != nil {
			return g, errGatewayCommand
		}
		g.identities[path] = id
	}
	query := []string{"--module", "tui_gateway.entry"}
	expected := []string{python, "-I", "-c", gatewayBootstrap(root)}
	if code != "" {
		query = append([]string{"--code", code, "--"}, args...)
		expected = append([]string{python, "-I", "-c", installedBootstrap(root, "exec("+controllerCodeLiteral(code)+")")}, args...)
	}
	output, err := queryRuntimeCommand(ctx, launcher, home, query...)
	if err != nil {
		return g, err
	}
	if err = json.Unmarshal(output, &g.argv); err != nil || !slices.Equal(g.argv, expected) {
		return g, errGatewayCommand
	}
	if err = g.fresh(); err != nil {
		return g, err
	}
	g.modern = true
	if code != "pass" {
		// Querying argv validates the launcher owner, not activation. Execute a
		// credential-free public code command to validate the committed manifest,
		// ABI and actual lease before any native auth file can be opened.
		probe, err := discoverRuntimeCommand(ctx, d, home, "pass", nil)
		if err != nil {
			return g, err
		}
		output, status, err := runBoundedRuntime(ctx, probe, home, home, 1024)
		if err != nil {
			return g, err
		}
		if status != 0 || len(output) != 0 {
			return g, errGatewayCommand
		}
		if err = g.fresh(); err != nil {
			return g, err
		}
	}
	return g, nil
}

type commandOutput struct {
	// Do not embed Buffer: its promoted ReadFrom lets io.Copy (including
	// os/exec's stdout copier) bypass Write and the retained-byte budget.
	mutex    sync.Mutex
	limit    int
	buffer   bytes.Buffer
	overflow bool
}

func (b *commandOutput) discard() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.overflow = true
	clear(b.buffer.Bytes())
	b.buffer.Reset()
}

func (b *commandOutput) Len() int      { return b.buffer.Len() }
func (b *commandOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	n := len(p)
	limit := b.limit
	if limit == 0 {
		limit = 16 << 10
	}
	if b.overflow || n > limit-b.Len() {
		b.overflow = true
		return n, nil // Drain without retaining; the query fails closed afterward.
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func queryRuntimeCommand(ctx context.Context, launcher, home string, entry ...string) ([]byte, error) {
	queryHome, err := os.MkdirTemp(home, "command-")
	if err != nil {
		return nil, errGatewayCommand
	}
	removeHome := true
	defer func() {
		if removeHome {
			_ = os.RemoveAll(queryHome)
		}
	}()
	home = queryHome
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if len(entry) == 0 {
		entry = []string{"--module", "tui_gateway.entry"}
	}
	cmd := exec.CommandContext(ctx, launcher, append([]string{"--print-runtime-command", "--installed-dependencies"}, entry...)...)
	cmd.Dir = home
	// No native profile, credentials, proxy, Python override or PM override.
	cmd.Env = []string{"HOME=" + home, "HERMES_HOME=" + home, "TMPDIR=" + home, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1", "HERMES_SKIP_VERSION_CHECK=1", "HERMES_ENABLE_PROJECT_PLUGINS=false", "HERMES_DISABLE_LAZY_INSTALLS=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output commandOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := prepareAttemptProcess(); err != nil {
		return nil, errGatewayCommand
	}
	if err := cmd.Start(); err != nil {
		return nil, errGatewayCommand
	}
	exited := make(chan error, 1)
	stop := watchAttemptProcess(cmd, exited)
	select {
	case <-ctx.Done():
	case <-exited:
	}
	stop()
	if err := stopOwnedAttemptProcess(cmd, io.NopCloser(strings.NewReader(""))); err != nil {
		removeHome = false
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.overflow || cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		return nil, errGatewayCommand
	}
	return output.Bytes(), nil
}

// Private controller-authored policy, never a copied profile or invented model
// capacity. max_turns is an iteration bound, not a one-request HTTP firewall:
// Hermes may issue bounded correction/summary requests. Ordinary model metadata
// lookup remains distinct from the closed ProviderFormatProbe contract.
func writeDisposableTurnConfig(home, provider, model string) error {
	config := map[string]any{
		"model":         map[string]any{"provider": provider, "default": model},
		"agent":         map[string]any{"max_turns": 1, "api_max_retries": 1, "auto_recovery_cycles": 0},
		"auxiliary":     map[string]any{"title_generation": map[string]any{"enabled": false, "model_upgrade_enabled": false}},
		"updates":       map[string]any{"check": false},
		"model_catalog": map[string]any{"enabled": false, "excluded_providers": []string{provider}},
		"plugins":       map[string]any{"enabled": []string{}}, "mcp_servers": map[string]any{},
		"auth":        map[string]any{"adopt_external_logins": false},
		"memory":      map[string]any{"memory_enabled": false, "user_profile_enabled": false},
		"compression": map[string]any{"enabled": false},
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(home, "config.yaml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}
