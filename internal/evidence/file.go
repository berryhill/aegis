package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	SelectedFilePolicyV1 = "aegis.dev/selected-file/v1"
	SelectedFileMaxBytes = 1 << 20
)

type SelectedFileMode string

const (
	SelectedFileText     SelectedFileMode = "trimmed-utf8-equals"
	SelectedFilePresence SelectedFileMode = "regular-file-present"
)

// SelectedFilePolicy is a controller-pinned, single-file assertion. Digest must
// be retained before runtime execution and supplied separately at verification.
// No glob, command, directory assertion, or other-file proof is implied.
type SelectedFilePolicy struct {
	Version      string           `json:"version"`
	RelativePath string           `json:"relative_path"`
	Mode         SelectedFileMode `json:"mode"`
	Text         string           `json:"text,omitempty"`
}

// SelectedFileBinding is supplied by the controller at verification, not by
// the immutable Loop definition or the model. It does not change policy digest.
type SelectedFileBinding struct {
	AttemptID              string
	ActionID               string
	RunID                  string
	OwnerID                string
	AuthorityContextID     string
	AuthorityContextDigest string
}

func (b SelectedFileBinding) Validate() error {
	if b.AttemptID == "" || b.ActionID == "" || b.RunID == "" || b.OwnerID == "" || b.AuthorityContextID == "" || b.AuthorityContextDigest == "" {
		return errors.New("invalid selected-file runtime binding")
	}
	return nil
}

func (p SelectedFilePolicy) Validate() error {
	if p.Version != SelectedFilePolicyV1 {
		return errors.New("invalid selected-file policy version")
	}
	s := p.RelativePath
	if s == "" || len(s) > 256 || strings.ContainsAny(s, "\\\x00") || path.IsAbs(s) || path.Clean(s) != s || s == "." || strings.HasPrefix(s, "../") || s == ".." {
		return errors.New("invalid selected-file path")
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid selected-file path")
		}
	}
	switch p.Mode {
	case SelectedFileText:
		if !utf8.ValidString(p.Text) || len(p.Text) > SelectedFileMaxBytes {
			return errors.New("invalid selected-file expected text")
		}
	case SelectedFilePresence:
		if p.Text != "" {
			return errors.New("presence policy cannot assert text")
		}
	default:
		return errors.New("unknown selected-file mode")
	}
	return nil
}

func (p SelectedFilePolicy) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	wire, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return sha256Reference(wire), nil
}

// SelectedFileResult is an observation for an orchestrator to bind to its
// existing evidence port; it is NOT a VerificationReceipt or completion grant.
type SelectedFileResult struct {
	PolicyDigest           string
	ContentDigest          string
	Content                []byte
	RelativePath           string
	AttemptID              string
	ActionID               string
	RunID                  string
	OwnerID                string
	AuthorityContextID     string
	AuthorityContextDigest string
	Outcome                Outcome
	FailureCategory        string
}

// VerifySelectedFile never accepts a path from runtime narration. It uses the
// pinned policy and opens beneath the trusted workspace root. File data is
// bounded; no artifact or receipt is issued by this observation alone.
func VerifySelectedFile(ctx context.Context, workspace string, policy SelectedFilePolicy, pinnedDigest string, binding SelectedFileBinding) (SelectedFileResult, error) {
	if err := ctx.Err(); err != nil {
		return SelectedFileResult{}, err
	}
	if err := binding.Validate(); err != nil {
		return SelectedFileResult{}, err
	}
	digest, err := policy.Digest()
	if err != nil {
		return SelectedFileResult{}, err
	}
	if !validDigest(pinnedDigest) || digest != pinnedDigest {
		return SelectedFileResult{}, errors.New("selected-file policy differs from pinned digest")
	}
	r := SelectedFileResult{PolicyDigest: digest, RelativePath: policy.RelativePath, AttemptID: binding.AttemptID, ActionID: binding.ActionID, RunID: binding.RunID, OwnerID: binding.OwnerID, AuthorityContextID: binding.AuthorityContextID, AuthorityContextDigest: binding.AuthorityContextDigest, Outcome: Failed, FailureCategory: "file_unreadable_or_unsafe"}
	if workspace == "" || !filepath.IsAbs(workspace) {
		return r, nil
	}
	st, err := os.Lstat(workspace)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return r, nil
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return r, nil
	}
	defer func() { root.Close() }()
	openedRoot, err := root.Stat(".")
	if err != nil || !os.SameFile(st, openedRoot) {
		return r, nil
	}
	// Check every component without following links; compare opened descriptor
	// identity to the lstat observation so a replacement cannot redirect reads.
	parts := strings.Split(policy.RelativePath, "/")
	for _, part := range parts[:len(parts)-1] {
		if err := ctx.Err(); err != nil {
			return SelectedFileResult{}, err
		}
		info, e := root.Lstat(part)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return r, nil
		}
		next, e := root.OpenRoot(part)
		if e != nil {
			return r, nil
		}
		opened, e := next.Stat(".")
		if e != nil || !os.SameFile(info, opened) {
			next.Close()
			return r, nil
		}
		root.Close()
		root = next
	}
	name := parts[len(parts)-1]
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > SelectedFileMaxBytes {
		return r, nil
	}
	file, err := root.Open(name)
	if err != nil {
		return r, nil
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return r, nil
	}
	data, err := io.ReadAll(io.LimitReader(file, SelectedFileMaxBytes+1))
	if err != nil || len(data) > SelectedFileMaxBytes {
		return r, nil
	}
	if err := ctx.Err(); err != nil {
		return SelectedFileResult{}, err
	}
	r.ContentDigest = sha256Reference(data)
	if policy.Mode == SelectedFileText {
		if !utf8.Valid(data) {
			r.FailureCategory = "invalid_utf8"
			return r, nil
		}
		if strings.TrimSpace(string(data)) != strings.TrimSpace(policy.Text) {
			r.FailureCategory = "text_mismatch"
			return r, nil
		}
	}
	r.Outcome = Passed
	r.FailureCategory = ""
	r.Content = append([]byte(nil), data...)
	return r, nil
}
