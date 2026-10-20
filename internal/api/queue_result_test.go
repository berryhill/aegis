package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
)

// Exact read-boundary substitutions in isolated durable fixtures. No runtime or
// canonical completion facts are changed to manufacture a successful run.
type resultReadRepository struct {
	fleet.Repository
	artifact    func(*evidence.RuntimeArtifact)
	receipt     func(*evidence.VerificationReceipt)
	disposition func(*disposition.Record)
	owner       bool
	wrongSource bool
	state       queue.State
}

func (r resultReadRepository) ListLoopExecutions(ctx context.Context) ([]execution.LoopExecution, error) {
	values, err := r.Repository.ListLoopExecutions(ctx)
	if r.wrongSource {
		values = append([]execution.LoopExecution(nil), values...)
		for i := range values {
			values[i].Loop.Digest = "sha256:" + strings.Repeat("0", 64)
		}
	}
	return values, err
}

func (r resultReadRepository) GetRuntimeArtifact(ctx context.Context, id string) (evidence.RuntimeArtifact, error) {
	v, e := r.Repository.GetRuntimeArtifact(ctx, id)
	if e == nil && r.artifact != nil {
		r.artifact(&v)
	}
	return v, e
}
func (r resultReadRepository) GetVerificationReceipt(ctx context.Context, id string) (evidence.VerificationReceipt, error) {
	v, e := r.Repository.GetVerificationReceipt(ctx, id)
	if e == nil && r.receipt != nil {
		r.receipt(&v)
	}
	return v, e
}
func (r resultReadRepository) GetDispositionByGraphRun(ctx context.Context, id string) (disposition.Record, error) {
	v, e := r.Repository.GetDispositionByGraphRun(ctx, id)
	if e == nil && r.disposition != nil {
		r.disposition(&v)
	}
	return v, e
}
func (r resultReadRepository) GetAgentRevision(ctx context.Context, id string, rev uint64) (registry.AgentRevision, error) {
	v, e := r.Repository.GetAgentRevision(ctx, id, rev)
	if r.owner {
		v.Ownership.OwnerID = "foreign-owner"
	}
	return v, e
}
func (r resultReadRepository) GetQueueProjection(ctx context.Context, id string) (queue.Projection, error) {
	v, e := r.Repository.GetQueueProjection(ctx, id)
	if r.state != "" {
		v.State = r.state
	}
	return v, e
}

func testQueueResultDenials(t *testing.T, s *app.Service, subject core.Subject, id string, client *http.Client) {
	t.Helper()
	original := s.FleetRepository
	defer func() { s.FleetRepository = original }()
	for _, tc := range []struct {
		name   string
		repo   resultReadRepository
		status int
	}{
		{"wrong-source", resultReadRepository{Repository: original, wrongSource: true}, http.StatusServiceUnavailable},
		{"foreign-owner", resultReadRepository{Repository: original, owner: true}, http.StatusForbidden},
		{"wrong-run", resultReadRepository{Repository: original, artifact: func(a *evidence.RuntimeArtifact) { a.RunID = "foreign-run" }}, http.StatusServiceUnavailable},
		{"wrong-attempt", resultReadRepository{Repository: original, artifact: func(a *evidence.RuntimeArtifact) { a.AttemptID = "foreign-attempt" }}, http.StatusServiceUnavailable},
		{"wrong-authority", resultReadRepository{Repository: original, artifact: func(a *evidence.RuntimeArtifact) { a.AuthorityContextID = "foreign-authority" }}, http.StatusServiceUnavailable},
		{"wrong-receipt", resultReadRepository{Repository: original, receipt: func(r *evidence.VerificationReceipt) { r.RunID = "foreign-run" }}, http.StatusServiceUnavailable},
		{"failed-receipt", resultReadRepository{Repository: original, receipt: func(r *evidence.VerificationReceipt) { r.Outcome = evidence.Failed }}, http.StatusServiceUnavailable},
		{"tampered-proof-reference", resultReadRepository{Repository: original, artifact: func(a *evidence.RuntimeArtifact) { a.ContentRef = "sha256:bad" }}, http.StatusServiceUnavailable},
		{"wrong-disposition-run", resultReadRepository{Repository: original, disposition: func(d *disposition.Record) { d.GraphRunID = "foreign-run" }}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.FleetRepository = tc.repo
			var body map[string]any
			// Keep the production read limiter enabled while covering denials.
			time.Sleep(250 * time.Millisecond)
			apiRequest(t, client, http.MethodGet, "/v1/queue/"+id+"/result", nil, &body, tc.status)
			if body["result"] != nil {
				t.Fatal("denial disclosed verified content")
			}
		})
	}
	s.FleetRepository = original
	for _, state := range []queue.State{queue.StateQueued, queue.StateFailed} {
		s.FleetRepository = resultReadRepository{Repository: original, state: state, disposition: func(d *disposition.Record) { d.State = execution.StateFailed }}
		var value app.QueueResult
		time.Sleep(250 * time.Millisecond)
		apiRequest(t, client, http.MethodGet, "/v1/queue/"+id+"/result", nil, &value, http.StatusOK)
		if value.Result != nil || value.Verification == "verified" {
			t.Fatalf("%s returned verified content", state)
		}
	}
	s.FleetRepository = original
	if _, err := s.GetQueueResultAs(context.Background(), core.Subject{}, id); err == nil {
		t.Fatal("unauthenticated result read")
	}
	for _, bad := range []string{"", "missing-queue", "../blob"} {
		if _, err := s.GetQueueResultAs(context.Background(), subject, bad); err == nil {
			t.Fatal("invalid identity accepted", bad)
		}
	}
	view, err := s.GetQueueItemAs(context.Background(), subject, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"contract-digest", "file-digest", "malformed-proof", "receipt-evidence"} {
		t.Run(name, func(t *testing.T) {
			a, r := *view.Artifact, view.Receipts[0]
			wire, err := s.Store.GetBlob(a.ContentRef)
			if err != nil {
				t.Fatal(err)
			}
			var proof map[string]string
			if err := json.Unmarshal(wire, &proof); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "contract-digest":
				proof["contract_digest"] = proof["file_digest"]
			case "file-digest":
				proof["file_digest"] = a.Digest
			case "malformed-proof":
				proof["unknown"] = "not canonical"
			case "receipt-evidence":
				r.EvidenceRef = "sha256:bad"
			}
			if name != "receipt-evidence" {
				wire, _ = json.Marshal(proof)
				a.ContentRef, err = s.Store.PutBlob(wire)
				if err != nil {
					t.Fatal(err)
				}
				a.Digest = a.ContentRef
				r.ExpectedDigest = a.Digest
				r.ObservedDigest = a.Digest
				r.EvidenceRef = ""
				wire, _ = json.Marshal(r)
				r.EvidenceRef, err = s.Store.PutBlob(wire)
				if err != nil {
					t.Fatal(err)
				}
			}
			s.FleetRepository = resultReadRepository{Repository: original, artifact: func(v *evidence.RuntimeArtifact) { *v = a }, receipt: func(v *evidence.VerificationReceipt) { *v = r }}
			var body map[string]any
			time.Sleep(250 * time.Millisecond)
			apiRequest(t, client, http.MethodGet, "/v1/queue/"+id+"/result", nil, &body, http.StatusServiceUnavailable)
			if body["result"] != nil {
				t.Fatal("corrupt proof disclosed result")
			}
			s.FleetRepository = original
		})
	}
}
