// Package contribution defines the closed GitHub contribution intake contract.
// Remote issue content is data, never a source of execution authority.
package contribution

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// IssueURL identifies an issue endpoint, not proof that the remote object is an
// issue: the authenticated reader must independently reject pull_request objects.
type IssueURL struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Number     uint64 `json:"number"`
	URL        string `json:"url"`
}

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// ParseIssueURL accepts only the canonical GitHub HTTPS issue path. Redirects,
// credentials, ports, query strings, fragments and encoded path aliases deny.
func ParseIssueURL(raw string) (IssueURL, error) {
	deny := errors.New("canonical GitHub issue URL required")
	if len(raw) > 512 || strings.TrimSpace(raw) != raw {
		return IssueURL{}, deny
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" || strings.ContainsAny(raw, "%\\\r\n\t#?") {
		return IssueURL{}, deny
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 5 || parts[0] != "" || parts[3] != "issues" || !repositoryPart.MatchString(parts[1]) || !repositoryPart.MatchString(parts[2]) || parts[1] == "." || parts[1] == ".." || parts[2] == "." || parts[2] == ".." {
		return IssueURL{}, deny
	}
	n, err := strconv.ParseUint(parts[4], 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != parts[4] {
		return IssueURL{}, deny
	}
	return IssueURL{Owner: parts[1], Repository: parts[2], Number: n, URL: raw}, nil
}
