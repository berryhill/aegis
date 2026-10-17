package update

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Prepared binds a checksum-verified official release candidate to one exact
// executable and parent directory. Fields are intentionally not caller mutable.
// It is single-use and must not be used concurrently.
type Prepared struct {
	requireProvenance bool
	result            Result
	target            string
	targetInfo        os.FileInfo
	targetDigest      [sha256.Size]byte
	directory         string
	directoryInfo     os.FileInfo
	candidate         string
	candidateInfo     os.FileInfo
	candidateDigest   [sha256.Size]byte
	consumed          bool
	beforeCommit      func() // deterministic package-test seam; nil in production
}

func snapshot(path string) (os.FileInfo, [sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	info, err := os.Lstat(path)
	if err != nil {
		return nil, zero, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return nil, zero, errors.New("executable is not a regular executable file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, zero, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, zero, errors.New("executable changed during inspection")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, zero, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() {
		return nil, zero, errors.New("executable changed during inspection")
	}
	return info, sha256.Sum256(data), nil
}

func (p *Prepared) bindTarget(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("exact absolute executable path required")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return errors.New("symlinked executable path denied")
	}
	p.target = path
	p.directory = filepath.Dir(path)
	p.directoryInfo, err = os.Lstat(p.directory)
	if err != nil {
		return err
	}
	p.targetInfo, p.targetDigest, err = snapshot(path)
	return err
}

func (p *Prepared) stage(binary []byte) error {
	f, err := os.CreateTemp(p.directory, ".aegis-update-*")
	if err != nil {
		return err
	}
	p.candidate = f.Name()
	p.candidateInfo, err = f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(binary); err == nil {
		err = f.Chmod(p.targetInfo.Mode().Perm())
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	p.candidateInfo, p.candidateDigest, err = snapshot(p.candidate)
	if err == nil && p.candidateDigest != sha256.Sum256(binary) {
		return errors.New("candidate content changed during preparation")
	}
	return err
}

// Validate repeats identity, content, mode and path admission before stop and
// again immediately before replacement. Cancellation never authorizes mutation.
func (p *Prepared) Validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.consumed || p.targetInfo == nil {
		return errors.New("prepared upgrade unavailable or consumed")
	}
	resolved, err := filepath.EvalSymlinks(p.target)
	if err != nil || resolved != p.target {
		return errors.New("target path changed")
	}
	dir, err := os.Lstat(p.directory)
	if err != nil || !os.SameFile(p.directoryInfo, dir) {
		return errors.New("installation directory changed")
	}
	info, digest, err := snapshot(p.target)
	if err != nil || !os.SameFile(p.targetInfo, info) || p.targetInfo.Mode() != info.Mode() || digest != p.targetDigest {
		return errors.New("target executable changed")
	}
	if p.candidate != "" {
		info, digest, err = snapshot(p.candidate)
		if err != nil || !os.SameFile(p.candidateInfo, info) || p.candidateInfo.Mode() != info.Mode() || digest != p.candidateDigest {
			return errors.New("prepared candidate changed")
		}
	}
	return ctx.Err()
}

func (p *Prepared) Install(ctx context.Context) (Result, error) {
	r := p.result
	if err := p.Validate(ctx); err != nil {
		return r, err
	}
	if p.candidate == "" {
		p.consumed = true
		return r, nil
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if p.beforeCommit != nil {
		p.beforeCommit()
	}
	// Pin the destination directory before final admission. Relative rename
	// cannot be redirected through a substituted ancestor after this point.
	root, err := os.OpenRoot(p.directory)
	if err != nil {
		return r, err
	}
	defer root.Close()
	directoryInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(p.directoryInfo, directoryInfo) {
		return r, errors.New("installation directory changed at commit")
	}
	if err := p.Validate(ctx); err != nil {
		return r, err
	}
	// Rename has no inode-conditional compare-and-swap contract. This narrows
	// but does not eliminate races by a hostile writer with the same OS UID.
	if err := root.Rename(filepath.Base(p.candidate), filepath.Base(p.target)); err != nil {
		return r, fmt.Errorf("replace executable: %w", err)
	}
	p.consumed = true
	p.candidate = ""
	r.Updated = true
	dir, err := os.Open(p.directory)
	if err != nil {
		return r, err
	}
	defer dir.Close()
	return r, dir.Sync()
}

func (p *Prepared) Close() {
	if p.candidate != "" {
		// Remove only our exact staged inode, never an attacker-substituted file.
		if info, err := os.Lstat(p.candidate); err == nil && p.candidateInfo != nil && os.SameFile(info, p.candidateInfo) {
			_ = os.Remove(p.candidate)
		}
	}
	p.consumed = true
}
