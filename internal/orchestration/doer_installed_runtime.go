//go:build linux

package orchestration

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// Installed runtime discovery is controller startup wiring, not Agent authority.
// It examines only the conventional existing library venv and public Laya cache;
// it never runs Doer, installs packages, downloads models or edits owner config.
func (w *QueueWorker) PrepareInstalledDoerRuntime(stateRoot string, adapter *hermesruntime.Adapter) error {
	if w.implementation != nil {
		return nil
	} // explicit controller configuration wins
	u, err := user.Current()
	if err != nil || u.Uid != configUID() {
		return ErrLocalLayaUnavailable
	}
	return w.prepareInstalledDoerRuntime(u.HomeDir, stateRoot, adapter)
}
func configUID() string { return strconv.Itoa(os.Geteuid()) }

var installedRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var installedAssets = []string{"rl_agent_config.json", "encoder/config.json", "tokenizer/tokenizer.json", "tokenizer/tokenizer_config.json", "model.safetensors"}

// trustedInstalledPath checks the resolved object and every ancestor. Ambient
// PATH, HF_HOME, tokens and Python search-path overrides are never consulted.
func trustedInstalledPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrLocalLayaUnavailable
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ErrLocalLayaUnavailable
	}
	for _, name := range []string{path, resolved} {
		parts := []string{}
		for p := name; ; p = filepath.Dir(p) {
			parts = append(parts, p)
			if p == "/" {
				break
			}
		}
		privateSearch := false
		for i := len(parts) - 1; i >= 0; i-- {
			p := parts[i]
			info, e := os.Stat(p)
			if e != nil || (!privateSearch && info.Mode().Perm()&0022 != 0) {
				return ErrLocalLayaUnavailable
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) {
				return ErrLocalLayaUnavailable
			}
			// As with native controller custody, an owned private search ancestor
			// makes group-writable descendants inaccessible to other host users.
			// Never infer that barrier when an access ACL is present.
			if _, e := unix.Getxattr(p, "system.posix_acl_access", nil); e == nil || (e != unix.ENODATA && e != unix.ENOTSUP) {
				return ErrLocalLayaUnavailable
			}
			if info.IsDir() && st.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0 {
				privateSearch = true
			}
		}
	}
	return nil
}
func trustedLayaExecutable(path string) error {
	if err := trustedExecutable(path); err == nil {
		return nil
	}
	// Preserve the venv spelling: resolving its Python symlink before invocation
	// would discard pyvenv.cfg and load a different environment.
	if trustedInstalledPath(path) != nil || filepath.Base(filepath.Dir(path)) != "bin" {
		return ErrLocalLayaUnavailable
	}
	if trustedInstalledPath(filepath.Join(filepath.Dir(filepath.Dir(path)), "pyvenv.cfg")) != nil {
		return ErrLocalLayaUnavailable
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ErrLocalLayaUnavailable
	}
	return nil
}
func (w *QueueWorker) prepareInstalledDoerRuntime(home, stateRoot string, adapter *hermesruntime.Adapter) error {
	if adapter == nil || !filepath.IsAbs(stateRoot) || trustedInstalledPath(stateRoot) != nil {
		return ErrLocalLayaUnavailable
	}
	venv := filepath.Join(home, ".hermes", "services", "doer", ".venv")
	python := filepath.Join(venv, "bin", "python")
	site := filepath.Join(venv, "lib", "python3.12", "site-packages")
	if trustedLayaExecutable(python) != nil {
		return ErrLocalLayaUnavailable
	}
	// Qualification is deliberately pinned to the already supported package, not
	// any executable named Doer or an arbitrary discoverable Python installation.
	if trustedInstalledPath(site) != nil {
		return ErrLocalLayaUnavailable
	}
	metadata, err := os.ReadFile(filepath.Join(site, "laya-0.3.20.dist-info", "METADATA"))
	if err != nil || !strings.Contains(string(metadata), "\nVersion: 0.3.20\n") {
		return ErrLocalLayaUnavailable
	}
	count := 0
	if err = filepath.WalkDir(site, func(path string, d os.DirEntry, err error) error {
		count++
		if err != nil || count > 150000 {
			return ErrLocalLayaUnavailable
		}
		info, e := d.Info()
		if e != nil {
			return ErrLocalLayaUnavailable
		}
		if _, e := unix.Getxattr(path, "system.posix_acl_access", nil); e == nil || (e != unix.ENODATA && e != unix.ENOTSUP) {
			return ErrLocalLayaUnavailable
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || (info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0002 != 0) {
			return ErrLocalLayaUnavailable
		}
		if (info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0020 != 0) && trustedInstalledPath(path) != nil {
			return ErrLocalLayaUnavailable
		}
		return nil
	}); err != nil {
		return ErrLocalLayaUnavailable
	}
	cache := filepath.Join(home, ".cache", "huggingface", "hub")
	repo := filepath.Join(cache, "models--convaiinnovations--laya")
	ref := filepath.Join(repo, "refs", "main")
	if trustedInstalledPath(ref) != nil {
		return ErrLocalLayaUnavailable
	}
	raw, err := os.ReadFile(ref)
	revision := strings.TrimSpace(string(raw))
	if err != nil || !installedRevision.MatchString(revision) {
		return ErrLocalLayaUnavailable
	}
	sources := make(map[string]string)
	for _, name := range installedAssets {
		source := filepath.Join(repo, "snapshots", revision, name)
		resolved, e := filepath.EvalSymlinks(source)
		// Public assets only; even a cache symlink cannot reference an auth file.
		if e != nil || trustedInstalledPath(source) != nil || (!strings.HasPrefix(resolved, repo+string(os.PathSeparator)) && !strings.HasPrefix(resolved, filepath.Join(cache, "blobs")+string(os.PathSeparator))) {
			return ErrLocalLayaUnavailable
		}
		info, e := os.Stat(resolved)
		if e != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 2<<30 {
			return ErrLocalLayaUnavailable
		}
		sources[name] = resolved
	}
	// A unique private runtime home avoids cross-controller cache writers. The
	// model is copied lazily once, immediately before the real fixed helper probe.
	parent := filepath.Join(stateRoot, "runtime")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if trustedInstalledPath(parent) != nil {
		return ErrLocalLayaUnavailable
	}
	privateHome, err := os.MkdirTemp(parent, "doer-local-")
	if err != nil {
		return err
	}
	process := &installedLayaProcess{python: python, home: privateHome, revision: revision, sources: sources}
	w.implementation = &ImplementationController{managedLocalDoer: true, config: config.Implementation{LayaPython: python, LayaHome: privateHome}, root: filepath.Join(stateRoot, "persistence", "fleet-v1"), adapter: adapter, decision: NewLayaDecisionAdapter(process)}
	w.doerProtectedPaths = append(w.doerProtectedPaths, venv, cache, privateHome)
	return nil
}

type installedLayaProcess struct {
	once                   sync.Once
	err                    error
	python, home, revision string
	sources                map[string]string
}

func (p *installedLayaProcess) Run(ctx context.Context, input []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.once.Do(func() { p.err = p.prepare() })
	if p.err != nil {
		return nil, ErrLocalLayaUnavailable
	}
	if trustedLayaExecutable(p.python) != nil {
		return nil, ErrLocalLayaUnavailable
	}
	return (LocalLayaProcess{PythonExecutable: p.python, Home: p.home}).Run(ctx, input)
}
func (p *installedLayaProcess) prepare() error {
	repo := filepath.Join(p.home, ".cache", "huggingface", "hub", "models--convaiinnovations--laya")
	for _, name := range installedAssets {
		source := p.sources[name]
		if trustedInstalledPath(source) != nil {
			return ErrLocalLayaUnavailable
		}
		target := filepath.Join(repo, "snapshots", p.revision, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, (2<<30)+1))
		closeErr := out.Close()
		in.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("private Laya asset copy failed")
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "refs"), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repo, "refs", "main"), []byte(p.revision), 0600)
}
