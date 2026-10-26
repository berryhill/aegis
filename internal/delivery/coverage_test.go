package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Response corruption is a synthetic negative fixture, not real GitHub proof.
type corruptTransport struct {
	base http.RoundTripper
	kind string
}

func (c corruptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, e := c.base.RoundTrip(r)
	if e != nil {
		return res, e
	}
	b, e := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if e != nil {
		return nil, e
	}
	switch c.kind {
	case "stale_check":
		if strings.Contains(r.URL.Path, "check-runs") {
			b = bytes.ReplaceAll(b, []byte(strings.Repeat("a", 40)), []byte(strings.Repeat("f", 40)))
		}
	case "stale_review":
		if strings.HasSuffix(r.URL.Path, "/reviews") {
			b = bytes.ReplaceAll(b, []byte(strings.Repeat("a", 40)), []byte(strings.Repeat("f", 40)))
		}
	case "wrong_ref":
		if strings.HasSuffix(r.URL.Path, "/pulls/7") {
			b = bytes.ReplaceAll(b, []byte(`"ref":"contribution/run"`), []byte(`"ref":"other/ref"`))
		}
	case "wrong_base":
		if strings.HasSuffix(r.URL.Path, "/pulls/7") {
			b = bytes.ReplaceAll(b, []byte(`"ref":"main"`), []byte(`"ref":"other"`))
		}
	case "pagination":
		if strings.Contains(r.URL.Path, "check-runs") {
			res.Header.Set("Link", `<https://api.github.com/next>; rel="next"`)
		}
	case "check_count":
		if strings.Contains(r.URL.Path, "check-runs") {
			b = bytes.ReplaceAll(b, []byte(`"total_count":1`), []byte(`"total_count":2`))
		}
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	res.ContentLength = int64(len(b))
	return res, nil
}
func TestRemoteStaleAndIncompleteCoverage(t *testing.T) {
	for _, kind := range []string{"stale_check", "stale_review", "wrong_ref", "wrong_base", "pagination", "check_count"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			g.client.Transport = corruptTransport{g.client.Transport, kind}
			i, _ := intentFor(p, Merge)
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome == Confirmed || f.writes != 0 {
				b, _ := json.Marshal(o)
				t.Fatalf("unsafe result %s writes=%d", b, f.writes)
			}
		})
	}
}
