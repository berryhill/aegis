package delivery

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// strictProtection supports classic GitHub branch protection, never a caller
// assertion of protection. The complete returned policy is pinned by digest.
// Ruleset-only protection, custom/admin roles and app-pinned checks are outside
// this initial supported envelope and deny rather than guessing bypass rights.
func (g *GitHub) strictProtection(ctx context.Context, s []byte, i Intent, p Plan) error {
	prefix := "/repos/" + i.Repository
	var rules []json.RawMessage
	if g.request(ctx, s, "GET", prefix+"/rules/branches/"+url.PathEscape(i.Base), nil, &rules) != nil || rules == nil || len(rules) != 0 {
		return ErrDenied
	}
	var raw map[string]any
	if p.ProtectionPolicy == "" || g.request(ctx, s, "GET", prefix+"/branches/"+url.PathEscape(i.Base)+"/protection", nil, &raw) != nil || digest(raw) != p.ProtectionPolicy {
		return ErrDenied
	}
	b, _ := json.Marshal(raw)
	var policy struct {
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
			Count        int                          `json:"required_approving_review_count"`
			DismissStale bool                         `json:"dismiss_stale_reviews"`
			LastPush     bool                         `json:"require_last_push_approval"`
			Bypass       map[string][]json.RawMessage `json:"bypass_pull_request_allowances"`
		} `json:"required_pull_request_reviews"`
	}
	if json.Unmarshal(b, &policy) != nil || !policy.Admins.Enabled || policy.Checks == nil || !policy.Checks.Strict || policy.Reviews == nil || len(p.RequiredReviewers) == 0 || policy.Reviews.Count < len(p.RequiredReviewers) || !policy.Reviews.DismissStale || !policy.Reviews.LastPush {
		return ErrDenied
	}
	for _, v := range policy.Reviews.Bypass {
		if len(v) != 0 {
			return ErrDenied
		}
	}
	names := map[string]bool{}
	for _, n := range policy.Checks.Contexts {
		if n == "" || names[n] {
			return ErrDenied
		}
		names[n] = true
	}
	for _, c := range policy.Checks.Checks {
		if c.AppID != -1 {
			return ErrDenied
		}
		names[c.Context] = true
	}
	got := []string{}
	for n := range names {
		got = append(got, n)
	}
	sort.Strings(got)
	want := append([]string(nil), p.RequiredChecks...)
	sort.Strings(want)
	if len(got) == 0 || strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		return ErrDenied
	}
	var permission struct {
		Permission string
		Role       string `json:"role_name"`
	}
	if g.request(ctx, s, "GET", prefix+"/collaborators/"+url.PathEscape(i.Actor)+"/permission", nil, &permission) != nil || permission.Permission != "write" || permission.Role != "write" {
		return ErrDenied
	}
	return nil
}
