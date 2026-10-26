package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const ReadbackSchema = "aegis.delivery.readback.v1"
const MaxProofBytes = 262144

// ReadbackProof is produced inside the trusted HTTP adapter, not by a worker.
// Intent and Result bind every retained fact to the exact immutable effect.
type ReadbackProof struct {
	Schema         string
	Intent         Intent
	Result         Observation
	ObservedResult Observation
	RetainedFrom   *ReadbackRetention    `json:",omitempty"`
	Requirements   *ReadbackRequirements `json:",omitempty"`
	ObservedAt     time.Time
	Facts          []ReadbackFact
}

// ReadbackFact excludes body, title, raw message and headers. HTMLURL is canonical only.
// Fields retain upstream spellings/values; absent fields are not passing facts.
type ReadbackPermissions struct {
	Push     bool `json:"push"`
	Pull     bool `json:"pull"`
	Admin    bool `json:"admin"`
	Maintain bool `json:"maintain"`
	Triage   bool `json:"triage"`
}
type ReadbackFact struct {
	HTMLURL        string               `json:"html_url,omitempty"`
	StateReason    string               `json:"state_reason,omitempty"`
	Author         *gitPerson           `json:"author,omitempty"`
	Committer      *gitPerson           `json:"committer,omitempty"`
	MessageDigest  string               `json:"message_digest,omitempty"`
	Permissions    *ReadbackPermissions `json:"permissions,omitempty"`
	AllowSquash    bool                 `json:"allow_squash_merge,omitempty"`
	AllowMerge     bool                 `json:"allow_merge_commit,omitempty"`
	AllowRebase    bool                 `json:"allow_rebase_merge,omitempty"`
	Target         ReadbackTarget
	ResponseDigest string
	Protection     *ReadbackProtection `json:",omitempty"`
	Rules          []ReadbackRule      `json:",omitempty"`
	Permission     string              `json:"permission,omitempty"`
	Role           string              `json:"role_name,omitempty"`
	Mergeable      *bool               `json:"mergeable,omitempty"`
	MergeableState string              `json:"mergeable_state,omitempty"`
	Kind           string
	HTTPStatus     int
	ID             uint64           `json:"id,omitempty"`
	Login          string           `json:"login,omitempty"`
	FullName       string           `json:"full_name,omitempty"`
	DefaultBranch  string           `json:"default_branch,omitempty"`
	Name           string           `json:"name,omitempty"`
	Context        string           `json:"context,omitempty"`
	Status         string           `json:"status,omitempty"`
	State          string           `json:"state,omitempty"`
	Conclusion     string           `json:"conclusion,omitempty"`
	HeadSHA        string           `json:"head_sha,omitempty"`
	CommitID       string           `json:"commit_id,omitempty"`
	SHA            string           `json:"sha,omitempty"`
	Number         uint64           `json:"number,omitempty"`
	Merged         bool             `json:"merged,omitempty"`
	Draft          bool             `json:"draft,omitempty"`
	MergeCommitSHA string           `json:"merge_commit_sha,omitempty"`
	Behind         int              `json:"behind_by,omitempty"`
	Ref            string           `json:"ref,omitempty"`
	User           *ReadbackUser    `json:"user,omitempty"`
	App            *ReadbackUser    `json:"app,omitempty"`
	Creator        *ReadbackUser    `json:"creator,omitempty"`
	Tree           *ReadbackObject  `json:"tree,omitempty"`
	Object         *ReadbackObject  `json:"object,omitempty"`
	Parents        []ReadbackObject `json:"parents,omitempty"`
	Head           *ReadbackBranch  `json:"head,omitempty"`
	Base           *ReadbackBranch  `json:"base,omitempty"`
	PolicyDigest   string           `json:"policy_digest,omitempty"`
}
type ReadbackUser struct {
	ID    uint64 `json:"id"`
	Login string `json:"login,omitempty"`
	Slug  string `json:"slug,omitempty"`
}
type ReadbackObject struct {
	SHA string `json:"sha"`
}
type ReadbackBranch struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		ID       uint64 `json:"id"`
		FullName string `json:"full_name"`
	} `json:"repo"`
}
type readbackKey struct{}
type readbackCollector struct {
	facts        []ReadbackFact
	overflow     bool
	requirements *ReadbackRequirements
}

