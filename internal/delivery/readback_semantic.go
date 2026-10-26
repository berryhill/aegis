package delivery

import (
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type ReadbackTarget struct {
	Repository, Head, Ref, ComparisonBase, ComparisonHead string
	PR                                                    uint64
	Issue                                                 uint64
}
type ReadbackRule struct {
	Type              string `json:"type"`
	RulesetID         uint64 `json:"ruleset_id"`
	RulesetSourceType string `json:"ruleset_source_type"`
	RulesetSource     string `json:"ruleset_source"`
}
type ReadbackProtection struct {
	Admins struct{ Enabled bool } `json:"enforce_admins"`
	Checks *struct {
		Strict   bool
		Contexts []string
		Checks   []struct {
			Context string
			AppID   int64 `json:"app_id"`
		}
	} `json:"required_status_checks"`
	Reviews *struct {
		Count        int                       `json:"required_approving_review_count"`
		DismissStale bool                      `json:"dismiss_stale_reviews"`
		LastPush     bool                      `json:"require_last_push_approval"`
		Bypass       map[string][]ReadbackUser `json:"bypass_pull_request_allowances"`
	} `json:"required_pull_request_reviews"`
}
type ReadbackRequirements struct {
	Policy, Delivery, Worktree, Method, Tree, Protection string
	Completion                                           string
	IssueID, IssueNumber                                 uint64
	Publication                                          *ReadbackPublication `json:",omitempty"`
	ObjectDigest                                         string
	Checks, Reviewers, PostChecks                        []string
}
type ReadbackRetention struct {
	Revision uint64
	Digest   string
	Result   Observation
}

func requirementsFor(p Plan) *ReadbackRequirements {
	return &ReadbackRequirements{Completion: p.Completion, IssueID: p.Intake.Source.IssueID, IssueNumber: uint64(p.Intake.Source.URL.Number), Publication: publicationFor(p.Object), ObjectDigest: digest(p.Object), Policy: p.Policy, Delivery: p.Delivery, Worktree: p.Intake.Worktree, Method: p.MergeMethod, Tree: p.Commit.Tree, Protection: p.ProtectionPolicy, Checks: append([]string(nil), p.RequiredChecks...), Reviewers: append([]string(nil), p.RequiredReviewers...), PostChecks: append([]string(nil), p.PostMergeChecks...)}
}
func requestTarget(path string) ReadbackTarget {
	u, _ := url.Parse(path)
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var t ReadbackTarget
	if len(parts) < 3 || parts[0] != "repos" {
		return t
	}
	t.Repository = parts[1] + "/" + parts[2]
	if len(parts) > 4 {
		switch parts[3] {
		case "commits":
			t.Head = parts[4]
		case "issues":
			t.Issue, _ = strconv.ParseUint(parts[4], 10, 64)
		case "pulls":
			t.PR, _ = strconv.ParseUint(parts[4], 10, 64)
		case "compare":
			v := strings.Split(parts[4], "...")
			if len(v) == 2 {
				t.ComparisonBase = v[0]
				t.ComparisonHead = v[1]
			}
		case "branches":
			t.Ref = parts[4]
		case "rules":
			if len(parts) > 5 {
				t.Ref = strings.Join(parts[5:], "/")
			}
		case "git":
			if len(parts) > 6 && parts[4] == "ref" {
				t.Ref = strings.Join(parts[6:], "/")
			} else if parts[4] == "commits" && len(parts) > 5 {
				t.Head = parts[5]
			}
		}
	}
	return t
}
func plainObservation(o Observation) Observation { o.ProofJSON = ""; o.ProofDigest = ""; return o }
func RecordDigest(r Record) string {
	return digest(struct {
		Intent      Intent
		Observation Observation
		Outcome     Outcome
		Revision    uint64
	}{r.Intent, r.Observation, r.Outcome, r.Revision})
}
func validateProofRelation(p ReadbackProof) error {
	if p.ObservedResult.ProofJSON != "" || p.ObservedResult.ProofDigest != "" {
		return ErrDenied
	}
	if p.RetainedFrom == nil {
		if p.Result != p.ObservedResult {
			return ErrDenied
		}
		return nil
	}
	r := p.RetainedFrom
	if r.Revision == 0 || !roleDigest.MatchString(r.Digest) || r.Result != plainObservation(r.Result) {
		return ErrDenied
	}
	joined, e := joinObservation(Record{Intent: p.Intent, Observation: r.Result}, p.ObservedResult)
	if e != nil || joined != p.Result {
		return ErrDenied
	}
	return nil
}

// ValidateRetainedReadback requires the actual prior canonical record, not a
// caller-supplied digest alone. Used both by Finish and history loading.
func ValidateRetainedReadback(old Record, o Observation) error {
	p, e := ParseReadback(old.Intent, o)
	if e != nil {
		return e
	}
	if p.RetainedFrom != nil {
		r := p.RetainedFrom
		if r.Revision != old.Revision || r.Digest != RecordDigest(old) || r.Result != plainObservation(old.Observation) {
			return ErrDenied
		}
	}
	return nil
}
func validateSemantic(p ReadbackProof) error {
	i := p.Intent
	o := p.ObservedResult
	if o.Outcome != Confirmed {
		return nil
	}
	if i.Operation != CheckDelivery && i.Operation != Merge && i.Operation != ReconcileIssue && i.Operation != UploadCommit {
		return nil
	}
	r := p.Requirements
	if r == nil || r.Policy != i.Policy {
		return ErrDenied
	}
	// Reconstruct the exact existing payload preimage: requirements are not an
	// independent policy assertion. #379 must still reload its reviewed definition.
	type common struct {
		FenceEpoch  uint64
		RoleBinding string
		Reference
		Actor, Repository, Base, Branch, Head, BaseHead, ExpectedOld, Policy, Delivery, Worktree string
		RepositoryID                                                                             uint64
	}
	c := common{i.FenceEpoch, i.RoleBinding, i.Reference, i.Actor, i.Repository, i.Base, i.Branch, i.Head, i.BaseHead, i.ExpectedOld, i.Policy, r.Delivery, r.Worktree, i.RepositoryID}
	if i.Operation == UploadCommit {
		bare := i
		bare.Payload = ""
		if r.Publication == nil || digest(struct {
			Intent       Intent
			ObjectDigest string
			Publication  *ReadbackPublication
		}{bare, r.ObjectDigest, r.Publication}) != i.Payload {
			return ErrDenied
		}
		actor, repo, commit := false, false, false
		for _, f := range p.Facts {
			if f.HTTPStatus != 200 {
				continue
			}
			switch f.Kind {
			case "actor":
				actor = f.Login == i.Actor && f.ID != 0
			case "repository":
				repo = f.Target.Repository == i.Repository && f.ID == i.RepositoryID && f.Permissions != nil && f.Permissions.Push
			case "commit":
				if f.Target.Repository == i.Repository && f.Target.Head == i.Head && f.SHA == i.Head && f.Tree != nil && len(f.Parents) == 1 && f.Author != nil && f.Committer != nil {
					commit = f.Tree.SHA == r.Publication.Tree && f.Parents[0].SHA == i.BaseHead && f.MessageDigest == r.Publication.MessageDigest && samePerson(*f.Author, r.Publication.Author) && samePerson(*f.Committer, r.Publication.Committer)
				}
			}
		}
		if !actor || !repo || !commit || o.Head != i.Head {
			return ErrDenied
		}
		return nil
	}
	if i.Operation == ReconcileIssue {
		if r.Completion == "" || r.IssueID == 0 || r.IssueNumber == 0 || digest(struct {
			Target          common
			Completion      string
			IssueID, Number uint64
		}{c, r.Completion, r.IssueID, r.IssueNumber}) != i.Payload {
			return ErrDenied
		}
		for n := len(p.Facts) - 1; n >= 0; n-- {
			f := p.Facts[n]
			if f.Kind == "issue" && f.HTTPStatus == 200 {
				if f.Target.Repository == i.Repository && f.Target.Issue == r.IssueNumber && f.ID == r.IssueID && f.Number == r.IssueNumber && f.State == "closed" && f.StateReason == "completed" {
					return nil
				}
				return ErrDenied
			}
		}
		return ErrDenied
	}
	if digest(struct {
		Target                        common
		PR                            uint64
		Method, Tree, Protection      string
		Checks, Reviewers, PostChecks []string
	}{c, i.PR, r.Method, r.Tree, r.Protection, r.Checks, r.Reviewers, r.PostChecks}) != i.Payload {
		return ErrDenied
	}
	actor, repo, pull, protection, rules, permission, ancestry, commit, containment := false, false, false, false, false, false, false, false, false
	base := ""
	checks := map[string]bool{}
	post := map[string]bool{}
	latest := map[string]ReadbackFact{}
	for _, f := range p.Facts {
		if f.HTTPStatus < 200 || f.HTTPStatus >= 300 {
			continue
		}
		if f.Kind != "actor" && f.Target.Repository != i.Repository {
			return ErrDenied
		}
		switch f.Kind {
		case "actor":
			if f.Login != i.Actor || f.ID == 0 {
				return ErrDenied
			}
			actor = true
		case "repository":
			if f.FullName != i.Repository || f.ID != i.RepositoryID || f.DefaultBranch != i.Base {
				return ErrDenied
			}
			if f.Permissions == nil || !f.Permissions.Push || !(r.Method == "squash" && f.AllowSquash || r.Method == "merge" && f.AllowMerge) {
				return ErrDenied
			}
			repo = true
		case "pull":
			if !canonicalPullURL(f.HTMLURL, i.Repository, i.PR) || f.User == nil || f.User.Login != i.Actor || f.Target.PR != i.PR || f.Number != i.PR || f.Head == nil || f.Base == nil || f.Head.SHA != i.Head || f.Head.Ref != i.Branch || f.Base.Ref != i.Base || f.Head.Repo.ID != i.RepositoryID || f.Base.Repo.ID != i.RepositoryID || f.Head.Repo.FullName != i.Repository || f.Base.Repo.FullName != i.Repository || f.Draft {
				return ErrDenied
			}
			if i.Operation == CheckDelivery {
				if f.Merged || f.State != "open" || f.Base.SHA != i.BaseHead || f.Mergeable == nil || !*f.Mergeable || f.MergeableState != "clean" {
					return ErrDenied
				}
			}
			if f.Merged && f.MergeCommitSHA == o.Merge && f.State == "closed" {
				pull = true
			}
			if i.Operation == CheckDelivery {
				pull = true
			}
		case "ref":
			if f.Object == nil {
				return ErrDenied
			}
			if f.Target.Ref == i.Base {
				base = f.Object.SHA
			}
			if f.Target.Ref == i.Branch && f.Object.SHA != i.Head {
				return ErrDenied
			}
		case "rules":
			if f.Target.Ref != i.Base || len(f.Rules) != 0 {
				return ErrDenied
			}
			rules = true
		case "permission":
			if f.Permission != "write" || f.Role != "write" {
				return ErrDenied
			}
			permission = true
		case "protection":
			v := f.Protection
			if f.Target.Ref != i.Base || f.PolicyDigest != r.Protection || v == nil || !v.Admins.Enabled || v.Checks == nil || !v.Checks.Strict || v.Reviews == nil || v.Reviews.Count < len(r.Reviewers) || !v.Reviews.DismissStale || !v.Reviews.LastPush {
				return ErrDenied
			}
			for _, b := range v.Reviews.Bypass {
				if len(b) != 0 {
					return ErrDenied
				}
			}
			names := map[string]bool{}
			for _, n := range v.Checks.Contexts {
				names[n] = true
			}
			for _, n := range v.Checks.Checks {
				if n.AppID != -1 {
					return ErrDenied
				}
				names[n.Context] = true
			}
			if len(names) != len(r.Checks) {
				return ErrDenied
			}
			for _, n := range r.Checks {
				if !names[n] {
					return ErrDenied
				}
			}
			protection = true
		case "comparison":
			if (f.Status != "ahead" && f.Status != "identical") || f.Behind != 0 {
				return ErrDenied
			}
			if f.Target.ComparisonBase == i.BaseHead && f.Target.ComparisonHead == i.Head {
				ancestry = true
			}
			if f.Target.ComparisonBase == o.Merge && f.Target.ComparisonHead == o.BaseHead {
				containment = true
			}
		case "commit":
			if f.SHA == o.Merge && f.Target.Head == o.Merge && f.Tree != nil && f.Tree.SHA == r.Tree {
				if r.Method == "squash" && len(f.Parents) == 1 && f.Parents[0].SHA == i.BaseHead {
					commit = true
				}
				if r.Method == "merge" && len(f.Parents) == 2 && f.Parents[0].SHA == i.BaseHead && f.Parents[1].SHA == i.Head {
					commit = true
				}
			}
		case "check_run", "commit_status":
			name := f.Name
			good := f.Status == "completed" && f.Conclusion == "success"
			if f.Kind == "commit_status" {
				name = f.Context
				good = f.State == "success"
			}
			if name == "" {
				continue
			}
			if f.ID == 0 {
				return ErrDenied
			}
			if f.Kind == "check_run" && (f.App == nil || f.App.ID == 0) {
				return ErrDenied
			}
			if f.Kind == "commit_status" && (f.Creator == nil || f.Creator.ID == 0) {
				return ErrDenied
			}
			if f.HeadSHA != f.Target.Head {
				return ErrDenied
			}
			if f.Target.Head == i.Head {
				checks[name] = good
			}
			if f.Target.Head == o.Merge {
				post[name] = good
			}
		case "review":
			if f.Target.PR != i.PR {
				return ErrDenied
			}
			if f.User == nil || f.User.Login == "" || f.ID == 0 {
				return ErrDenied
			}
			if f.State != "COMMENTED" && f.ID > latest[f.User.Login].ID {
				latest[f.User.Login] = f
			}
		}
	}
	if !actor || !repo || !pull {
		return ErrDenied
	}
	for _, f := range latest {
		if f.State == "CHANGES_REQUESTED" {
			return ErrDenied
		}
	}
	if i.Operation == CheckDelivery {
		if !protection || !rules || !permission || !ancestry || base != i.BaseHead || len(r.Checks) == 0 || len(r.Reviewers) == 0 {
			return ErrDenied
		}
		for _, n := range r.Checks {
			if !checks[n] {
				return ErrDenied
			}
		}
		for _, n := range r.Reviewers {
			f := latest[n]
			if n == i.Actor || f.State != "APPROVED" || f.CommitID != i.Head {
				return ErrDenied
			}
		}
	} else {
		if !protection || !rules || !permission || !ancestry || !commit || !containment || base != o.BaseHead || len(r.PostChecks) == 0 {
			return ErrDenied
		}
		for _, n := range r.Checks {
			if !checks[n] {
				return ErrDenied
			}
		}
		for _, n := range r.Reviewers {
			f := latest[n]
			if n == i.Actor || f.State != "APPROVED" || f.CommitID != i.Head {
				return ErrDenied
			}
		}
		for _, n := range r.PostChecks {
			if !post[n] {
				return ErrDenied
			}
		}
	}
	return nil
}

var readbackText = regexp.MustCompile(`^[A-Za-z0-9_./:@+ ()\[\]~-]*$`)

func safeReadbackFact(f ReadbackFact) bool {
	var safe func(reflect.Value) bool
	safe = func(v reflect.Value) bool {
		switch v.Kind() {
		case reflect.String:
			s := v.String()
			low := strings.ToLower(s)
			return len(s) <= 512 && readbackText.MatchString(s) && !strings.Contains(low, "canary") && !strings.Contains(low, "bearer") && !strings.Contains(low, "ghp_") && !strings.Contains(low, "github_pat_")
		case reflect.Pointer:
			return v.IsNil() || safe(v.Elem())
		case reflect.Struct:
			for n := 0; n < v.NumField(); n++ {
				if !safe(v.Field(n)) {
					return false
				}
			}
		case reflect.Slice:
			for n := 0; n < v.Len(); n++ {
				if !safe(v.Index(n)) {
					return false
				}
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				if !safe(it.Key()) || !safe(it.Value()) {
					return false
				}
			}
		}
		return true
	}
	return safe(reflect.ValueOf(f))
}

func canonicalPullURL(raw, repo string, number uint64) bool {
	if number == 0 || !repoName.MatchString(repo) {
		return false
	}
	return raw == "https://github.com/"+repo+"/pull/"+strconv.FormatUint(number, 10)
}

// ReadbackPerson retains actual GitHub Git database identity metadata.
type ReadbackPerson = gitPerson
type ReadbackPublication struct {
	Tree, Parent, MessageDigest string
	Author, Committer           ReadbackPerson
}

func publicationFor(o GitObject) *ReadbackPublication {
	if o.Kind != "commit" {
		return nil
	}
	p, e := commitPayload(o)
	if e != nil {
		return nil
	}
	return &ReadbackPublication{p.Tree, p.Parents[0], messageDigest(p.Message), p.Author, p.Committer}
}
