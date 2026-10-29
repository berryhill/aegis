package evidence

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- Git SHA-1 object identity, never an authority signature.
	"encoding/hex"
	"strconv"
	"strings"
)

// ContributionGitObject points to raw Git commit bytes retained by the closed
// delivery producer. SHA-256 authenticates blob custody; Git's object ID binds
// those bytes to the independently observed remote commit. Neither hash alone
// establishes remote provenance, branch protection, review or merge authority.
type ContributionGitObject struct {
	OID     string `json:"oid"`
	Content string `json:"content"`
}

// ContributionMergeObjects is a verifier input contract, not a replacement
// delivery receipt. The real delivery owner must select every reference from its
// retained observed effect history. No caller-supplied contained/passed flag is
// accepted. This closed policy requires a candidate tested on the checked base.
type ContributionMergeObjects struct {
	Candidate         ContributionGitObject `json:"candidate"`
	Published         ContributionGitObject `json:"published"`
	Merged            ContributionGitObject `json:"merged"`
	TestedBase        string                `json:"tested_base"`
	CheckedBase       string                `json:"checked_base"`
	TestedTree        string                `json:"tested_tree"`
	FinalHead         string                `json:"final_head"`
	PRHead            string                `json:"pr_head"`
	ProtectedBaseHead string                `json:"protected_base_head"`
	Method            string                `json:"method"`
	// An ordered current-base first-parent chain ending at the observed merge.
	// A concurrently advanced base is permitted only with actual containment.
	Containment []ContributionGitObject `json:"containment"`
}

type contributionCommit struct {
	tree    string
	parents []string
}

func decodeContributionCommit(oid string, content []byte) (contributionCommit, error) {
	bad := ErrContributionEvidence
	if !contributionGitOID(oid) || len(content) == 0 || len(content) > 1<<20 || bytes.IndexByte(content, 0) >= 0 {
		return contributionCommit{}, bad
	}
	// Git's hash is over the canonical object header plus the raw object bytes.
	h := sha1.New() // #nosec G401 -- Compatibility with Git object IDs, not cryptographic authorization.
	_, _ = h.Write([]byte("commit " + strconv.Itoa(len(content)) + "\x00"))
	_, _ = h.Write(content)
	if hex.EncodeToString(h.Sum(nil)) != oid {
		return contributionCommit{}, bad
	}
	header, _, ok := bytes.Cut(content, []byte("\n\n"))
	if !ok {
		return contributionCommit{}, bad
	}
	out := contributionCommit{parents: []string{}}
	author, committer := false, false
	for _, line := range strings.Split(string(header), "\n") {
		switch {
		case strings.HasPrefix(line, "tree "):
			if out.tree != "" || !contributionGitOID(strings.TrimPrefix(line, "tree ")) {
				return contributionCommit{}, bad
			}
			out.tree = strings.TrimPrefix(line, "tree ")
		case strings.HasPrefix(line, "parent "):
			p := strings.TrimPrefix(line, "parent ")
			if !contributionGitOID(p) || len(out.parents) >= 2 {
				return contributionCommit{}, bad
			}
			for _, old := range out.parents {
				if old == p {
					return contributionCommit{}, bad
				}
			}
			out.parents = append(out.parents, p)
		case strings.HasPrefix(line, "author "):
			if author || len(line) <= 7 {
				return contributionCommit{}, bad
			}
			author = true
		case strings.HasPrefix(line, "committer "):
			if committer || len(line) <= 10 {
				return contributionCommit{}, bad
			}
			committer = true
		case strings.HasPrefix(line, "gpgsig "), strings.HasPrefix(line, "gpgsig-sha256 "), strings.HasPrefix(line, " "), strings.HasPrefix(line, "encoding "):
			// Optional Git metadata remains part of the hashed bytes, never authority.
		default:
			return contributionCommit{}, bad
		}
	}
	if out.tree == "" || !author || !committer {
		return contributionCommit{}, bad
	}
	return out, nil
}

// VerifyContributionMergeObjects freshly reloads ALL raw objects and proves the
// tested-tree/publication/merge/containment relationship. Squash identity differs
// normally; requiring equality of unrelated commit SHAs would reject good merges.
// This does not check GitHub effects or issue closure and cannot mint success.
func (v *BlobVerifier) VerifyContributionMergeObjects(ctx context.Context, p ContributionMergeObjects) error {
	if v == nil || v.store == nil || ctx.Err() != nil || !contributionGitOID(p.TestedBase) || p.CheckedBase != p.TestedBase || !contributionGitOID(p.TestedTree) || p.FinalHead != p.Published.OID || p.PRHead != p.FinalHead || p.Containment == nil || len(p.Containment) == 0 || len(p.Containment) > 128 || p.Containment[0].OID != p.ProtectedBaseHead {
		return ErrContributionEvidence
	}
	load := func(ref ContributionGitObject) (contributionCommit, error) {
		if ctx.Err() != nil || !validDigest(ref.Content) {
			return contributionCommit{}, ErrContributionEvidence
		}
		b, err := v.store.GetBlob(ref.Content)
		if err != nil || sha256Reference(b) != ref.Content {
			return contributionCommit{}, ErrContributionEvidence
		}
		return decodeContributionCommit(ref.OID, b)
	}
	candidate, err := load(p.Candidate)
	if err != nil {
		return err
	}
	published, err := load(p.Published)
	if err != nil {
		return err
	}
	merged, err := load(p.Merged)
	if err != nil {
		return err
	}
	if candidate.tree != p.TestedTree || published.tree != p.TestedTree || merged.tree != p.TestedTree || len(candidate.parents) != 1 || candidate.parents[0] != p.TestedBase || len(published.parents) != 1 || published.parents[0] != p.CheckedBase {
		return ErrContributionEvidence
	}
	switch p.Method {
	case "squash":
		if len(merged.parents) != 1 || merged.parents[0] != p.CheckedBase {
			return ErrContributionEvidence
		}
	case "merge":
		if len(merged.parents) != 2 || merged.parents[0] != p.CheckedBase || merged.parents[1] != p.FinalHead {
			return ErrContributionEvidence
		}
	case "fast-forward":
		if p.Merged.OID != p.FinalHead {
			return ErrContributionEvidence
		}
	default:
		return ErrContributionEvidence
	}
	seen := map[string]bool{}
	previous := ""
	for i, ref := range p.Containment {
		if seen[ref.OID] || (previous != "" && ref.OID != previous) {
			return ErrContributionEvidence
		}
		seen[ref.OID] = true
		commit, err := load(ref)
		if err != nil {
			return err
		}
		if i == len(p.Containment)-1 {
			if ref != p.Merged {
				return ErrContributionEvidence
			}
		} else {
			if len(commit.parents) == 0 {
				return ErrContributionEvidence
			}
			previous = commit.parents[0]
		}
	}
	return ctx.Err()
}
