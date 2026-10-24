package contribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ReadIssueAction = "github.read_contribution_issue.v1"
const MaxDiscussion = 20

// Source is bounded untrusted content with immutable observed-byte provenance.
// Author association is deliberately not converted into owner authority.
type Source struct {
	Author              Author    `json:"user"`
	State               string    `json:"state"`
	CreatedAt           time.Time `json:"created_at"`
	ObservedAt          time.Time `json:"observed_at"`
	URL                 IssueURL  `json:"url"`
	RepositoryID        uint64    `json:"repository_id"`
	IssueID             uint64    `json:"issue_id"`
	Title               string    `json:"title"`
	Body                string    `json:"body"`
	UpdatedAt           time.Time `json:"updated_at"`
	Revision            string    `json:"revision"`
	ContentDigest       string    `json:"content_digest"`
	Discussion          []Comment `json:"discussion"`
	DiscussionTruncated bool      `json:"discussion_truncated"`
}
type Comment struct {
	Author        Author    `json:"user"`
	CreatedAt     time.Time `json:"created_at"`
	ID            uint64    `json:"id"`
	Body          string    `json:"body"`
	UpdatedAt     time.Time `json:"updated_at"`
	ContentDigest string    `json:"content_digest"`
}

// Reader is controller-owned, public-read-only and has no credential access.
// ReadIssueAction requires separate admission by the caller; repository metadata
// permission is not sufficient. Redirects always fail, including same-origin.
type Author struct {
	ID    uint64 `json:"id"`
	Login string `json:"login"`
}
type Reader struct{ Client *http.Client }

func Digest(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func (r Reader) get(ctx context.Context, path string, target any) (string, []byte, error) {
	client := http.Client{Timeout: 15 * time.Second}
	if r.Client != nil {
		client = *r.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(req)
	if err != nil {
		return "", nil, errors.New("contribution source unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, errors.New("contribution source HTTP status denied")
	}
	wire, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil || len(wire) > 256<<10 {
		return "", nil, errors.New("contribution source exceeds bound")
	}
	// GitHub responses are an open upstream schema. Project only explicitly
	// selected fields; these bytes never pass as an internal authority contract.
	if err = json.Unmarshal(wire, target); err != nil {
		return "", nil, errors.New("contribution source malformed")
	}
	return response.Header.Get("ETag"), wire, nil
}
func (r Reader) Read(ctx context.Context, issue IssueURL) (Source, error) {
	parsed, err := ParseIssueURL(issue.URL)
	if err != nil || parsed != issue {
		return Source{}, errors.New("invalid issue identity")
	}
	path := "/repos/" + issue.Owner + "/" + issue.Repository
	var repo struct {
		ID       uint64 `json:"id"`
		FullName string `json:"full_name"`
	}
	if _, _, err = r.get(ctx, path, &repo); err != nil {
		return Source{}, err
	}
	if repo.ID == 0 || repo.FullName != issue.Owner+"/"+issue.Repository {
		return Source{}, errors.New("repository identity changed")
	}
	var remote struct {
		Author      Author          `json:"user"`
		State       string          `json:"state"`
		CreatedAt   time.Time       `json:"created_at"`
		ID          uint64          `json:"id"`
		Number      uint64          `json:"number"`
		URL         string          `json:"html_url"`
		Title       string          `json:"title"`
		Body        string          `json:"body"`
		UpdatedAt   time.Time       `json:"updated_at"`
		Comments    uint64          `json:"comments"`
		PullRequest json.RawMessage `json:"pull_request"`
	}
	revision, wire, err := r.get(ctx, fmt.Sprintf("%s/issues/%d", path, issue.Number), &remote)
	if err != nil {
		return Source{}, err
	}
	if remote.ID == 0 || remote.Number != issue.Number || remote.URL != issue.URL || len(remote.PullRequest) != 0 || remote.UpdatedAt.IsZero() || len(remote.Body) > 65536 || len(remote.Title) > 1024 {
		return Source{}, errors.New("issue object identity or bounds denied")
	}
	if remote.Author.ID == 0 || remote.Author.Login == "" || remote.CreatedAt.IsZero() || remote.State != "open" || remote.Comments > MaxDiscussion {
		return Source{}, errors.New("issue authorship, state or complete discussion coverage denied")
	}
	out := Source{Author: remote.Author, State: remote.State, CreatedAt: remote.CreatedAt, ObservedAt: time.Now().UTC(), URL: issue, RepositoryID: repo.ID, IssueID: remote.ID, Title: remote.Title, Body: remote.Body, UpdatedAt: remote.UpdatedAt, Revision: revision, ContentDigest: Digest(wire), Discussion: []Comment{}}
	if remote.Comments > 0 {
		// Read one bounded latest page. The observed issue revision and comment
		// hashes make concurrent edits explicit snapshots, not claimed latest truth.
		page := uint64(1)
		var comments []Comment
		_, _, err = r.get(ctx, fmt.Sprintf("%s/issues/%d/comments?per_page=%d&page=%d", path, issue.Number, MaxDiscussion, page), &comments)
		if err != nil {
			return Source{}, err
		}
		if len(comments) > MaxDiscussion || uint64(len(comments)) != remote.Comments {
			return Source{}, errors.New("discussion bound exceeded")
		}
		for _, c := range comments {
			if c.ID == 0 || c.Author.ID == 0 || c.Author.Login == "" || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || len(c.Body) > 32768 {
				return Source{}, errors.New("discussion malformed")
			}
			b, _ := json.Marshal(c)
			c.ContentDigest = Digest(b)
			out.Discussion = append(out.Discussion, c)
		}
	}
	return out, nil
}
