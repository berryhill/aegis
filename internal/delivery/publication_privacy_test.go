package delivery

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Synthetic sealed intake stands in for authenticated admission, not effect
// authority. The actual privacy implementation and closed HTTP adapter run below.
func publicationPlan(t *testing.T) Plan {
	t.Helper()
	p := objectPlan(t)
	p.Branch = p.Commit.Branch
	p.Candidate = digest("accepted-candidate")
	now := time.Now().UTC()
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: digest("agent")}
	role := contribution.RoleBinding{Agent: ref, Charter: ref, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/agent"}}
	u, err := contribution.ParseIssueURL("https://github.com/owner/repo/issues/1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := contribution.Seal(contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: "owner", SessionID: "session", Key: "key", PayloadDigest: digest("payload"), QueueItemID: "queue", SubmissionID: "submission", RunID: p.Run, Loop: ref, Controller: ref, Builder: ref, Tester: ref, Source: contribution.Source{URL: u, RepositoryID: p.RepositoryID, IssueID: 9, Author: contribution.Author{ID: 1, Login: "owner"}, State: "open", CreatedAt: now, UpdatedAt: now, ObservedAt: now, ContentDigest: digest("source"), Discussion: []contribution.Comment{}}, Outcome: "behavior", Acceptance: "real test", Invariants: []string{}, Exclusions: []string{}, Repository: p.Repository, Base: p.BaseHead, Worktree: "/owned/worktree", Effect: p.Delivery, Roles: []contribution.RoleBinding{role, role, role}, TemplateDigest: digest("template"), CandidateBudget: 2, AdmittedAt: now, Deadline: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	p.Intake = c
	p.Contract = c.Digest
	return p
}
func publicationReseal(t *testing.T, p *Plan) {
	t.Helper()
	c, e := contribution.Seal(p.Intake)
	if e != nil {
		t.Fatal(e)
	}
	p.Intake = c
	p.Contract = c.Digest
}
func publicationRegraph(t *testing.T, p *Plan, names []string, bodies [][]byte) {
	t.Helper()
	p.Objects = nil
	p.Commit.Files = nil
	var raw []byte
	for n, name := range names {
		b := GitObject{Kind: "blob", Raw: bodies[n]}
		b.OID = objectOID(b.Kind, b.Raw)
		p.Objects = append(p.Objects, b)
		oid, _ := hex.DecodeString(b.OID)
		raw = append(raw, []byte("100644 "+name+"\x00")...)
		raw = append(raw, oid...)
		p.Commit.Files = append(p.Commit.Files, File{Path: name, Mode: "100644", Bytes: b.Raw})
	}
	tree := GitObject{Kind: "tree", Raw: raw}
	tree.OID = objectOID(tree.Kind, raw)
	p.Commit.Tree = tree.OID
	o, e := authoredObject(p.Commit)
	if e != nil {
		t.Fatal(e)
	}
	p.Commit.Commit = o.OID
	p.Head = o.OID
	p.Objects = append(p.Objects, tree, o)
}
func TestPublicationContentSafeGraph(t *testing.T) {
	p := publicationPlan(t)
	for _, op := range []Operation{Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue, TransferGraph, UploadBlob, UploadTree, UploadCommit} {
		child := p
		for _, o := range p.Objects {
			if objectOp(o) == op {
				child.Object = o
			}
		}
		i, e := intentFor(child, op)
		if e != nil {
			t.Fatal(e)
		}
		if e = (PublicationPrivacy{}).Review(context.Background(), i, child); e != nil {
			t.Fatalf("safe %s: %v", op, e)
		}
	}
	publicationRegraph(t, &p, []string{"approval.go", "session.go"}, [][]byte{[]byte("package demo\nvar private_key = 1\nvar token = generateToken()\n// ghp_prefix_documentation\n"), []byte("package demo\n// session and secret scanner implementation\n")})
	i, e := intentFor(p, Publish)
	if e != nil {
		t.Fatal(e)
	}
	if e = (PublicationPrivacy{}).Review(context.Background(), i, p); e != nil {
		t.Fatal("legitimate source denied", e)
	}
	p.Delivery = "no-merge"
	i, _ = intentFor(p, Publish)
	if e = (PublicationPrivacy{}).Review(context.Background(), i, p); e != nil {
		t.Fatal("narrow publication denied", e)
	}
	i, _ = intentFor(p, Merge)
	if (PublicationPrivacy{}).Review(context.Background(), i, p) == nil {
		t.Fatal("no-merge escaped")
	}
}
func TestPublicationDetector(t *testing.T) {
	fixtures := [][]byte{[]byte("ghp_" + strings.Repeat("A", 36)), []byte("-----BEGIN PRIVATE KEY-----"), []byte("api_key = 'fixture-value'")}
	for n, b := range fixtures {
		for encoding, raw := range map[string][]byte{"plain": b, "base64": []byte(base64.StdEncoding.EncodeToString(b)), "hex": []byte(hex.EncodeToString(b)), "double": []byte(base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(b))))} {
			budget := maxGraphBytes
			if publicationInspect(context.Background(), raw, 0, &budget) == nil {
				t.Errorf("pattern %d encoding %s escaped", n, encoding)
			}
		}
	}
	for _, s := range []string{"var private_key = 1", "func private_key() {}", "ghp_prefix_documentation", "var token = generateToken()"} {
		budget := maxGraphBytes
		if e := publicationInspect(context.Background(), []byte(s), 0, &budget); e != nil {
			t.Fatal("code identifier denied", e)
		}
	}
	for _, raw := range [][]byte{{0}, {255}, make([]byte, maxObjectBytes+1)} {
		budget := maxGraphBytes
		if publicationInspect(context.Background(), raw, 0, &budget) == nil {
			t.Error("uninspectable input allowed")
		}
	}
	for _, path := range []string{".env", "a/.env.local", ".aegis/session/canary", ".hermes/approvals/data", ".gnupg/key", "secrets/canary", "a/../b", "a\\b", "a/private.key"} {
		if publicationPath(path) {
			t.Error("private path accepted")
		}
	}
}

