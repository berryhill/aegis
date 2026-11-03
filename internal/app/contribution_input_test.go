package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/orchestration"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

type contributionCaptureAdapter struct {
	wire  string
	calls int
}

func (a *contributionCaptureAdapter) AttemptTurn(_ context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
	a.wire = r.Input
	a.calls++
	return hermesruntime.AttemptTurnResult{Output: `{"edits":[{"path":"value.go","content":"cGFja2FnZSBmaXh0dXJlCg=="}]}`}, nil
}

func TestContributionEncodedInputBoundary(t *testing.T) {
	ctx := context.Background()
	s, _, subject, ref, reader := contributionFixture(t)
	reader.source.Body = strings.Repeat("x", 36000)
	c, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "boundary", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetLoopViewAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &contributionCaptureAdapter{}
	proposer := orchestration.HermesPatchProposer{Adapter: adapter}
	request := func(c contribution.Contract) implementation.Request {
		bound, err := contribution.BindLoop(view.Revision, c)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := contribution.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return implementation.Request{Contract: bound, Contribution: wire, PassID: c.RunID + "-builder-1"}
	}
	if _, err = proposer.Propose(ctx, request(c)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing", "malformed", "foreign", "schema", "altered"} {
		req := request(c)
		switch kind {
		case "missing":
			req.Contribution = nil
		case "malformed":
			req.Contribution = []byte(`{}`)
		case "foreign":
			req.Contract.Contribution.ID = "another-run"
		case "schema":
			req.Contract.Contribution.SchemaVersion = "unknown"
		case "altered":
			other := c
			other.Source.Body += "changed"
			req.Contribution, _ = json.Marshal(other)
		}
		before := adapter.calls
		if _, err = proposer.Propose(ctx, req); err == nil || adapter.calls != before {
			t.Fatal("invalid context reached runtime", kind, err)
		}
	}
	// ASCII padding changes UTF-8/JSON length exactly; do not count runes.
	padding := 65536 - len(adapter.wire)
	c.Source.Body += strings.Repeat("x", padding)
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proposer.Propose(ctx, request(c)); err != nil || len(adapter.wire) != 65536 {
		t.Fatal("exact 65536 bytes rejected", len(adapter.wire), err)
	}
	calls := adapter.calls
	c.Source.Body += "x"
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proposer.Propose(ctx, request(c)); !errors.Is(err, contribution.ErrInputBound) || adapter.calls != calls {
		t.Fatal("65537 bytes reached runtime", err)
	}
	// JSON escaping, not raw text length, determines the final bound.
	c.Source.Body = strings.Repeat("<", 12000)
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = proposer.Propose(ctx, request(c)); !errors.Is(err, contribution.ErrInputBound) || adapter.calls != calls {
		t.Fatal("escaped oversized input reached runtime", err)
	}
	// Preserve the exact old Task binding at 32768 bytes; switch representation
	// only for the first byte that historically failed. Never widen Task.
	c.Source.Body = ""
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	small, err := contribution.BindLoop(view.Revision, c)
	if err != nil {
		t.Fatal(err)
	}
	c.Source.Body = strings.Repeat("x", 32768-len(small.Task))
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	edge, err := contribution.BindLoop(view.Revision, c)
	if err != nil || len(edge.Task) != 32768 || edge.Contribution != nil {
		t.Fatal("legacy byte boundary changed", err)
	}
	c.Source.Body += "x"
	c, err = contribution.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	edge, err = contribution.BindLoop(view.Revision, c)
	if err != nil || edge.Task != c.Outcome || edge.Contribution == nil {
		t.Fatal("first oversized byte not reference-bound", err)
	}
}

func TestContributionLongSourceRetained(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	reader.source.Body = strings.Repeat("é", 18000)
	c, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "long-source", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repo.GetContributionForQueue(ctx, c.QueueItemID)
	if err != nil || retained.Digest != c.Digest || retained.Source.Body != reader.source.Body {
		t.Fatal("lost exact source", err)
	}
	view, err := s.GetLoopViewAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := contribution.BindLoop(view.Revision, retained)
	if err != nil || len(bound.Task) > 32768 || bound.Contribution == nil {
		t.Fatal("bound implementation unavailable", err)
	}
	if contribution.ValidateImplementationContext(bound, &retained) != nil || contribution.ValidateImplementationContext(bound, nil) == nil {
		t.Fatal("missing resolved context accepted")
	}
	altered := retained
	altered.Source.Body += "altered"
	if contribution.ValidateImplementationContext(bound, &altered) == nil {
		t.Fatal("altered bytes accepted")
	}
	changed := *bound.Contribution
	changed.Digest = contribution.Digest([]byte("foreign"))
	bound.Contribution = &changed
	if contribution.ValidateImplementationContext(bound, &retained) == nil {
		t.Fatal("foreign reference accepted")
	}
	if _, err = s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, c.Key, "changed", reader); !errors.Is(err, fleet.ErrConflict) {
		t.Fatal("changed payload did not conflict", err)
	}
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	configureLocalImportFleet(t, s, reopened)
	replay, err := s.admitContributionAs(ctx, subject, "new-session", ref, reader.source.URL.URL, c.Key, "local-only", reader)
	if err != nil || !reflect.DeepEqual(replay, c) {
		t.Fatal("restart lost snapshot", err)
	}
	items, err := reopened.ListQueueItems(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal("duplicate effects", err)
	}
}

func TestContributionInlineEncodedOverflowSafeRejection(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	reader.source.Body = strings.Repeat("\\", 15800)
	_, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "escaped-inline", "local-only", reader)
	if !errors.Is(err, contribution.ErrInputBound) {
		t.Fatal("inline encoding overflow lost typed rejection", err)
	}
	items, e := repo.ListQueueItems(ctx)
	if e != nil || len(items) != 0 {
		t.Fatal("overflow allocated effects", e)
	}
}

func TestContributionOversizeRejectedBeforeReservation(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	reader.source.Body = strings.Repeat("x", 65536)
	for i := 0; i < 2; i++ {
		_, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "same-key", "local-only", reader)
		if !errors.Is(err, contribution.ErrInputBound) {
			t.Fatal("missing precise pre-admission bound", err)
		}
		items, err := repo.ListQueueItems(ctx)
		if err != nil || len(items) != 0 {
			t.Fatal("rejection leaked Queue", err)
		}
		if _, err = repo.GetContribution(ctx, subject.PrincipalID, "same-key"); !errors.Is(err, fleet.ErrNotFound) {
			t.Fatal("rejection reserved contribution", err)
		}
	}
	reader.source.Body = strings.Repeat("x", 36000)
	c, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "same-key", "local-only", reader)
	if err != nil {
		t.Fatal("rejection leaked writer reservation", err)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), reader.source.Body) {
		t.Fatal("truncated source")
	}
}
