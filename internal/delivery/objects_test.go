package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func objectPlan(t *testing.T) Plan {
	t.Helper()
	g, s := commitFixture(t)
	objects, e := g.ExtractObjects(context.Background(), s.Commit, s.Tree, s.Parent)
	if e != nil {
		t.Fatal(e)
	}
	p := fixturePlan()
	p.Commit = s
	p.Head = s.Commit
	p.BaseHead = s.Parent
	p.Objects = objects
	if e = validateGraph(p); e != nil {
		t.Fatal(e)
	}
	return p
}

func TestExtractActualAcceptedGraph(t *testing.T) {
	p := objectPlan(t)
	if len(p.Objects) != 3 {
		t.Fatalf("objects %d", len(p.Objects))
	}
	for _, o := range p.Objects {
		if !objectValid(o) {
			t.Fatal(o.Kind)
		}
	}
	p.Objects[0].Raw = []byte("drift")
	if validateGraph(p) == nil {
		t.Fatal("drift accepted")
	}
}
func TestObjectPositiveProfile(t *testing.T) {
	p := objectPlan(t)
	raw := p.Objects[len(p.Objects)-1].Raw
	for _, header := range []string{"gpgsig signed\n", "encoding UTF-8\n", "parent " + p.BaseHead + "\n", "x-extension anything\n"} {
		o := GitObject{Kind: "commit", Raw: []byte(strings.Replace(string(raw), "\n\n", "\n"+header+"\n", 1))}
		o.OID = objectOID(o.Kind, o.Raw)
		if _, e := commitPayload(o); e == nil {
			t.Fatal("unsupported header accepted", header)
		}
	}
	for _, name := range []string{".env", ".ssh/key", "keys/private.pem", ".aws/credentials"} {
		if !privatePath(name) {
			t.Fatal(name)
		}
	}
	for _, text := range []string{"ghp_secret", "-----BEGIN PRIVATE KEY-----", "password=secret"} {
		if !secretBytes([]byte(text)) {
			t.Fatal(text)
		}
	}
	p = objectPlan(t)
	p.Object = GitObject{Kind: "blob", Raw: []byte("unapproved")}
	p.Object.OID = objectOID("blob", p.Object.Raw)
	if _, e := intentFor(p, UploadBlob); e == nil {
		t.Fatal("outside graph accepted")
	}
}

type objectServer struct {
	sync.Mutex
	present         map[string]bool
	writes          map[string]int
	faultKind, when string
	wrong           bool
	order           []string
	ref             bool
}