func (c *readbackCollector) add(f ReadbackFact) {
	if !safeReadbackFact(f) {
		c.overflow = true
		return
	}
	if len(c.facts) >= 400 {
		c.overflow = true
		return
	}
	c.facts = append(c.facts, f)
}
func captureReadback(ctx context.Context, path string, status int, b []byte) {
	c, _ := ctx.Value(readbackKey{}).(*readbackCollector)
	if c == nil {
		return
	}
	kind := ""
	switch {
	case path == "/user":
		kind = "actor"
	case strings.Contains(path, "/rules/branches/"):
		kind = "rules"
	case strings.HasSuffix(path, "/permission"):
		kind = "permission"
	case strings.Contains(path, "/check-runs?"):
		kind = "check_run"
	case strings.Contains(path, "/status?"):
		kind = "commit_status"
	case strings.Contains(path, "/reviews?"):
		kind = "review"
	case strings.HasSuffix(path, "/protection"):
		kind = "protection"
	case strings.Contains(path, "/compare/"):
		kind = "comparison"
	case strings.Contains(path, "/git/commits/"):
		kind = "commit"
	case strings.Contains(path, "/git/ref/heads/"):
		kind = "ref"
	case strings.HasSuffix(path, "/merge"):
		kind = "merge_response"
	case strings.Contains(path, "/issues/"):
		kind = "issue"
	case strings.Contains(path, "/git/blobs/"):
		kind = "blob"
	case strings.Contains(path, "/git/trees/"):
		kind = "tree"
	case strings.Contains(path, "/pulls/") || strings.Contains(path, "/pulls?"):
		kind = "pull"
	case strings.Count(path, "/") == 3:
		kind = "repository"
	default:
		return
	}
	f := ReadbackFact{Kind: kind, HTTPStatus: status, Target: requestTarget(path), ResponseDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(b))}
	if status < 200 || status >= 300 {
		f.ResponseDigest = ""
		c.add(f)
		return
	}
	if kind == "protection" {
		var raw map[string]any
		if json.Unmarshal(b, &raw) == nil {
			f.PolicyDigest = digest(raw)
			var projection ReadbackProtection
			if json.Unmarshal(b, &projection) != nil {
				c.overflow = true
				return
			}
			f.Protection = &projection
		}
		c.add(f)
		return
	}
	if kind == "rules" {
		if json.Unmarshal(b, &f.Rules) != nil {
			c.overflow = true
			return
		}
		c.add(f)
		return
	}
	var values []json.RawMessage
	switch kind {
	case "pull":
		if strings.Contains(path, "/pulls?") {
			_ = json.Unmarshal(b, &values)
		} else {
			values = []json.RawMessage{b}
		}
	case "review":
		_ = json.Unmarshal(b, &values)
	case "check_run":
		var v struct {
			Runs []json.RawMessage `json:"check_runs"`
		}
		_ = json.Unmarshal(b, &v)
		values = v.Runs
	case "commit_status":
		var v struct {
			SHA      string
			Statuses []json.RawMessage
		}
		_ = json.Unmarshal(b, &v)
		values = v.Statuses
		f.HeadSHA = v.SHA
	default:
		values = []json.RawMessage{b}
	}
	if len(values) == 0 {
		c.add(f)
	}
	for _, v := range values {
		n := f
		if kind == "blob" || kind == "tree" {
			var raw struct{ SHA string }
			if json.Unmarshal(v, &raw) != nil {
				c.overflow = true
				return
			}
			n.SHA = raw.SHA
		} else if json.Unmarshal(v, &n) != nil {
			c.overflow = true
			return
		}
		if kind == "commit" {
			n.MessageDigest = ""
			var raw struct {
				Message *string `json:"message"`
			}
			if json.Unmarshal(v, &raw) != nil {
				c.overflow = true
				return
			}
			if raw.Message != nil {
				n.MessageDigest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(*raw.Message)))
			}
		}
		if kind == "pull" && !canonicalPullURL(n.HTMLURL, f.Target.Repository, n.Number) {
			c.overflow = true
			return
		}
		n.Target = f.Target
		n.ResponseDigest = f.ResponseDigest
		n.Kind = kind
		n.HTTPStatus = status
		c.add(n)
	}
}
func sealReadback(i Intent, o Observation, c *readbackCollector) Observation {
	o.ProofJSON = ""
	o.ProofDigest = ""
	if c.overflow {
		o.Outcome = Unknown
		o.Code = "readback_capture_rejected"
		return o
	}
	p := ReadbackProof{Schema: ReadbackSchema, Intent: i, Result: o, ObservedResult: o, ObservedAt: time.Now().UTC(), Facts: c.facts, Requirements: c.requirements}
	if validateSemantic(p) != nil {
		o.Outcome = Unknown
		o.Code = "readback_semantic_denied"
		p.Result = o
		p.ObservedResult = o
	}
	b, e := json.Marshal(p)
	if e != nil || len(b) > MaxProofBytes {
		o.Outcome = Unknown
		o.Code = "readback_capture_rejected"
		return o
	}
	o.ProofJSON = string(b)
	o.ProofDigest = digest(p)
	return o
}

// ParseReadback rejects alternate encodings, duplicate/unknown fields and any
// mismatch with the exact canonical observation or intent. It grants no authority.
func ParseReadback(i Intent, o Observation) (ReadbackProof, error) {
	var p ReadbackProof
	if len(o.ProofJSON) == 0 || len(o.ProofJSON) > MaxProofBytes || reference.RejectDuplicateObjectKeys([]byte(o.ProofJSON)) != nil {
		return p, ErrDenied
	}
	d := json.NewDecoder(strings.NewReader(o.ProofJSON))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return p, ErrDenied
	}
	b, _ := json.Marshal(p)
	plain := o
	plain.ProofJSON = ""
	plain.ProofDigest = ""
	if !bytes.Equal(b, []byte(o.ProofJSON)) || p.Schema != ReadbackSchema || p.Intent != i || p.Result != plain || p.ObservedAt.IsZero() || p.ObservedAt.Location() != time.UTC || len(p.Facts) > 400 || digest(p) != o.ProofDigest {
		return p, ErrDenied
	}
	for _, f := range p.Facts {
		switch f.Kind {
		case "actor", "repository", "check_run", "commit_status", "review", "protection", "comparison", "commit", "ref", "pull", "rules", "permission", "merge_response", "issue", "blob", "tree":
		default:
			return p, ErrDenied
		}
		if f.HTTPStatus < 100 || f.HTTPStatus > 599 || ((f.HTTPStatus >= 200 && f.HTTPStatus < 300) && !roleDigest.MatchString(f.ResponseDigest)) || !safeReadbackFact(f) {
			return p, ErrDenied
		}
	}
	if validateProofRelation(p) != nil || validateSemantic(p) != nil {
		return p, ErrDenied
	}
	return p, nil
}

func messageDigest(message string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(message)))
}
