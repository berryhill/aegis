package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/slash"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Constructor call at the same real private-frame boundary owned by #378.
// This fixture delegates every verification/finalization to the actual helper;
// it never accepts records or returns a synthetic completion reference.
type managerFrameCompletion struct {
	s       *app.Service
	subject core.Subject
	key     string
	policy  reference.DigestRef
	actual  *app.ContributionCompletionAdapter
}

func (f *managerFrameCompletion) get(ctx context.Context) (*app.ContributionCompletionAdapter, error) {
	if f.actual == nil {
		var err error
		f.actual, err = f.s.NewContributionCompletionInFrame(ctx, f.subject, f.key, f.policy)
		if err != nil {
			return nil, err
		}
	}
	return f.actual, nil
}
func (f *managerFrameCompletion) VerifyContributionCompletion(ctx context.Context, r delivery.Reference) (string, error) {
	a, e := f.get(ctx)
	if e != nil {
		return "", e
	}
	return a.VerifyContributionCompletion(ctx, r)
}
func (f *managerFrameCompletion) FinalizeContributionCompletion(ctx context.Context, r delivery.Reference) error {
	a, e := f.get(ctx)
	if e != nil {
		return e
	}
	return a.FinalizeContributionCompletion(ctx, r)
}
func (f *managerFrameCompletion) FinalizeContributionBoundary(ctx context.Context, r delivery.Reference) error {
	a, e := f.get(ctx)
	if e != nil {
		return e
	}
	return a.FinalizeContributionBoundary(ctx, r)
}

// A missing referenced record is corruption of an indexed result, not absence
// of completion. This wrapper removes no durable data and grants no authority.
type missingTerminalArtifact struct {
	*fleetbadger.Store
	missingReceipt bool
}

func (p missingTerminalArtifact) GetRuntimeArtifact(ctx context.Context, id string) (evidence.RuntimeArtifact, error) {
	if p.missingReceipt {
		return p.Store.GetRuntimeArtifact(ctx, id)
	}
	return evidence.RuntimeArtifact{}, fleet.ErrNotFound
}
func (p missingTerminalArtifact) GetVerificationReceipt(ctx context.Context, id string) (evidence.VerificationReceipt, error) {
	if !p.missingReceipt {
		return p.Store.GetVerificationReceipt(ctx, id)
	}
	return evidence.VerificationReceipt{}, fleet.ErrNotFound
}

