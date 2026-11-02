package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Custody is implemented by the controller's existing exact-binding credential
// authority, not by worker environment or gh. Use must re-resolve revocation,
// version, destination and operation scope and never retain bytes after fn.
// CredentialCustody bridges exact existing encrypted authority bindings; #377
// admission and production owning-service wiring remain external.
type Custody interface {
	Use(context.Context, Intent, func([]byte) error) error
}

// Privacy is a trusted publication-content review, not a model PASS. The owning
// bridge must validate exact public text AND all committed bytes/path audience.
type Privacy interface {
	Review(context.Context, Intent, Plan) error
}
type GitHub struct {
	origin  string
	client  *http.Client
	custody Custody
	privacy Privacy
}

func NewGitHub(origin string, c Custody, p Privacy) (*GitHub, error) {
	if origin != "https://api.github.com" {
		return nil, ErrDenied
	}
	return newGitHub(origin, c, p)
}

// newGitHub permits synthetic loopback fixtures; production callers use NewGitHub.
func newGitHub(origin string, c Custody, p Privacy) (*GitHub, error) {
	u, e := url.Parse(origin)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || c == nil || p == nil {
		return nil, ErrDenied
	}
	ip := net.ParseIP(u.Hostname())
	if !(u.Scheme == "https" && u.Host == "api.github.com") && !(u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, ErrDenied
	}
	tr := &http.Transport{Proxy: nil, DisableCompression: true, ResponseHeaderTimeout: 15 * time.Second, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
	return &GitHub{origin: origin, client: &http.Client{Transport: tr, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, custody: c, privacy: p}, nil
}

var errAbsent = errors.New("remote target absent")
var errRemote = errors.New("remote outcome unknown")
var errChecksFailed = errors.New("required checks failed")
var errMergeConflict = errors.New("merge conflict")

// request is private and reachable only from the closed operations below. Never
// return upstream body/error/header strings; they can contain credentials/data.
type requestIntentKey struct{}

func (g *GitHub) request(ctx context.Context, secret []byte, method, path string, payload, out any) error {
	if checkAdapterHTTP(ctx, method) != nil {
		return ErrDenied
	}
	if secret == nil {
		i, ok := ctx.Value(requestIntentKey{}).(Intent)
		if !ok {
			return ErrDenied
		}
		return g.custody.Use(ctx, i, func(fresh []byte) error { return g.request(ctx, fresh, method, path, payload, out) })
	}
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, e := http.NewRequestWithContext(ctx, method, g.origin+path, bytes.NewReader(body))
	if e != nil {
		return ErrDenied
	}
	req.Header.Set("Authorization", "Bearer "+string(secret))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, e := g.client.Do(req)
	if e != nil {
		return errRemote
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		return errRemote
	}
	if c, ok := ctx.Value(readbackKey{}).(*readbackCollector); ok && len(secret) > 0 && bytes.Contains(b, secret) {
		c.overflow = true
	} else {
		captureReadback(ctx, path, response.StatusCode, b)
	}
	if response.StatusCode == 404 {
		return errAbsent
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode >= 500 {
			return errRemote
		}
		return ErrDenied
	}
	if response.Header.Get("Link") != "" {
		return ErrDenied
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		return errRemote
	}
	return nil
}

type repository struct {
	ID            uint64
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool
	Permissions   struct{ Push bool }
	AllowSquash   bool `json:"allow_squash_merge"`
	AllowMerge    bool `json:"allow_merge_commit"`
	AllowRebase   bool `json:"allow_rebase_merge"`
}

func (g *GitHub) identity(ctx context.Context, s []byte, i Intent, write bool) (repository, error) {
	var actor struct {
		Login string
		ID    uint64
	}
	if g.request(ctx, s, "GET", "/user", nil, &actor) != nil || actor.Login != i.Actor || actor.ID == 0 {
		return repository{}, ErrDenied
	}
	var r repository
	if g.request(ctx, s, "GET", "/repos/"+i.Repository, nil, &r) != nil || r.ID != i.RepositoryID || r.FullName != i.Repository || r.DefaultBranch != i.Base || r.Archived || (write && !r.Permissions.Push) {
		return r, ErrDenied
	}
	return r, nil
}
func (g *GitHub) ref(ctx context.Context, s []byte, i Intent, branch string) (string, error) {
	var r struct {
		Ref    string
		Object struct{ SHA string }
	}
	e := g.request(ctx, s, "GET", "/repos/"+i.Repository+"/git/ref/heads/"+branch, nil, &r)
	if e != nil {
		return "", e
	}
	if r.Ref != "refs/heads/"+branch || !sha.MatchString(r.Object.SHA) {
		return "", ErrDenied
	}
	return r.Object.SHA, nil
}

type remoteBranch struct {
	Ref, SHA string
	Repo     struct {
		ID       uint64
		FullName string `json:"full_name"`
	}
}
type pull struct {
	HTMLURL        string `json:"html_url"`
	User           struct{ Login string }
	Number         uint64
	State          string
	Draft, Merged  bool
	MergeCommit    string `json:"merge_commit_sha"`
	Mergeable      *bool
	MergeableState string `json:"mergeable_state"`
	Title, Body    string
	Head, Base     remoteBranch
}

func validatePull(r pull, i Intent, merged bool) error {
	if !canonicalPullURL(r.HTMLURL, i.Repository, r.Number) || r.User.Login != i.Actor || r.Number == 0 || (i.PR != 0 && i.PR != r.Number) || r.Draft || r.Head.Ref != i.Branch || r.Head.SHA != i.Head || r.Base.Ref != i.Base || r.Head.Repo.ID != i.RepositoryID || r.Base.Repo.ID != i.RepositoryID || r.Head.Repo.FullName != i.Repository || r.Base.Repo.FullName != i.Repository {
		return ErrDenied
	}
	if merged {
		if !r.Merged || r.State != "closed" || !sha.MatchString(r.MergeCommit) {
			return errRemote
		}
	} else if r.Merged || r.State != "open" || r.Base.SHA != i.BaseHead {
		return ErrDenied
	}
	return nil
}
func (g *GitHub) readPull(ctx context.Context, s []byte, i Intent) (pull, error) {
	var r pull
	e := g.request(ctx, s, "GET", fmt.Sprintf("/repos/%s/pulls/%d", i.Repository, i.PR), nil, &r)
	if e == nil {
		e = validatePull(r, i, r.Merged)
	}
	return r, e
}
func (g *GitHub) findPull(ctx context.Context, s []byte, i Intent, p Plan) (pull, error) {
	var all []pull
	owner := strings.Split(i.Repository, "/")[0]
	q := url.Values{"state": {"all"}, "head": {owner + ":" + i.Branch}, "base": {i.Base}, "per_page": {"100"}}
	if e := g.request(ctx, s, "GET", "/repos/"+i.Repository+"/pulls?"+q.Encode(), nil, &all); e != nil {
		return pull{}, e
	}
	if len(all) == 0 {
		return pull{}, errAbsent
	}
	if len(all) != 1 {
		return pull{}, ErrDenied
	}
	r := all[0]
	if validatePull(r, i, false) != nil || r.Title != p.PRTitle || r.Body != p.PRBody {
		return r, ErrDenied
	}
	return r, nil
}

var closing = regexp.MustCompile(`(?i)\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\s*:?\s*(#|https?://|[A-Za-z0-9_.-]+/)`)

func (g *GitHub) review(ctx context.Context, i Intent, p Plan) error {
	if g.privacy == nil || closing.MatchString(p.PRTitle+"\n"+p.PRBody) || closing.MatchString(p.Commit.Message) || len(p.PRTitle) > 256 || len(p.PRBody) > 32768 {
		return ErrDenied
	}
	for _, f := range p.Commit.Files {
		if closing.Match(f.Bytes) {
			return ErrDenied
		}
	}
	if g.privacy.Review(ctx, i, p) != nil {
		return ErrDenied
	}
	return nil
}
func (g *GitHub) Apply(ctx context.Context, i Intent, p Plan, guard Guard) Observation {
	ctx, mode := enterAdapterMode(ctx, i, false)
	defer mode.active.Store(false)
	// Replace inherited mode, but never escape an enclosing historical read.
	for parent := mode.parent; parent != nil; parent = parent.parent {
		if parent.read || !parent.active.Load() {
			return Observation{Outcome: Denied, Code: "adapter_mode_denied"}
		}
	}
	return g.perform(ctx, i, p, guard, true)
}
func (g *GitHub) Reconcile(ctx context.Context, i Intent, p Plan, guard Guard) Observation {
	ctx, mode := enterAdapterMode(ctx, i, true)
	defer mode.active.Store(false)
	return g.perform(ctx, i, p, guard, false)
}
func (g *GitHub) perform(ctx context.Context, i Intent, p Plan, guard Guard, write bool) (o Observation) {
	c := &readbackCollector{requirements: requirementsFor(p)}
	ctx = context.WithValue(ctx, readbackKey{}, c)
	defer func() { o = sealReadback(i, o, c) }()
	if i.Operation == TransferGraph {
		expected, e := intentFor(p, TransferGraph)
		if e != nil || expected != i || guard == nil || guard(ctx) != nil || !permits(p.Delivery, TransferGraph) || g.review(ctx, i, p) != nil {
			return Observation{Outcome: Denied, Code: "graph_denied"}
		}
		return Observation{Outcome: Confirmed, Head: p.Head, Tree: p.Commit.Tree}
	}
	if i.Operation == UploadBlob || i.Operation == UploadTree || i.Operation == UploadCommit {
		return g.objectEffect(ctx, i, p, guard, write)
	}
	o = Observation{Outcome: Unknown, Code: "remote_unknown"}
	attempted := false
	expected, e := intentFor(p, i.Operation)
	if e != nil || i != expected || guard == nil {
		return Observation{Outcome: Denied, Code: "target_denied"}
	}
	// An adapter is not a bypass around the engine's narrower delivery guard.
	if write && !permits(p.Delivery, i.Operation) {
		return Observation{Outcome: Denied, Code: "delivery_restricted"}
	}
	if write && g.review(ctx, i, p) != nil {
		return Observation{Outcome: Denied, Code: "publication_denied"}
	}
	ctx = context.WithValue(ctx, requestIntentKey{}, i)
	e = func(s []byte) error {
		if guard(ctx) != nil {
			return ErrDenied
		}
		r, e := g.identity(ctx, s, i, write)
		if e != nil {
			return e
		}
		// Every actual mutation repeats identity+permissions AND fresh admission,
		// adjacent to that mutation, even after slow CI/review reads.
		mutate := func(method, path string, payload, out any) error {
			return g.custody.Use(ctx, i, func(s []byte) error {
				if guard(ctx) != nil {
					return ErrDenied
				}
				if _, e := g.identity(ctx, s, i, true); e != nil {
					return e
				}
				if guard(ctx) != nil {
					return ErrDenied
				}
				if i.Operation == UpdatePullRequest {
					fresh, e := g.readPull(ctx, s, i)
					if e != nil || fresh.Merged || fresh.User.Login != i.Actor {
						return ErrDenied
					}
					head, e := g.ref(ctx, s, i, i.Branch)
					if e != nil || head != i.Head {
						return ErrDenied
					}
				}
				if i.Operation == Merge {
					if g.strictProtection(ctx, s, i, p) != nil {
						return ErrDenied
					}
					base, e := g.ref(ctx, s, i, i.Base)
					if e != nil || base != i.BaseHead {
						return ErrDenied
					}
					head, e := g.ref(ctx, s, i, i.Branch)
					if e != nil || head != i.Head {
						return ErrDenied
					}
					fresh, e := g.readPull(ctx, s, i)
					if e != nil || fresh.Merged || fresh.Mergeable == nil || !*fresh.Mergeable || fresh.MergeableState != "clean" {
						return ErrDenied
					}
				}
				// This credential surrounds admission, exact-head review read and write.
				if guard(ctx) != nil {
					return ErrDenied
				}
				if i.Operation == Merge && g.reviews(ctx, s, i, p.RequiredReviewers) != nil {
					return ErrDenied
				}
				// Reads are not atomic with GitHub. Strict server stale/last-push review
				// enforcement above is required to close the last request window.
				attempted = true
				return g.request(ctx, s, method, path, payload, out)
			})
		}
		prefix := "/repos/" + i.Repository
		switch i.Operation {
		case Publish:
			// Historical publication proves only the exact reserved ref/head. It never
			// reconstructs or transfers objects; productive publication still checks the graph.
			if write && g.graphPresent(ctx, s, i, p) != nil {
				return ErrDenied
			}
			head, e := g.ref(ctx, s, i, i.Branch)
			if e == nil && head == i.Head {
				o = Observation{Outcome: Confirmed, Head: head}
				return nil
			}
			if !write {
				return errRemote
			}
			// Publication requires a remotely present exact accepted commit.
			// When supplied, the accepted graph was transferred and verified first.
			var object struct{ SHA string }
			if g.request(ctx, s, "GET", prefix+"/git/commits/"+i.Head, nil, &object) != nil || object.SHA != i.Head {
				return ErrDenied
			}
			if errors.Is(e, errAbsent) && i.ExpectedOld == "" {
				var out any
				e = mutate("POST", prefix+"/git/refs", struct {
					Ref string `json:"ref"`
					SHA string `json:"sha"`
				}{"refs/heads/" + i.Branch, i.Head}, &out)
			} else if e == nil && head == i.ExpectedOld && sha.MatchString(i.ExpectedOld) {
				var out any
				e = mutate("PATCH", prefix+"/git/refs/heads/"+i.Branch, struct {
					SHA   string `json:"sha"`
					Force bool   `json:"force"`
				}{i.Head, false}, &out)
			} else {
				return ErrDenied
			}
			if e != nil {
				return e
			}
			head, e = g.ref(ctx, s, i, i.Branch)
			if e != nil || head != i.Head {
				return errRemote
			}
			o = Observation{Outcome: Confirmed, Head: head}
		case PullRequest:
			head, e := g.ref(ctx, s, i, i.Branch)
			if e != nil || head != i.Head {
				return ErrDenied
			}
			existing, e := g.findPull(ctx, s, i, p)
			if e == nil {
				o = Observation{Outcome: Confirmed, Head: i.Head, PR: existing.Number}
				return nil
			}
			if !write || !errors.Is(e, errAbsent) {
				return e
			}
			if p.PRTitle == "" {
				return ErrDenied
			}
			var out pull
			e = mutate("POST", prefix+"/pulls", struct {
				Title               string `json:"title"`
				Body                string `json:"body"`
				Head                string `json:"head"`
				Base                string `json:"base"`
				Draft               bool   `json:"draft"`
				MaintainerCanModify bool   `json:"maintainer_can_modify"`
			}{p.PRTitle, p.PRBody, i.Branch, i.Base, false, false}, &out)
			if e != nil {
				return e
			}
			existing, e = g.findPull(ctx, s, i, p)
			if e != nil {
				return errRemote
			}
			o = Observation{Outcome: Confirmed, Head: i.Head, PR: existing.Number}
		case UpdatePullRequest:
			if i.PR == 0 || p.PRTitle == "" {
				return ErrDenied
			}
			pr, e := g.readPull(ctx, s, i)
			if e != nil || pr.Merged || pr.User.Login != i.Actor {
				return ErrDenied
			}
			if pr.Title != p.PRTitle || pr.Body != p.PRBody {
				if !write {
					return errRemote
				}
				var out pull
				if e = mutate("PATCH", fmt.Sprintf("%s/pulls/%d", prefix, i.PR), struct {
					Title string `json:"title"`
					Body  string `json:"body"`
				}{p.PRTitle, p.PRBody}, &out); e != nil {
					return e
				}
				pr, e = g.readPull(ctx, s, i)
				if e != nil || pr.Merged || pr.User.Login != i.Actor || pr.Title != p.PRTitle || pr.Body != p.PRBody {
					return errRemote
				}
			}
			o = Observation{Outcome: Confirmed, Head: i.Head, PR: i.PR}
		case CheckDelivery:
			// This operation has no mutation path, including on reconciliation.
			if i.PR == 0 {
				return ErrDenied
			}
			// Read the live base without rebinding this immutable intent. A changed
			// base is a distinct held integration prerequisite, never a CI failure
			// or permission to rebuild from unadmitted upstream source.
			var pr pull
			if err := g.request(ctx, s, "GET", fmt.Sprintf("/repos/%s/pulls/%d", i.Repository, i.PR), nil, &pr); err != nil {
				return err
			}
			o = Observation{Outcome: Unknown, Code: "base_gate", Head: i.Head, PR: i.PR}
			live := i
			live.BaseHead = pr.Base.SHA
			if !sha.MatchString(live.BaseHead) || validatePull(pr, live, false) != nil {
				return ErrDenied
			}
			base, err := g.ref(ctx, s, i, i.Base)
			if err != nil || base != live.BaseHead {
				return ErrDenied
			}
			if base != i.BaseHead {
				var advance struct {
					Status string
					Behind int `json:"behind_by"`
				}
				if g.request(ctx, s, "GET", prefix+"/compare/"+i.BaseHead+"..."+base, nil, &advance) != nil || advance.Status != "ahead" || advance.Behind != 0 {
					return ErrDenied
				}
				head, err := g.ref(ctx, s, i, i.Branch)
				if err != nil || head != i.Head {
					return ErrDenied
				}
				o.Code = "base_advanced"
				return ErrDenied
			}
			head, err := g.ref(ctx, s, i, i.Branch)
			if err != nil || head != i.Head {
				return ErrDenied
			}
			if pr.Mergeable != nil && !*pr.Mergeable && pr.MergeableState == "dirty" {
				return errMergeConflict
			}
			if pr.Mergeable == nil || !*pr.Mergeable || pr.MergeableState != "clean" {
				return ErrDenied
			}
			var ancestry struct {
				Status string
				Behind int `json:"behind_by"`
			}
			if g.request(ctx, s, "GET", prefix+"/compare/"+i.BaseHead+"..."+i.Head, nil, &ancestry) != nil || (ancestry.Status != "ahead" && ancestry.Status != "identical") || ancestry.Behind != 0 {
				return ErrDenied
			}
			o.Code = "protection_gate"
			if g.strictProtection(ctx, s, i, p) != nil {
				return ErrDenied
			}
			o.Code = "ci_gate"
			if err = g.checks(ctx, s, i, i.Head, p.RequiredChecks); err != nil {
				return err
			}
			o.Code = "review_gate"
			if err = g.reviews(ctx, s, i, p.RequiredReviewers); err != nil {
				return err
			}
			o.Outcome, o.Code = Confirmed, "delivery_checked"
		case Merge:
			if i.PR == 0 {
				return ErrDenied
			}
			pr, e := g.readPull(ctx, s, i)
			if e != nil {
				return e
			}
			if pr.Merged {
				return g.merged(ctx, s, i, p, pr, &o)
			}
			if !write {
				return errRemote
			}
			base, e := g.ref(ctx, s, i, i.Base)
			if e != nil || base != i.BaseHead {
				return ErrDenied
			}
			head, e := g.ref(ctx, s, i, i.Branch)
			if e != nil || head != i.Head {
				return ErrDenied
			}
			if pr.Mergeable != nil && !*pr.Mergeable && pr.MergeableState == "dirty" {
				return errMergeConflict
			}
			if pr.Mergeable == nil || !*pr.Mergeable || pr.MergeableState != "clean" {
				return ErrDenied
			}
			var ancestry struct {
				Status string
				Behind int `json:"behind_by"`
			}
			if g.request(ctx, s, "GET", prefix+"/compare/"+i.BaseHead+"..."+i.Head, nil, &ancestry) != nil || (ancestry.Status != "ahead" && ancestry.Status != "identical") || ancestry.Behind != 0 {
				return ErrDenied
			}
			if e = g.checks(ctx, s, i, i.Head, p.RequiredChecks); e != nil {
				o.Code = "ci_gate"
				return e
			}
			if e = g.reviews(ctx, s, i, p.RequiredReviewers); e != nil {
				o.Code = "review_gate"
				return e
			}
			allowed := p.MergeMethod == "squash" && r.AllowSquash || p.MergeMethod == "merge" && r.AllowMerge
			// Rebase is deliberately unsupported: no single tested-tree reconciliation.
			if !allowed || !sha.MatchString(p.Commit.Tree) {
				return ErrDenied
			}
			base, e = g.ref(ctx, s, i, i.Base)
			if e != nil || base != i.BaseHead {
				return ErrDenied
			}
			var response struct {
				Merged bool
				SHA    string
			}
			e = mutate("PUT", fmt.Sprintf("%s/pulls/%d/merge", prefix, i.PR), struct {
				SHA    string `json:"sha"`
				Method string `json:"merge_method"`
			}{i.Head, p.MergeMethod}, &response)
			if e != nil {
				return e
			}
			if !response.Merged || !sha.MatchString(response.SHA) {
				return errRemote
			}
			pr, e = g.readPull(ctx, s, i)
			if e != nil || pr.MergeCommit != response.SHA {
				return errRemote
			}
			return g.merged(ctx, s, i, p, pr, &o)
		case ReconcileIssue:
			if p.Completion == "" || p.Intake.Source.IssueID == 0 || p.Intake.Source.URL.Number == 0 || p.Intake.Repository != i.Repository || p.Intake.Source.RepositoryID != i.RepositoryID || p.Intake.Digest != i.Contract {
				return ErrDenied
			}
			path := fmt.Sprintf("%s/issues/%d", prefix, p.Intake.Source.URL.Number)
			var issue struct {
				ID          uint64
				Number      uint64
				StateReason string `json:"state_reason"`
				State       string
				PullRequest json.RawMessage `json:"pull_request"`
			}
			if e = g.request(ctx, s, "GET", path, nil, &issue); e != nil {
				return e
			}
			if issue.ID != p.Intake.Source.IssueID || issue.Number != uint64(p.Intake.Source.URL.Number) || len(issue.PullRequest) > 0 {
				return ErrDenied
			}
			if issue.State != "closed" {
				if !write {
					return errRemote
				}
				if issue.State != "open" {
					return ErrDenied
				}
				if e = mutate("PATCH", path, struct {
					State  string `json:"state"`
					Reason string `json:"state_reason"`
				}{"closed", "completed"}, &issue); e != nil {
					return e
				}
				if e = g.request(ctx, s, "GET", path, nil, &issue); e != nil {
					return e
				}
			}
			if issue.ID != p.Intake.Source.IssueID || issue.Number != uint64(p.Intake.Source.URL.Number) || issue.StateReason != "completed" || issue.State != "closed" || len(issue.PullRequest) > 0 {
				return errRemote
			}
			o = Observation{Outcome: Confirmed, Code: "issue_closed"}
		default:
			return ErrDenied
		}
		return nil
	}(nil)
	if e != nil {
		if o.Merge != "" {
			o.Outcome, o.Code = Unknown, "merged_validation_unresolved"
		} else if i.Operation == CheckDelivery {
			// Held reads remain retryable UNKNOWN; only concrete failures are terminal.
			o.Outcome = Unknown
			if errors.Is(e, errChecksFailed) {
				o.Outcome, o.Code = Failed, "ci_failed"
			}
			if errors.Is(e, errMergeConflict) {
				o.Outcome, o.Code = Failed, "merge_conflict"
			}
		} else if attempted || !write {
			o.Outcome, o.Code = Unknown, "remote_unknown"
		} else if errors.Is(e, errMergeConflict) {
			o.Outcome, o.Code = Failed, "merge_conflict"
		} else if errors.Is(e, errChecksFailed) {
			o.Outcome, o.Code = Failed, "ci_failed"
		} else if errors.Is(e, ErrDenied) {
			o.Outcome = Denied
			if o.Code == "remote_unknown" {
				o.Code = "remote_denied"
			}
		} else if o.Merge != "" {
			o.Code = "merged_validation_unresolved"
		} else {
			o.Code = "remote_unknown"
		}
	}
	return
}
func (g *GitHub) checks(ctx context.Context, s []byte, i Intent, head string, required []string) error {
	if len(required) == 0 || len(required) > 100 {
		return ErrDenied
	}
	var runs struct {
		Total int `json:"total_count"`
		Runs  []struct {
			Name, Status, Conclusion string
			Head                     string `json:"head_sha"`
		} `json:"check_runs"`
	}
	prefix := "/repos/" + i.Repository + "/commits/" + head
	if e := g.request(ctx, s, "GET", prefix+"/check-runs?per_page=100", nil, &runs); e != nil {
		return e
	}
	if runs.Total != len(runs.Runs) {
		return ErrDenied
	}
	states := map[string]bool{}
	for _, r := range runs.Runs {
		if _, ok := states[r.Name]; ok {
			return ErrDenied
		}
		states[r.Name] = r.Head == head && r.Status == "completed" && r.Conclusion == "success"
	}
	var statuses struct {
		Total    int `json:"total_count"`
		SHA      string
		Statuses []struct{ Context, State string }
	}
	if e := g.request(ctx, s, "GET", prefix+"/status?per_page=100", nil, &statuses); e != nil {
		return e
	}
	if statuses.Total != len(statuses.Statuses) || len(statuses.Statuses) > 0 && statuses.SHA != head {
		return ErrDenied
	}
	for _, r := range statuses.Statuses {
		if _, ok := states[r.Context]; ok {
			return ErrDenied
		}
		states[r.Context] = r.State == "success"
	}
	seen := map[string]bool{}
	for _, name := range required {
		for _, run := range runs.Runs {
			if run.Name == name && run.Head == head && run.Status == "completed" && (run.Conclusion == "failure" || run.Conclusion == "timed_out" || run.Conclusion == "cancelled") {
				return errChecksFailed
			}
		}
		for _, status := range statuses.Statuses {
			if status.Context == name && (status.State == "failure" || status.State == "error") {
				return errChecksFailed
			}
		}
		if name == "" || seen[name] || !states[name] {
			return ErrDenied
		}
		seen[name] = true
	}
	return nil
}
func (g *GitHub) reviews(ctx context.Context, s []byte, i Intent, required []string) error {
	if len(required) == 0 || len(required) > 100 {
		return ErrDenied
	}
	var reviews []struct {
		ID     uint64
		User   struct{ Login string }
		State  string
		Commit string `json:"commit_id"`
	}
	if e := g.request(ctx, s, "GET", fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100", i.Repository, i.PR), nil, &reviews); e != nil {
		return e
	}
	sort.Slice(reviews, func(a, b int) bool { return reviews[a].ID < reviews[b].ID })
	latest := map[string]string{}
	for _, r := range reviews {
		if r.State == "COMMENTED" {
			continue
		}
		if r.Commit != i.Head {
			latest[r.User.Login] = "stale"
		} else {
			latest[r.User.Login] = r.State
		}
	}
	for _, state := range latest {
		if state == "CHANGES_REQUESTED" {
			return ErrDenied
		}
	}
	seen := map[string]bool{}
	for _, user := range required {
		if user == "" || seen[user] || user == i.Actor || latest[user] != "APPROVED" {
			return ErrDenied
		}
		seen[user] = true
	}
	return nil
}
func (g *GitHub) merged(ctx context.Context, s []byte, i Intent, p Plan, pr pull, o *Observation) error {
	if validatePull(pr, i, true) != nil {
		return errRemote
	}
	*o = Observation{Outcome: Unknown, Code: "merged_validation_unresolved", Head: i.Head, PR: i.PR, Merge: pr.MergeCommit}
	var commit struct {
		SHA     string
		Tree    struct{ SHA string }
		Parents []struct{ SHA string }
	}
	if g.request(ctx, s, "GET", "/repos/"+i.Repository+"/git/commits/"+pr.MergeCommit, nil, &commit) != nil || commit.SHA != pr.MergeCommit || commit.Tree.SHA != p.Commit.Tree {
		return errRemote
	}
	if p.MergeMethod == "squash" {
		if len(commit.Parents) != 1 || commit.Parents[0].SHA != i.BaseHead {
			return errRemote
		}
	} else if p.MergeMethod == "merge" {
		if len(commit.Parents) != 2 || commit.Parents[0].SHA != i.BaseHead || commit.Parents[1].SHA != i.Head {
			return errRemote
		}
	} else {
		return errRemote
	}
	o.Tree = commit.Tree.SHA
	base, e := g.ref(ctx, s, i, i.Base)
	if e != nil {
		return e
	}
	o.BaseHead = base
	var comparison struct {
		Status string
		Behind int `json:"behind_by"`
	}
	if g.request(ctx, s, "GET", "/repos/"+i.Repository+"/compare/"+pr.MergeCommit+"..."+base, nil, &comparison) != nil || (comparison.Status != "ahead" && comparison.Status != "identical") || comparison.Behind != 0 {
		return errRemote
	}
	if g.checks(ctx, s, i, pr.MergeCommit, p.PostMergeChecks) != nil {
		return errRemote
	}
	if g.strictProtection(ctx, s, i, p) != nil || g.checks(ctx, s, i, i.Head, p.RequiredChecks) != nil || g.reviews(ctx, s, i, p.RequiredReviewers) != nil {
		return errRemote
	}
	var ancestry struct {
		Status string
		Behind int `json:"behind_by"`
	}
	if g.request(ctx, s, "GET", "/repos/"+i.Repository+"/compare/"+i.BaseHead+"..."+i.Head, nil, &ancestry) != nil || (ancestry.Status != "ahead" && ancestry.Status != "identical") || ancestry.Behind != 0 {
		return errRemote
	}
	o.Outcome = Confirmed
	o.Code = "merged_verified"
	return nil
}

// ClosedAdapter routes local versus remote effects; no arbitrary shell or REST.
type ClosedAdapter struct {
	Git    *Git
	GitHub *GitHub
}

func (a ClosedAdapter) localTarget(ctx context.Context, i Intent, p Plan) bool {
	expected, e := intentFor(p, Commit)
	parent := publicationParent(p.BaseHead, p.ExpectedOld, p.RepairFailure)
	if e != nil || expected != i || a.Git == nil || p.Commit.Branch != p.Branch || p.Commit.Commit != p.Head || p.Commit.Parent != parent || !sha.MatchString(parent) || !sha.MatchString(p.Commit.Tree) || p.Commit.Branch == p.Base || p.Intake.Worktree != a.Git.Directory {
		return false
	}
	root, e := a.Git.run(ctx, nil, nil, "rev-parse", "--show-toplevel")
	if e != nil || strings.TrimSpace(string(root)) != a.Git.Directory {
		return false
	}
	object, e := authoredObject(p.Commit)
	return e == nil && object.OID == p.Commit.Commit
}

func (a ClosedAdapter) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if i.Operation == Commit {
		if !a.localTarget(ctx, i, p) || !permits(p.Delivery, Commit) {
			return Observation{Outcome: Denied, Code: "local_target_denied"}
		}
		return a.Git.Commit(ctx, p.Commit, g)
	}
	if a.GitHub == nil {
		return Observation{Outcome: Denied, Code: "remote_unavailable"}
	}
	return a.GitHub.Apply(ctx, i, p, g)
}
func (a ClosedAdapter) Reconcile(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if i.Operation == Commit {
		if !a.localTarget(ctx, i, p) {
			return Observation{Outcome: Denied, Code: "local_target_denied"}
		}
		if g == nil || g(ctx) != nil {
			return Observation{Outcome: Unknown, Code: "local_read_denied"}
		}
		return a.Git.Reconcile(ctx, p.Commit)
	}
	if a.GitHub == nil {
		return Observation{Outcome: Unknown, Code: "remote_unavailable"}
	}
	return a.GitHub.Reconcile(ctx, i, p, g)
}
