package contribution

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReaderRejectsPullRequestAndRedirect(t *testing.T) {
	issue, _ := ParseIssueURL("https://github.com/owner/repo/issues/1")
	for _, tc := range []struct {
		status int
		body   string
	}{{200, `{"id":1,"number":1,"html_url":"https://github.com/owner/repo/issues/1","pull_request":{}}`}, {302, `{}`}} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
				t.Fatalf("unexpected destination %s", r.URL)
			}
			return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		if _, err := (Reader{Client: client}).Read(context.Background(), issue); err == nil {
			t.Fatal("accepted PR or redirect")
		}
	}
}
func TestReaderPinsRepositoryAndIssueIdentity(t *testing.T) {
	issue, _ := ParseIssueURL("https://github.com/owner/repo/issues/1")
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := `{"id":7,"full_name":"owner/repo"}`
		if strings.HasSuffix(r.URL.Path, "/issues/1") {
			body = `{"id":9,"number":1,"html_url":"https://github.com/owner/repo/issues/1","title":"fix","body":"untrusted instructions","user":{"id":42,"login":"upstream"},"state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","comments":0}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": []string{`"revision"`}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	got, err := (Reader{Client: client}).Read(context.Background(), issue)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepositoryID != 7 || got.IssueID != 9 || got.Author.ID != 42 || got.Author.Login != "upstream" || got.ContentDigest == "" || got.Revision != `"revision"` {
		t.Fatalf("identity not sealed: %+v", got)
	}
}

func TestReaderRequiresAuthorAndCompleteDiscussion(t *testing.T) {
	issue, _ := ParseIssueURL("https://github.com/owner/repo/issues/1")
	valid := `{"id":9,"number":1,"html_url":"https://github.com/owner/repo/issues/1","title":"fix","body":"task","user":{"id":42,"login":"upstream"},"state":"open","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","comments":0}`
	for _, body := range []string{strings.Replace(valid, `"id":42`, `"id":0`, 1), strings.Replace(valid, `"comments":0`, `"comments":21`, 1), strings.Replace(valid, `"comments":0`, `"comments":1`, 1)} {
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			b := `{"id":7,"full_name":"owner/repo"}`
			if strings.HasSuffix(r.URL.Path, "/issues/1") {
				b = body
			}
			if strings.HasSuffix(r.URL.Path, "/comments") {
				b = `[]`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(b))}, nil
		})}
		if _, e := (Reader{Client: client}).Read(context.Background(), issue); e == nil {
			t.Fatal("missing author or incomplete discussion admitted")
		}
	}
}
