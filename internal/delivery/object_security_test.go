package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObjectRedirectDeniedWithoutFollowing(t *testing.T) {
	p := objectPlan(t)
	p.Object = p.Objects[0]
	i, _ := intentFor(p, UploadBlob)
	followed := false
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followed = true }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	g, _ := newGitHub(src.URL, fixtureCustody{}, fixturePrivacy{})
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if followed || o.Outcome == Confirmed {
		t.Fatal("redirect followed", o)
	}
}
func TestObjectRepairCannotBypassUnknownGraph(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{faultKind: "blob", when: "before"}
	g := serveObjects(t, p, f)
	owner := &memoryOwner{}
	e := NewEngine(owner, fixtureSource{p}, &fixtureAuthority{}, g)
	r, err := e.Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Unknown {
		t.Fatal(r, err)
	}
	repaired := p
	repaired.Candidate = "different-accepted-candidate"
	e.source = fixtureSource{repaired}
	_, err = e.Execute(context.Background(), repaired.Reference, Publish)
	if err != ErrConflict {
		t.Fatal("replacement escaped unknown", err)
	}
	if f.ref {
		t.Fatal("ref published")
	}
}
func TestObjectBoundsAndUnreachableDenied(t *testing.T) {
	p := objectPlan(t)
	o := GitObject{Kind: "blob", Raw: []byte(strings.Repeat("x", maxObjectBytes+1))}
	o.OID = objectOID(o.Kind, o.Raw)
	if objectValid(o) {
		t.Fatal("oversize")
	}
	p.Objects = append([]GitObject{{Kind: "blob", OID: objectOID("blob", []byte("unreachable")), Raw: []byte("unreachable")}}, p.Objects...)
	if validateGraph(p) == nil {
		t.Fatal("unreachable upload accepted")
	}
}

type revokingCustody struct {
	revoked   bool
	mutations int
}

func (c *revokingCustody) Use(_ context.Context, _ Intent, fn func([]byte) error) error {
	if c.revoked {
		return ErrDenied
	}
	return fn([]byte("synthetic-controller-token"))
}
func TestFreshCustodyAfterFinalGuard(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{}
	g := serveObjects(t, p, f)
	c := &revokingCustody{}
	g.custody = c
	p.Object = p.Objects[0]
	i, _ := intentFor(p, UploadBlob)
	calls := 0
	o := g.Apply(context.Background(), i, p, func(context.Context) error {
		calls++
		if calls >= 3 {
			c.revoked = true
		}
		return nil
	})
	if o.Outcome == Confirmed || len(f.writes) != 0 {
		t.Fatal("stale credential mutated", o, f.writes)
	}
}
