package loop

import (
	"errors"
	"path/filepath"
	"strings"
)

// RepositoryEngineeringSchema is deliberately separate from the historical
// non-test implementation contract. Regression tests are editable proposals;
// required acceptance remains in disjoint immutable trusted_test files.
const RepositoryEngineeringSchema = "aegis.loop.repository-engineering.v1"
const IssueEngineeringSchema = "aegis.contribution.issue-engineering.v1"
const EngineeringExecutionKind = "go-repository-engineering.v1"

// IssueEngineeringPolicy bounds untrusted issue scope. It grants no effects.
// Paths are directory prefixes, not globs, commands or filesystem discovery.
type IssueEngineeringPolicy struct {
	WritableAssets   []string `json:"writable_assets,omitempty"`
	WritablePrefixes []string `json:"writable_prefixes"`
	MaxWritableFiles uint16   `json:"max_writable_files"`
}

func (p IssueEngineeringPolicy) Validate() error {
	if p.MaxWritableFiles < 2 || p.MaxWritableFiles > 64 || len(p.WritablePrefixes) == 0 || len(p.WritablePrefixes) > 32 {
		return errors.New("bounded engineering scope required")
	}
	if len(p.WritableAssets) > 64 {
		return errors.New("bounded asset scope required")
	}
	seen := map[string]bool{}
	for _, asset := range p.WritableAssets {
		if !SafeRepositoryContextPath(asset) || !EngineeringTextAsset(asset) || seen[asset] {
			return errors.New("closed text asset scope required")
		}
		seen[asset] = true
	}
	for _, prefix := range p.WritablePrefixes {
		if !filepath.IsLocal(prefix) || filepath.Clean(prefix) != prefix || strings.ContainsAny(prefix, "\\\x00\r\n\t") || strings.HasPrefix(prefix, ".") || prefix == "vendor" || strings.HasPrefix(prefix, "vendor/") || seen[prefix] {
			return errors.New("local engineering directory prefixes required")
		}
		seen[prefix] = true
	}
	return nil
}
func (p IssueEngineeringPolicy) Allows(name string) bool {
	if !SafeRepositoryContextPath(name) || (!strings.HasSuffix(name, ".go") && !EngineeringTextAsset(name)) {
		return false
	}
	for _, asset := range p.WritableAssets {
		if asset == name {
			return true
		}
	}
	for _, prefix := range p.WritablePrefixes {
		if strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}

// DependencyBundle is an independently approved tar of a Go file-module proxy.
// Its complete bytes, not ambient caches or network access, bind dependency input.
type DependencyBundle struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

func (b DependencyBundle) Validate() error {
	if !filepath.IsAbs(b.Path) || filepath.Clean(b.Path) != b.Path || !contextDigestPattern.MatchString(b.Digest) {
		return errors.New("exact offline dependency bundle required")
	}
	return nil
}
