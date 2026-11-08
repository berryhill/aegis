package implementation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/loop"
)

// EngineeringInventory is read-only repository task data, not a write grant.
// Every admitted Go file is named and hashed; excess inventory fails closed.
type EngineeringInventory struct {
	Files  []loop.RepositoryContextFile `json:"files"`
	Digest string                       `json:"digest"`
}

func CollectEngineeringInventory(ctx context.Context, repository string, policy loop.IssueEngineeringPolicy) (EngineeringInventory, error) {
	var result EngineeringInventory
	reject := errors.New("bounded engineering inventory rejected")
	if ctx == nil || policy.Validate() != nil || !filepath.IsAbs(repository) {
		return result, reject
	}
	resolved, err := filepath.EvalSymlinks(repository)
	if err != nil || resolved != repository {
		return result, reject
	}
	root, err := os.OpenRoot(repository)
	if err != nil {
		return result, reject
	}
	defer root.Close()
	seen := map[string]bool{}
	visited := 0
	for _, prefix := range append(append([]string{}, policy.WritablePrefixes...), policy.WritableAssets...) {
		err = fs.WalkDir(root.FS(), prefix, func(name string, entry fs.DirEntry, walkErr error) error {
			visited++
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if walkErr != nil || visited > 32768 {
				return reject
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return reject
			}
			if entry.IsDir() {
				return nil
			}
			if !policy.Allows(name) || seen[name] {
				return nil
			}
			if len(result.Files) >= 8192 {
				return reject
			}
			info, e := entry.Info()
			if e != nil || !info.Mode().IsRegular() || info.Size() > loop.MaxRepositoryContextBytes {
				return reject
			}
			b, e := readRepositoryContextFile(root, name, info.Size())
			if e != nil {
				return reject
			}
			seen[name] = true
			result.Files = append(result.Files, loop.RepositoryContextFile{Path: name, Bytes: int64(len(b)), Digest: digest(b), Role: "source"})
			return nil
		})
		if err != nil {
			return EngineeringInventory{}, err
		}
	}
	if len(result.Files) == 0 {
		return result, reject
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	result.Digest = RepositorySourceDigest(&loop.RepositoryContext{Files: result.Files})
	return result, nil
}

// ReadEngineeringSelection reloads exact selected preimages after a managed
// read-plan turn. No arbitrary paths, partial result or ambient context is exposed.
func ReadEngineeringSelection(ctx context.Context, repository string, inventory EngineeringInventory, paths []string) ([]RepositoryContextText, error) {
	reject := errors.New("bounded engineering read selection rejected")
	if len(paths) == 0 || len(paths) > 64 || inventory.Digest != RepositorySourceDigest(&loop.RepositoryContext{Files: inventory.Files}) {
		return nil, reject
	}
	root, err := os.OpenRoot(repository)
	if err != nil {
		return nil, reject
	}
	defer root.Close()
	entries := map[string]loop.RepositoryContextFile{}
	for _, f := range inventory.Files {
		entries[f.Path] = f
	}
	seen := map[string]bool{}
	var result []RepositoryContextText
	var total int64
	for _, p := range paths {
		f, ok := entries[p]
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !ok || seen[p] || f.Bytes > loop.MaxRepositoryContextBytes-total {
			return nil, reject
		}
		seen[p] = true
		b, e := readRepositoryContextFile(root, p, f.Bytes)
		if e != nil || digest(b) != f.Digest {
			return nil, reject
		}
		if !utf8.Valid(b) || strings.Contains(string(b), "PRIVATE KEY-----") {
			return nil, reject
		}
		for _, r := range string(b) {
			if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				return nil, reject
			}
		}
		total += int64(len(b))
		result = append(result, RepositoryContextText{Path: p, Role: "source", Digest: f.Digest, Text: string(b)})
	}
	return result, nil
}
