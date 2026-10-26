package managergateway

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"reflect"
	"testing"
)

// Observes the REAL verifier-prepared completion. It first submits identical
// input through the generic owner, requires denial/no mutations, then delegates
// to the actual dedicated transaction. It never mints proof or reports PASS.
type continuationCompletionProbe struct {
	*fleetbadger.Store
	t             *testing.T
	genericDenied bool
}

func (p *continuationCompletionProbe) CompleteContributionQueueItem(ctx context.Context, c fleet.Completion, f fleet.AuditFact, r fleet.EvidenceReader) error {
	p.t.Helper()
	before, e := p.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
	if e != nil {
		return e
	}
	audit, e := p.AuditEvents(ctx)
	if e != nil {
		return e
	}
	if e = p.Store.CompleteQueueItem(ctx, c, f, r); !errors.Is(e, fleet.ErrConflict) {
		p.t.Fatalf("generic late success not denied: %v", e)
	}
	after, e := p.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
	if e != nil || !reflect.DeepEqual(before, after) {
		p.t.Fatal("generic denied completion changed projection")
	}
	afterAudit, e := p.AuditEvents(ctx)
	if e != nil || !reflect.DeepEqual(audit, afterAudit) {
		p.t.Fatal("generic denied completion changed audit")
	}
	if _, e = p.GetDispositionByGraphRun(ctx, c.Disposition.GraphRunID); !errors.Is(e, fleet.ErrNotFound) {
		p.t.Fatal("generic completion created disposition")
	}
	p.genericDenied = true
	return p.Store.CompleteContributionQueueItem(ctx, c, f, r)
}
