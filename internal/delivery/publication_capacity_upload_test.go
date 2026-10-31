package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

func TestPublicationCapacityBoundaryActualUpload(t *testing.T) {
	p := capacityPlan(t, 392)
	remote := &objectServer{}
	adapter := serveObjects(t, p, remote)
	// The original three-object fixture has only one blob and routes all POSTs
	// to that blob. Extend only this test's HTTP front end to select by bytes.
	upstream, err := url.Parse(adapter.origin)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	blobs := map[string]GitObject{}
	for _, o := range p.Objects {
		if o.Kind == "blob" {
			blobs[o.OID] = o
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const endpoint = "/repos/owner/repo/git/blobs"
		if r.URL.Path != endpoint && !strings.HasPrefix(r.URL.Path, endpoint+"/") {
			proxy.ServeHTTP(w, r)
			return
		}
		remote.Lock()
		defer remote.Unlock()
		if r.Method == "POST" {
			var payload struct{ Content, Encoding string }
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Encoding != "base64" {
				t.Error("blob request")
				w.WriteHeader(400)
				return
			}
			raw, e := base64.StdEncoding.DecodeString(payload.Content)
			oid := objectOID("blob", raw)
			if e != nil || blobs[oid].OID == "" {
				t.Error("unapproved blob")
				w.WriteHeader(400)
				return
			}
			remote.writes[oid]++
			remote.present[oid] = true
			remote.order = append(remote.order, "blob")
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": oid})
			return
		}
		oid := strings.TrimPrefix(r.URL.Path, endpoint+"/")
		if !remote.present[oid] {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": oid, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(blobs[oid].Raw)})
	}))
	t.Cleanup(server.Close)
	adapter, err = newGitHub(server.URL, fixtureCustody{}, fixturePrivacy{})
	if err != nil {
		t.Fatal(err)
	}
	owner := &memoryOwner{}
	r, err := NewEngine(owner, fixtureSource{p}, &fixtureAuthority{}, adapter).Execute(context.Background(), p.Reference, Publish)
	if err != nil || r.Outcome != Confirmed {
		t.Fatalf("boundary publication: %+v %v", r, err)
	}
	if len(remote.writes) != 392 || !remote.ref || len(owner.records) != 394 {
		t.Fatalf("uploads=%d ref=%v slots=%d", len(remote.writes), remote.ref, len(owner.records))
	}
	// The remaining six operation slots still fit with no capacity increase.
	for _, op := range []Operation{Commit, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue} {
		i, err := intentFor(p, op)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = owner.Reserve(context.Background(), i); err != nil {
			t.Fatal(err)
		}
	}
	if len(owner.records) != 400 {
		t.Fatal(len(owner.records))
	}
}
