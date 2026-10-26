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

// Model GitHub's canonical empty-tree response, independently of the fixture's
// storage representation. A valid local deletion must upload [] rather than null.
type v4EmptyTreeTransport struct {
	base     http.RoundTripper
	nullTree bool
}

func (e *v4EmptyTreeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/git/trees") {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		var payload map[string]any
		if err := json.Unmarshal(b, &payload); err != nil {
			return nil, err
		}
		e.nullTree = payload["tree"] == nil
	}
	res, err := e.base.RoundTrip(r)
	if err != nil {
		return res, err
	}
	if r.Method == "GET" && strings.Contains(r.URL.Path, "/git/trees/") && res.StatusCode == http.StatusOK {
		b, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return nil, err
		}
		b = bytes.ReplaceAll(b, []byte(`"tree":null`), []byte(`"tree":[]`))
		res.Body = io.NopCloser(bytes.NewReader(b))
		res.ContentLength = int64(len(b))
	}
	return res, nil
}

func TestV4EmptyTreeActualGit(t *testing.T) {
	g, source := commitFixture(t)
	if err := os.Remove(filepath.Join(g.Directory, "source")); err != nil {
		t.Fatal(err)
	}
	source.Files = []File{{Path: "source", Mode: "000000"}}
	tree, commit, err := g.prepare(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	source.Tree, source.Commit = tree, commit
	objects, err := g.ExtractObjects(context.Background(), commit, tree, source.Parent)
	if err != nil {
		t.Fatal(err)
	}
	plan := fixturePlan()
	plan.Commit, plan.Head, plan.BaseHead, plan.Objects = source, commit, source.Parent, objects
	if err := validateGraph(plan); err != nil {
		t.Fatal(err)
	}
	server := &objectServer{}
	github := serveObjects(t, plan, server)
	transport := &v4EmptyTreeTransport{base: github.client.Transport}
	github.client.Transport = transport
	result, err := NewEngine(&memoryOwner{}, fixtureSource{plan}, &fixtureAuthority{}, github).Execute(context.Background(), plan.Reference, Publish)
	if err != nil || result.Outcome != Confirmed || transport.nullTree {
		t.Fatalf("valid empty-tree transfer: outcome=%s null_payload=%v err=%v", result.Outcome, transport.nullTree, err)
	}
}

type v4RotatingActorCustody struct{ calls int }

func (c *v4RotatingActorCustody) Use(_ context.Context, _ Intent, fn func([]byte) error) error {
	c.calls++
	if c.calls >= 4 {
		return fn([]byte("different-actor-token"))
	}
	return fn([]byte("synthetic-controller-token"))
}

type v4ActorTransport struct {
	base             http.RoundTripper
	mismatchedWrites int
}

func (a *v4ActorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "Bearer different-actor-token" {
		if r.Method != "GET" {
			a.mismatchedWrites++
		}
		if r.URL.Path == "/user" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"login":"different-actor"}`)), Request: r}, nil
		}
	}
	return a.base.RoundTrip(r)
}

func TestV4ObjectWriteCredentialActorChecked(t *testing.T) {
	plan := objectPlan(t)
	server := &objectServer{}
	github := serveObjects(t, plan, server)
	github.custody = &v4RotatingActorCustody{}
	transport := &v4ActorTransport{base: github.client.Transport}
	github.client.Transport = transport
	plan.Object = plan.Objects[0]
	intent, err := intentFor(plan, UploadBlob)
	if err != nil {
		t.Fatal(err)
	}
	result := github.Apply(context.Background(), intent, plan, func(context.Context) error { return nil })
	if transport.mismatchedWrites != 0 || result.Outcome == Confirmed {
		t.Fatalf("unchecked final actor: writes=%d outcome=%s", transport.mismatchedWrites, result.Outcome)
	}
}
