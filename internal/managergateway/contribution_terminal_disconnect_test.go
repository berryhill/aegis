package managergateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/slash"
)

func TestContributionTerminalNotificationAfterOwnedDisconnect(t *testing.T) {
	ctx := context.Background()
	a, repository, subject, ref, reader := contributionFixture(t)
	a.ContributionReader = reader
	contract, err := a.AdmitContributionAs(ctx, subject, "metadata-session", ref, reader.source.URL.URL, "metadata-disconnect", "local-only")
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	entry := session{id: "metadata-session", token: sha256.Sum256([]byte("metadata-fixture")), subject: subject, issuedAt: a.Now(), expires: subject.ExpiresAt, mode: "degraded"}
	gateway.sessions[entry.id] = entry
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	result := slash.Result{}
	if _, err := gateway.contributionTerminal(cancelled, entry, contract, &result); !errors.Is(err, context.Canceled) {
		t.Fatal("ordinary cancelled read unexpectedly detached", err)
	}
	if _, err := gateway.contributionTerminalAfterOwnedRun(cancelled, entry, contract, app.ContributionExecutionResult{}, &result); !errors.Is(err, context.Canceled) {
		t.Fatal("unaccepted execution acquired detached read", err)
	}
	// This unit exercises metadata notification only, not candidate acceptance.
	// The real native disconnect journey independently supplies this state.
	accepted := app.ContributionExecutionResult{Candidate: &orchestration.ContributionRecord{State: "candidate_accepted"}}
	if found, err := gateway.contributionTerminalAfterOwnedRun(cancelled, entry, contract, accepted, &result); err != nil || found {
		t.Fatal("legacy metadata notification should find no final result", found, err)
	}
	lateFailure := errors.New("controlled late journey failure")
	if found, err := gateway.contributionTerminalAfterJourney(cancelled, entry, contract, accepted, lateFailure, &result); found || !errors.Is(err, lateFailure) {
		t.Fatal("no canonical result suppressed journey failure", found, err)
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer deadlineCancel()
	if _, err := gateway.contributionTerminalAfterOwnedRun(deadlineCtx, entry, contract, accepted, &result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline-exceeded request acquired detached read", err)
	}
	oldContract := contract
	oldContract.Deadline = time.Now().Add(-time.Second)
	if found, err := gateway.contributionTerminalAfterOwnedRun(cancelled, entry, oldContract, accepted, &result); found || err == nil {
		t.Fatal("notification extended original contract deadline", found, err)
	}
	oldSubject := entry
	oldSubject.subject.ExpiresAt = time.Now().Add(-time.Second)
	gateway.sessions[entry.id] = oldSubject
	if found, err := gateway.contributionTerminalAfterOwnedRun(cancelled, oldSubject, contract, accepted, &result); found || err == nil {
		t.Fatal("notification extended original subject deadline", found, err)
	}
	gateway.sessions[entry.id] = entry
	foreign := entry
	foreign.subject.PrincipalID = "foreign"
	if found, err := gateway.contributionTerminalAfterOwnedRun(cancelled, foreign, contract, accepted, &result); found || err == nil {
		t.Fatal("foreign notification accepted", found, err)
	}
	delete(gateway.sessions, entry.id)
	if found, err := gateway.contributionTerminalAfterOwnedRun(cancelled, entry, contract, accepted, &result); found || err == nil {
		t.Fatal("revoked manager notification accepted", found, err)
	}
	gateway.sessions[entry.id] = entry
	_, view, err := a.ReadContributionAs(ctx, subject, contract.Key)
	if err != nil || len(view.Attempts) != 0 || len(view.Claims) != 0 {
		t.Fatal("metadata notification dispatched execution", err)
	}
	items, err := repository.ListQueueItems(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal("metadata read changed queue", err)
	}
	expired := entry
	expired.expires = a.Now().Add(-time.Second)
	gateway.sessions[entry.id] = expired
	if _, err := gateway.contributionTerminalAfterOwnedRun(cancelled, expired, contract, accepted, &result); !errors.Is(err, app.ErrExpired) {
		t.Fatal("notification renewed original manager expiry", err)
	}
}
