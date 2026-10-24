package loop

import (
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
)

// RepositoryContext is explicit untrusted task data, never authority. SourceDigest
// is an exact controller-owned source-state commitment, not a mutable branch name.
// Its canonical digest is the context identity; file order is significant.
const MaxRepositoryContextBytes = 8 << 20
const MaxRepositoryContextFiles = 128

type RepositoryContext struct {
	TaskDigest    string                  `json:"task_digest"`
	SourceDigest  string                  `json:"source_digest"`
	MaxFiles      uint16                  `json:"max_files"`
	MaxFileBytes  int64                   `json:"max_file_bytes"`
	MaxTotalBytes int64                   `json:"max_total_bytes"`
	Files         []RepositoryContextFile `json:"files"`
}

type RepositoryContextFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	Digest string `json:"digest"`
	Role   string `json:"role"`
}

var contextDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// SafeRepositoryContextPath deliberately limits this Go-context slice to explicit
// source/test files. Denied names must never be opened or echoed in diagnostics.
func SafeRepositoryContextPath(p string) bool {
	if p == "" || len(p) > 4096 || strings.ContainsAny(p, "\\\x00\r\n	:") || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || (!strings.HasSuffix(p, ".go") && p != "go.mod") {
		return false
	}
	for _, part := range strings.Split(strings.ToLower(p), "/") {
		if strings.HasPrefix(part, ".") || strings.Contains(part, "credential") || strings.Contains(part, "secret") || strings.Contains(part, "private") || strings.HasPrefix(part, "id_rsa") || strings.HasPrefix(part, "id_ed25519") {
			return false
		}
	}
	return true
}

func (m RepositoryContext) Validate(task string, writable []string) error {
	reject := errors.New("repository context rejected")
	if m.TaskDigest != sha256Digest([]byte(task)) || !contextDigestPattern.MatchString(m.SourceDigest) || m.MaxFiles < 1 || m.MaxFiles > MaxRepositoryContextFiles || m.MaxFileBytes < 1 || m.MaxFileBytes > MaxRepositoryContextBytes || m.MaxTotalBytes < 1 || m.MaxTotalBytes > MaxRepositoryContextBytes || m.MaxFileBytes > m.MaxTotalBytes || len(m.Files) == 0 || len(m.Files) > int(m.MaxFiles) {
		return reject
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if !SafeRepositoryContextPath(f.Path) || seen[f.Path] || f.Bytes < 0 || f.Bytes > m.MaxFileBytes || f.Bytes > m.MaxTotalBytes-total || !contextDigestPattern.MatchString(f.Digest) {
			return reject
		}
		if f.Path == "go.mod" {
			if f.Role != "checker_input" {
				return reject
			}
		} else if f.Role != "source" && f.Role != "trusted_test" {
			return reject
		}
		if strings.HasSuffix(f.Path, "_test.go") != (f.Role == "trusted_test") {
			return reject
		}
		if f.Role == "trusted_test" {
			for _, p := range writable {
				if p == f.Path {
					return reject
				}
			}
		}
		seen[f.Path] = true
		total += f.Bytes
	}
	return nil
}

// Digest binds provenance, budgets and every explicit file preimage. Callers
// validate the manifest against the task and write allowlist before collection.
func (m RepositoryContext) Digest() (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return sha256Digest(b), nil
}