func serveObjects(t *testing.T, p Plan, f *objectServer) *GitHub {
	t.Helper()
	f.present = map[string]bool{}
	f.writes = map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Lock()
		defer f.Unlock()
		send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		if r.URL.Path == "/repos/owner/repo/git/commits/"+p.BaseHead {
			send(map[string]string{"sha": p.BaseHead})
			return
		}
		switch r.URL.Path {
		case "/user":
			send(map[string]any{"login": p.Actor, "id": 42})
			return
		case "/repos/owner/repo":
			send(map[string]any{"id": 1, "full_name": p.Repository, "default_branch": "main", "permissions": map[string]bool{"push": true}})
			return
		case "/repos/owner/repo/git/ref/heads/contribution/run":
			if !f.ref {
				w.WriteHeader(404)
				return
			}
			send(map[string]any{"ref": "refs/heads/" + p.Branch, "object": map[string]string{"sha": p.Head}})
			return
		case "/repos/owner/repo/git/refs":
			for _, o := range p.Objects {
				if !f.present[o.OID] {
					t.Error("ref before objects")
				}
			}
			f.ref = true
			f.order = append(f.order, "ref")
			send(map[string]any{})
			return
		}
		for _, o := range p.Objects {
			path := "/repos/owner/repo/git/" + o.Kind + "s"
			if r.URL.Path != path && r.URL.Path != path+"/"+o.OID {
				continue
			}
			if r.Method == "POST" {
				f.writes[o.OID]++
				f.order = append(f.order, o.Kind)
				if f.faultKind == o.Kind && f.when == "before" {
					w.WriteHeader(503)
					return
				}
				var got map[string]any
				if json.NewDecoder(r.Body).Decode(&got) != nil {
					t.Error("bad payload")
				}
				switch o.Kind {
				case "blob":
					if got["encoding"] != "base64" || got["content"] != base64.StdEncoding.EncodeToString(o.Raw) {
						t.Error("blob payload")
					}
				case "tree":
					if _, ok := got["tree"]; !ok {
						t.Error("tree payload")
					}
				case "commit":
					cp, _ := commitPayload(o)
					if got["message"] != cp.Message || got["tree"] != cp.Tree {
						t.Error("commit payload")
					}
				}
				f.present[o.OID] = true
				if f.faultKind == o.Kind && f.when == "after" {
					w.WriteHeader(503)
					return
				}
				oid := o.OID
				if f.wrong {
					oid = strings.Repeat("f", 40)
				}
				send(map[string]string{"sha": oid})
				return
			}
			if !f.present[o.OID] {
				w.WriteHeader(404)
				return
			}
			switch o.Kind {
			case "blob":
				send(map[string]any{"sha": o.OID, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(o.Raw)})
			case "tree":
				entries, _ := treeEntries(o.Raw)
				send(map[string]any{"sha": o.OID, "tree": entries, "truncated": false})
			case "commit":
				cp, _ := commitPayload(o)
				send(map[string]any{"sha": o.OID, "message": cp.Message, "tree": map[string]string{"sha": cp.Tree}, "parents": []any{map[string]string{"sha": cp.Parents[0]}}, "author": cp.Author, "committer": cp.Committer})
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	g, e := newGitHub(srv.URL, fixtureCustody{}, fixturePrivacy{})
	if e != nil {
		t.Fatal(e)
	}
	return g
}

func TestObjectTransferBeforeRef(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{}
	g := serveObjects(t, p, f)
	e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
	r, err := e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Confirmed {
		t.Fatalf("%+v %v", r, err)
	}
	if fmt.Sprint(f.order) != "[blob tree commit ref]" {
		t.Fatal(f.order)
	}
}
func TestObjectLossReconcilesNeverResends(t *testing.T) {
	for _, kind := range []string{"blob", "tree", "commit"} {
		for _, when := range []string{"before", "after"} {
			t.Run(kind+when, func(t *testing.T) {
				p := objectPlan(t)
				f := &objectServer{faultKind: kind, when: when}
				g := serveObjects(t, p, f)
				e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
				r, err := e.Execute(context.Background(), p.Reference, Publish)
				if err != nil || r.Outcome != Unknown {
					t.Fatalf("first %+v %v", r, err)
				}
				r, err = e.Execute(context.Background(), p.Reference, Publish)
				if err != nil {
					t.Fatal(err)
				}
				if when == "before" && r.Outcome != Unknown {
					t.Fatal(r)
				}
				if when == "after" && r.Outcome != Confirmed {
					t.Fatal(r)
				}
				for _, n := range f.writes {
					if n != 1 {
						t.Fatal("resent", f.writes)
					}
				}
			})
		}
	}
}
func TestObjectConcurrentStableSlots(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{}
	g := serveObjects(t, p, f)
	e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = e.Execute(context.Background(), p.Reference, Publish) })
	}
	wg.Wait()
	for _, n := range f.writes {
		if n != 1 {
			t.Fatal(f.writes)
		}
	}
	ids := map[string]bool{}
	for _, o := range p.Objects {
		p.Object = o
		i, err := intentFor(p, objectOp(o))
		if err != nil || ids[i.ID] {
			t.Fatal("nonunique", err)
		}
		ids[i.ID] = true
	}
}
func TestObjectWrongCreateHashUnknown(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{wrong: true}
	g := serveObjects(t, p, f)
	e := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
	r, err := e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Unknown || f.ref {
		t.Fatalf("%+v %v", r, err)
	}
}
