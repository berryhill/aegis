package implementation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// ApplyDoerEdits applies model-proposed bytes through the controller's exact
// allowlist. The contract is policy data, not authority: admit must freshly
// resolve authority for each file. This is not a host filesystem sandbox.
func ApplyDoerEdits(ctx context.Context, c loop.DoerContract, edits []Edit, admit Admit) error {
	if ctx == nil || admit == nil {
		return errors.New("Doer context and admission required")
	}
	if _, err := c.Digest(); err != nil {
		return err
	}
	if len(edits) == 0 || len(edits) > len(c.WritableFiles) {
		return errors.New("bounded nonempty Doer edits required")
	}
	allowed := make(map[string]bool, len(c.WritableFiles))
	for _, name := range c.WritableFiles {
		allowed[name] = true
	}
	seen := make(map[string]bool, len(edits))
	size := 0
	for _, edit := range edits {
		if !allowed[edit.Path] || seen[edit.Path] {
			return errors.New("edit outside Doer allowlist or duplicate path")
		}
		seen[edit.Path] = true
		if len(edit.Content) > maxBytes-size {
			return errors.New("Doer edit content exceeded limit")
		}
		size += len(edit.Content)
	}
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return errors.New("resolved Doer workspace required")
	}
	before, err := os.Lstat(c.Workspace)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return errors.New("Doer workspace identity unavailable")
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return errors.New("Doer workspace changed during admission")
	}
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := applyOneDoerEdit(ctx, root, edit, admit); err != nil {
			return err
		}
	}
	return nil
}

func applyOneDoerEdit(ctx context.Context, root *os.Root, edit Edit, admit Admit) error {
	// Open and pin one directory at a time. Verify each path component before
	// descent; os.Root bounds traversal even if a concurrent replacement occurs.
	parent := root
	var owned []*os.Root
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].Close()
		}
	}()
	parts := strings.Split(edit.Path, "/")
	for _, part := range parts[:len(parts)-1] {
		info, err := parent.Lstat(part)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Doer edit parent must be a real directory")
		}
		child, err := parent.OpenRoot(part)
		if err != nil {
			return err
		}
		actual, err := child.Stat(".")
		if err != nil || !os.SameFile(info, actual) {
			child.Close()
			return errors.New("Doer edit parent changed during traversal")
		}
		owned = append(owned, child)
		parent = child
	}
	name := parts[len(parts)-1]
	existing, err := parent.Lstat(name)
	if err == nil {
		if !existing.Mode().IsRegular() || !singleLink(existing) {
			return errors.New("Doer edit target must be a single-link regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := admit(ctx, "write:"+edit.Path); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temp := ".aegis-doer-edit-" + hex.EncodeToString(nonce[:])
	file, err := parent.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer parent.Remove(temp)
	_, writeErr := file.Write(edit.Content)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Recheck the target before replacing it. Rename replaces the directory
	// entry rather than following a final symlink introduced in a race.
	current, err := parent.Lstat(name)
	if err == nil {
		if !current.Mode().IsRegular() || !singleLink(current) {
			return errors.New("Doer edit target changed to nonregular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := parent.Rename(temp, name); err != nil {
		return fmt.Errorf("rename Doer edit: %w", err)
	}
	dir, err := parent.Open(".")
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr = dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
