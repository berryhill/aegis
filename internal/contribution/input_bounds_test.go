package contribution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReaderExactSourceByteBoundsAndLatestDiscussion(t *testing.T) {
	issue, _ := ParseIssueURL("https://github.com/owner/repo/issues/1")
	for _, tc := range []struct {
		name    string
		body    string
		comment string
		count   int
		want    error
	}{
		{"body_exact_utf8", strings.Repeat("é", 32768), "latest owner instruction", 20, nil},
		{"body_extra_byte", strings.Repeat("é", 32768) + "x", "latest", 1, ErrSourceIssueBound},
		{"comment_exact", "task", strings.Repeat("é", 16384), 20, nil},
		{"comment_extra_byte", "task", strings.Repeat("é", 16384) + "x", 1, ErrSourceDiscussionBound},
		{"incomplete_count", "task", "latest", 21, ErrSourceDiscussionBound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := map[string]any{"id": 9, "number": 1, "html_url": issue.URL, "title": "fix", "body": tc.body, "user": Author{ID: 42, Login: "owner"}, "state": "open", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z", "comments": tc.count}
			discussion := []map[string]any{}
			for i := 0; i < tc.count; i++ {
				body := "earlier"
				if i == tc.count-1 {
					body = tc.comment
				}
				discussion = append(discussion, map[string]any{"id": i + 1, "user": Author{ID: 42, Login: "owner"}, "body": body, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"})
			}
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				var wire []byte
				switch {
				case strings.HasSuffix(r.URL.Path, "/comments"):
					wire, _ = json.Marshal(discussion)
				case strings.HasSuffix(r.URL.Path, "/issues/1"):
					wire, _ = json.Marshal(remote)
				default:
					wire = []byte(`{"id":7,"full_name":"owner/repo"}`)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(wire)))}, nil
			})}
			source, err := (Reader{Client: client}).Read(context.Background(), issue)
			if tc.want != nil {
				if !errors.Is(err, tc.want) || source.IssueID != 0 {
					t.Fatal("missing precise atomic rejection", err)
				}
				return
			}
			if err != nil || source.Body != tc.body || len(source.Discussion) != tc.count || source.Discussion[tc.count-1].Body != tc.comment || source.Discussion[tc.count-1].Author.ID != 42 || source.Discussion[tc.count-1].ContentDigest == "" {
				t.Fatal("lost complete content/provenance", err)
			}
		})
	}
}

func TestReaderExactHTTPResponseBound(t *testing.T) {
	for _, size := range []int{256 << 10, (256 << 10) + 1} {
		wire := `{"id":7,"full_name":"owner/repo"}`
		wire += strings.Repeat(" ", size-len(wire))
		reader := Reader{Client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
		})}}
		var target any
		_, _, err := reader.get(context.Background(), "/repos/owner/repo", &target)
		if size == 256<<10 && err != nil {
			t.Fatal("exact HTTP byte ceiling rejected", err)
		}
		if size > 256<<10 && !errors.Is(err, ErrSourceResponseBound) {
			t.Fatal("HTTP overflow unclassified", err)
		}
	}
}
