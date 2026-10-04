package orchestration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

var errProtectedDoerWorkspace = errors.New("protected Doer workspace")

// validateProtectedDoerWorkspace checks controller-owned protection scopes.
// It reads filesystem metadata only. It is not a sandbox or a substitute for
// fresh admission and race-resistant file access at every runtime effect.
func validateProtectedDoerWorkspace(c loop.DoerContract, protectedPaths []string) error {
	deny := func(reason string) error {
		return fmt.Errorf("%w: %s", errProtectedDoerWorkspace, reason)
	}
	validAbsolute := func(p string) bool {
		return filepath.IsAbs(p) && filepath.Clean(p) == p &&
			filepath.Dir(p) != p && !strings.ContainsRune(p, '\x00')
	}
	// Rel checks components, not textual prefixes: /state2 is not /state.
	within := func(p, scope string) bool {
		rel, err := filepath.Rel(scope, p)
		return err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	// Preserve a missing suffix while resolving its existing ancestors. This
	// protects future files below symlinked directories as well as existing
	// targets. Dangling symlinks fail closed rather than becoming lexical paths.
	resolve := func(p string) (string, error) {
		current := p
		var suffix []string
		for {
			resolved, err := filepath.EvalSymlinks(current)
			if err == nil {
				for i := len(suffix) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, suffix[i])
				}
				return resolved, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			info, statErr := os.Lstat(current)
			if statErr == nil {
				// An existing entry whose resolution failed is not a safe
				// missing path (in particular, a dangling symlink).
				if info.Mode()&os.ModeSymlink != 0 {
					return "", fmt.Errorf("unresolvable symlink %q: %w", current, err)
				}
				return "", err
			}
			if !errors.Is(statErr, os.ErrNotExist) {
				return "", statErr
			}
			parent := filepath.Dir(current)
			if parent == current {
				return "", err
			}
			suffix = append(suffix, filepath.Base(current))
			current = parent
		}
	}
	if len(protectedPaths) == 0 {
		return deny("controller protection scopes are required")
	}
	if !validAbsolute(c.Workspace) {
		return deny("workspace must be absolute, clean and nonroot")
	}
	workspace, err := resolve(c.Workspace)
	if err != nil {
		return deny(fmt.Sprintf("resolve workspace: %v", err))
	}
	if !validAbsolute(workspace) {
		return deny("resolved workspace must be absolute, clean and nonroot")
	}
	type scope struct{ lexical, resolved string }
	scopes := make([]scope, 0, len(protectedPaths))
	for _, p := range protectedPaths {
		if !validAbsolute(p) {
			return deny("protected paths must be absolute, clean and nonroot")
		}
		r, err := resolve(p)
		if err != nil {
			return deny(fmt.Sprintf("resolve protected path %q: %v", p, err))
		}
		if !validAbsolute(r) {
			return deny("resolved protected paths must be absolute, clean and nonroot")
		}
		scopes = append(scopes, scope{p, r})
	}
	for _, s := range scopes {
		for _, w := range []string{c.Workspace, workspace} {
			for _, p := range []string{s.lexical, s.resolved} {
				if within(w, p) {
					return deny("workspace is at or below a protected path")
				}
			}
		}
	}
	if len(c.WritableFiles) == 0 {
		return deny("selected writable files are required")
	}
	for _, file := range c.WritableFiles {
		// DoerContract selects workspace-relative paths, never absolute paths
		// or traversal. Validate before Join can erase invalid components.
		if file == "" || file == "." || filepath.IsAbs(file) ||
			filepath.Clean(file) != file || file == ".." ||
			strings.HasPrefix(file, ".."+string(filepath.Separator)) ||
			strings.ContainsAny(file, "\\\x00\r\n") {
			return deny("invalid workspace-relative writable file")
		}
		lexical := filepath.Join(c.Workspace, file)
		target, err := resolve(lexical)
		if err != nil {
			return deny(fmt.Sprintf("resolve selected writable path: %v", err))
		}
		if !validAbsolute(lexical) || !validAbsolute(target) {
			return deny("writable targets must be absolute, clean and nonroot")
		}
		for _, s := range scopes {
			for _, w := range []string{lexical, target} {
				for _, p := range []string{s.lexical, s.resolved} {
					// An ancestor write could replace a protected directory.
					// An ancestor workspace alone is harmless and is allowed.
					if within(w, p) || within(p, w) {
						return deny("selected writable path intersects a protected path")
					}
				}
			}
		}
	}
	return nil
}
