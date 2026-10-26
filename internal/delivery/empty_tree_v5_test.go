package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type emptyTreeTransport struct {
	base     http.RoundTripper
	nullTree bool
}

func (e *emptyTreeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/git/trees") {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		var p map[string]any
		json.Unmarshal(b, &p)
		if p["tree"] == nil {
			e.nullTree = true
		}
	}
	res, err := e.base.RoundTrip(r)
	if err != nil {
		return res, err
	}
	if r.Method == "GET" && strings.Contains(r.URL.Path, "/git/trees/") && res.StatusCode == 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		b = bytes.ReplaceAll(b, []byte(`"tree":null`), []byte(`"tree":[]`))
		res.Body = io.NopCloser(bytes.NewReader(b))
		res.ContentLength = int64(len(b))
	}
	return res, nil
}
func TestIndependentEmptyTreeActualGit(t *testing.T) {
	g, s := commitFixture(t)
	if e := os.Remove(filepath.Join(g.Directory, "source")); e != nil {
		t.Fatal(e)
	}
	s.Files = []File{{Path: "source", Mode: "000000"}}
	tree, commit, e := g.prepare(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	s.Tree = tree
	s.Commit = commit
	objects, e := g.ExtractObjects(context.Background(), commit, tree, s.Parent)
	if e != nil {
		t.Fatal(e)
	}
	p := fixturePlan()
	p.Commit = s
	p.Head = commit
	p.BaseHead = s.Parent
	p.Objects = objects
	if validateGraph(p) != nil {
		t.Fatal("valid empty tree rejected locally")
	}
	f := &objectServer{}
	h := serveObjects(t, p, f)
	tr := &emptyTreeTransport{base: h.client.Transport}
	h.client.Transport = tr
	r, e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, h).Execute(context.Background(), p.Reference, Publish)
	t.Logf("empty tree=%s outcome=%s null tree payload=%v err=%v", tree, r.Outcome, tr.nullTree, e)
	if e != nil || r.Outcome != Confirmed || tr.nullTree {
		t.Fatal("valid real empty tree cannot transfer with canonical [] response / emits null tree")
	}
}