func TestManagerTerminalContributionReadback(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}
func managerTerminalReadback(t *testing.T, s *app.Service, subject core.Subject, c contribution.Contract, clock *contributionStageClock) {
	t.Helper()
	ctx := context.Background()
	expected, err := s.GetContributionResultAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.ReadContributionExecutionAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	indexedRepository := s.FleetRepository
	for _, missingReceipt := range []bool{false, true} {
		s.FleetRepository = missingTerminalArtifact{Store: indexedRepository.(*fleetbadger.Store), missingReceipt: missingReceipt}
		_, missingErr := s.GetFinalContributionResultAs(ctx, subject, c.Key)
		s.FleetRepository = indexedRepository
		if missingErr == nil || app.IsContributionResultNotFound(missingErr) {
			t.Fatal("indexed terminal evidence absence permits execution fallback", missingReceipt, missingErr)
		}
	}
	gateway, err := New(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	token, id := "terminal-fixture-token", "terminal-fixture-manager"
	gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: s.Now(), expires: subject.ExpiresAt, mode: "degraded", contributionLoop: c.Loop}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	accepted := app.ContributionExecutionResult{Candidate: &before}
	for _, route := range []string{"run", "resume"} {
		entry := gateway.sessions[id]
		recovered := slash.Result{}
		found, err := gateway.contributionTerminalAfterJourney(cancelled, entry, c, accepted, errors.New("controlled late "+route+" journey error"), &recovered)
		if err != nil || !found {
			t.Fatal("canonical terminal did not reconcile late journey error", route, found, err)
		}
		wire, _ := json.Marshal(recovered.Data["result"])
		want, _ := json.Marshal(expected)
		if string(wire) != string(want) {
			t.Fatal("recovery changed canonical result", route)
		}
	}
	for label, ref := range map[string]string{"native": expected.Report.Native, "history": expected.Report.History} {
		t.Run("retained-joined-corruption-"+label, func(t *testing.T) {
			path := filepath.Join(s.Store.Root(), "blobs", "sha256", strings.TrimPrefix(ref, "sha256:"))
			original, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			defer func() {
				if e := os.WriteFile(path, original, 0600); e != nil {
					t.Fatal(e)
				}
			}()
			if e := os.WriteFile(path, []byte("controlled retained evidence corruption"), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := s.Store.GetBlob(ref); e == nil {
				t.Fatal("corruption fixture not active")
			}
			if _, e := s.GetContributionResultAs(ctx, subject, c.Key); e == nil || app.IsContributionResultNotFound(e) {
				t.Fatal("corrupt joined result permits success or replay", e)
			}
			entry := gateway.sessions[id]
			recovered := slash.Result{}
			if found, e := gateway.contributionTerminalAfterJourney(cancelled, entry, c, accepted, context.Canceled, &recovered); found || e == nil {
				t.Fatal("late journey error hid corrupt canonical evidence", found, e)
			}
			for _, command := range []string{"/contribute status " + c.Key, "/contribute resume " + c.Key, "/contribute run " + c.Source.URL.URL + " local-only"} {
				if _, e := gateway.Execute(ctx, subject, id, token, command); e == nil {
					t.Fatal("corrupt joined evidence accepted by manager", command)
				}
			}
		})
	}
	for _, command := range []string{"/contribute status " + c.Key, "/contribute resume " + c.Key, "/contribute run " + c.Source.URL.URL + " local-only"} {
		result, e := gateway.Execute(ctx, subject, id, token, command)
		if e != nil {
			t.Fatal(command, e)
		}
		if result.Data["terminal"] != true || result.Data["completed"] != true || result.Data["whole_contribution_succeeded"] != false || result.Data["outcome"] != "narrower-target-completed" {
			t.Fatalf("terminal route wrong: %+v", result)
		}
	}
	after, err := s.ReadContributionExecutionAs(ctx, subject, c.Key)
	if err != nil || before.Consumed != after.Consumed {
		t.Fatal("terminal manager replayed native stage", err)
	}
	clock.Advance(301 * time.Second)
	fresh, err := s.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	newID := "fresh-terminal-manager"
	gateway.sessions[newID] = session{id: newID, token: sha256.Sum256([]byte(token)), subject: fresh, issuedAt: s.Now(), expires: fresh.ExpiresAt, mode: "degraded"}
	for _, op := range []string{"status", "resume"} {
		result, e := gateway.Execute(ctx, fresh, newID, token, "/contribute "+op+" "+c.Key)
		if e != nil || result.Data["terminal"] != true {
			t.Fatal("fresh retained navigation depended on expired runtime", op, e)
		}
	}
	actual, err := s.GetContributionResultAs(ctx, fresh, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	if string(a) != string(b) {
		t.Fatal("retained result changed")
	}
	foreign := fresh
	foreign.PrincipalID = "foreign"
	if _, err = gateway.Execute(ctx, foreign, newID, token, "/contribute status "+c.Key); err == nil {
		t.Fatal("foreign manager read")
	}
	stale := fresh
	stale.ExpiresAt = s.Now().Add(-time.Second)
	if _, err = gateway.Execute(ctx, stale, newID, token, "/contribute status "+c.Key); err == nil {
		t.Fatal("stale caller read")
	}
	for _, ref := range []string{expected.Artifact.ContentRef, expected.Receipts[0].EvidenceRef} {
		path := filepath.Join(s.Store.Root(), "blobs", "sha256", strings.TrimPrefix(ref, "sha256:"))
		original, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, []byte("controlled corrupt evidence"), 0600); e != nil {
			t.Fatal(e)
		}
		_, readErr := gateway.Execute(ctx, fresh, newID, token, "/contribute resume "+c.Key)
		if e = os.WriteFile(path, original, 0600); e != nil {
			t.Fatal(e)
		}
		if readErr == nil {
			t.Fatal("corrupt terminal evidence fell through to execution")
		}
	}
}
