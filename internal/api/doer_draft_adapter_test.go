package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/labstack/echo/v5"
)

type draftAdapterRepository struct {
	fleet.Repository
	old, next registry.AgentRevision
}

func (r draftAdapterRepository) GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error) {
	return r.old, nil
}
func (r draftAdapterRepository) LatestAgentRevision(context.Context, string) (registry.AgentRevision, error) {
	return r.next, nil
}

func TestDoerDraftAdapterSaveReadCASAndSuccessor(t *testing.T) {
	s, subject, candidate := candidateReadinessFixture(t, "none")
	e := echo.New()
	g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", subject); return next(c) }
	})
	registerDoerServiceRoutes(g, s)
	call := func(method, path string, input any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	decodeDraft := func(rr *httptest.ResponseRecorder) app.DoerDraft {
		t.Helper()
		var d app.DoerDraft
		if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &d) != nil {
			t.Fatalf("draft response: %d %s", rr.Code, rr.Body.String())
		}
		return d
	}
	input := app.DoerDraftInput{Agent: candidate.Agent, LoopID: candidate.Candidate.LoopID, Revision: 1, Contract: *candidate.Candidate.Doer}
	original := decodeDraft(call(http.MethodPost, "/v1/loops/doer/drafts", input))
	path := "/v1/loops/doer/drafts/" + original.ID
	if got := decodeDraft(call(http.MethodGet, path, nil)); !reflect.DeepEqual(got, original) {
		t.Fatal("read differs from saved draft")
	}
	input.ID, input.ExpectedVersion = original.ID, original.Version
	input.Contract.Task = "Retained updated task"
	updated := decodeDraft(call(http.MethodPost, "/v1/loops/doer/drafts", input))
	if updated.Version != original.Version+1 || updated.PublicationKey != original.PublicationKey {
		t.Fatal("CAS lost identity")
	}
	if rr := call(http.MethodPost, "/v1/loops/doer/drafts", input); rr.Code < 400 {
		t.Fatal("stale version accepted")
	}
	if got := decodeDraft(call(http.MethodGet, path, nil)); !reflect.DeepEqual(got, updated) {
		t.Fatal("denial mutated draft")
	}
	old, err := s.FleetRepository.LatestAgentRevision(context.Background(), candidate.Agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := s.GetCharter(old.Charter.ID, old.Charter.Revision)
	if err != nil {
		t.Fatal(err)
	}
	c := charter.Charter
	c.Revision++
	canonical, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	old.Ownership.OwnerID = subject.PrincipalID
	next := old
	next.Revision++
	next.Digest = canonical.Digest
	next.Ownership.OwnerID = subject.PrincipalID
	next.Charter = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: c.Revision, Digest: canonical.Digest}
	next.CharterSuccessor = &registry.CharterSuccessor{Previous: candidate.Agent, ApprovedBy: subject.PrincipalID}
	s.FleetRepository = draftAdapterRepository{Repository: s.FleetRepository, old: old, next: next}
	continuation := app.ContinueDoerDraftInput{ID: original.ID, ExpectedVersion: updated.Version, Expected: updated.Agent, Charter: next.Charter}
	continuation.ID = "different"
	if rr := call(http.MethodPost, path+"/continue", continuation); rr.Code != 400 {
		t.Fatal("path substitution accepted")
	}
	continuation.ID = original.ID
	rebound := decodeDraft(call(http.MethodPost, path+"/continue", continuation))
	if rebound.Version != updated.Version+1 || rebound.Agent.Digest != next.Digest || rebound.PublicationKey == updated.PublicationKey || !reflect.DeepEqual(rebound.Contract, updated.Contract) {
		t.Fatal("successor failed to retain contract and renew binding")
	}
	if rr := call(http.MethodPost, path+"/continue", continuation); rr.Code < 400 {
		t.Fatal("successor replay accepted")
	}
	if got := decodeDraft(call(http.MethodGet, path, nil)); !reflect.DeepEqual(got, rebound) {
		t.Fatal("successor readback differs")
	}
	loops, err := s.ListLoopsAs(context.Background(), subject)
	if err != nil || len(loops) != 0 {
		t.Fatal("draft published a Loop")
	}
	queue, err := s.ListQueueAs(context.Background(), subject)
	if err != nil || len(queue) != 0 {
		t.Fatal("draft created queue work")
	}
	approvals, err := s.ListApprovals()
	if err != nil || len(approvals) != 0 {
		t.Fatal("draft approved authority")
	}
	receipts, err := s.ListReceipts()
	if err != nil || len(receipts) != 0 {
		t.Fatal("draft provisioned runtime")
	}
}