type publicationHTTPCount struct {
	next  http.RoundTripper
	calls atomic.Int64
}

func (c *publicationHTTPCount) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.next.RoundTrip(r)
}

type publicationTestCustody struct{ calls int }

func (c *publicationTestCustody) Use(_ context.Context, _ Intent, f func([]byte) error) error {
	c.calls++
	return f([]byte("synthetic-loopback"))
}

// Every negative is paired with an actual successful operation on the same
// well-formed baseline. No fixture privacy bypass is installed as the positive.
func TestPublicationPrivacyBeforeCredentialAndHTTP(t *testing.T) {
	mutations := map[string]func(*Plan, *Intent){
		"cancel":  func(p *Plan, i *Intent) {},
		"seal":    func(p *Plan, i *Intent) { p.Intake.Digest = digest("wrong") },
		"base":    func(p *Plan, i *Intent) { p.Intake.Base = p.Base; publicationReseal(t, p) },
		"repo":    func(p *Plan, i *Intent) { p.Repository = "other/repo" },
		"repo-id": func(p *Plan, i *Intent) { p.RepositoryID++ },
		"source":  func(p *Plan, i *Intent) { p.Intake.Source.URL.URL = "https://elsewhere.invalid/owner/repo/issues/1" },
		"run":     func(p *Plan, i *Intent) { p.Run = "other" },
		"oid":     func(p *Plan, i *Intent) { p.Objects[0].Raw = []byte("drift") },
		"path": func(p *Plan, i *Intent) {
			publicationRegraph(t, p, []string{".hermes"}, [][]byte{[]byte("private canary")})
		},
		"intent": func(p *Plan, i *Intent) { i.Payload = "malformed" },
		"base-blob": func(p *Plan, i *Intent) {
			publicationRegraph(t, p, []string{"base", "source"}, [][]byte{[]byte("ghp_" + strings.Repeat("A", 36)), []byte("safe")})
			p.Commit.Files = p.Commit.Files[1:]
		},
		"scoped-delta": func(p *Plan, i *Intent) { p.Commit.Files[0].Bytes = []byte("drift") },
		"credential": func(p *Plan, i *Intent) {
			publicationRegraph(t, p, []string{"source"}, [][]byte{[]byte("api_key = 'fixture-value'")})
		},
		"PR-text": func(p *Plan, i *Intent) {
			p.PRBody = base64.StdEncoding.EncodeToString([]byte("ghp_" + strings.Repeat("A", 36)))
		},
		"policy":  func(p *Plan, i *Intent) { p.Delivery = "no-publish" },
		"closing": func(p *Plan, i *Intent) { p.PRBody = "Fixes #1" },
	}
	for name, change := range mutations {
		t.Run(name, func(t *testing.T) {
			p := publicationPlan(t)
			state := &objectServer{}
			g := serveObjects(t, p, state)
			c := &publicationTestCustody{}
			httpCount := &publicationHTTPCount{next: g.client.Transport}
			g.client.Transport = httpCount
			g.privacy = PublicationPrivacy{}
			g.custody = c
			i, e := intentFor(p, Publish)
			if e != nil {
				t.Fatal(e)
			}
			if e = (PublicationPrivacy{}).Review(context.Background(), i, p); e != nil {
				t.Fatal("baseline privacy", e)
			}
			engine := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
			result, e := engine.Execute(context.Background(), p.Reference, Publish)
			if e != nil || result.Outcome != Confirmed || c.calls == 0 || !state.ref || len(state.writes) != len(p.Objects) {
				t.Fatalf("productive baseline failed: %s %v calls=%d ref=%v writes=%d", result.Outcome, e, c.calls, state.ref, len(state.writes))
			}
			for _, o := range p.Objects {
				if state.writes[o.OID] != 1 {
					t.Fatal("expected exact Git object API once")
				}
			}
			before := c.calls
			beforeHTTP := httpCount.calls.Load()
			if beforeHTTP == 0 {
				t.Fatal("positive made no HTTP calls")
			}
			change(&p, &i)
			if name != "intent" {
				if next, e := intentFor(p, Publish); e == nil {
					i = next
				}
			}
			ctx := context.Background()
			if name == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if (PublicationPrivacy{}).Review(ctx, i, p) == nil {
				t.Fatal("negative privacy allowed")
			}
			observed := g.Apply(ctx, i, p, func(context.Context) error { return nil })
			if observed.Outcome != Denied || c.calls != before || httpCount.calls.Load() != beforeHTTP {
				t.Fatal("negative reached custody/HTTP", observed.Outcome)
			}
		})
	}
	p := publicationPlan(t)
	i, _ := intentFor(p, Publish)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is((PublicationPrivacy{}).Review(ctx, i, p), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
