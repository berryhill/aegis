package app

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionWholeCancelJoinsExactChild(t *testing.T) {
	s, _, sub, ref, reader := contributionFixtureAt(t, time.Now().UTC())
	c, err := s.admitContributionAs(context.Background(), sub, "original", ref, reader.source.URL.URL, "whole-stop", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	transport, abort := context.WithCancel(context.Background())
	run, finish, err := s.beginContributionRun(transport, c)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(run, "sleep", "60")
	if err = child.Start(); err != nil {
		finish()
		t.Fatal(err)
	}
	abort()
	if run.Err() != nil {
		t.Fatal("HTTP abort cancelled owned run")
	}
	waited := make(chan struct{})
	release := make(chan struct{})
	go func() { _ = child.Wait(); close(waited); <-release; finish() }()
	stopped := make(chan error, 1)
	go func() { stopped <- s.StopContributionInSessionAs(context.Background(), sub, "fresh-session", c.Key) }()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("exact child did not stop")
	}
	_, state, _, err := s.ObserveContributionContinuityAs(context.Background(), sub, c.Key)
	if err != nil || !state.CancelRequested || state.CancelAcknowledged {
		t.Fatalf("premature acknowledgement %+v %v", state, err)
	}
	_, view, err := s.ReadContributionAs(context.Background(), sub, c.Key)
	if err != nil || view.Projection.State == queue.StateCancelled {
		t.Fatal("Queue cancelled before owner done", err)
	}
	if _, _, err = s.beginContributionRun(context.Background(), c); err == nil {
		t.Fatal("fence admitted another writer")
	}
	close(release)
	if err = <-stopped; err != nil {
		t.Fatal(err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(context.Background(), sub, c.Key)
	if err != nil || !state.CancelAcknowledged {
		t.Fatalf("missing joined ack %+v %v", state, err)
	}
	before := state.Sequence
	if err = s.StopContributionInSessionAs(context.Background(), sub, "fresh-session", c.Key); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(context.Background(), sub, c.Key)
	if err != nil || state.Sequence != before {
		t.Fatal("repeat appended acknowledgement", err)
	}
}

func TestContributionWholeCancelUnknownPreparationHeld(t *testing.T) {
	s, _, sub, ref, reader := contributionFixtureAt(t, time.Now().UTC())
	ctx := context.Background()
	c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "unknown-stop", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "checkpoint", Stage: "prepare", Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.Key, Digest: c.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StopContributionInSessionAs(ctx, sub, "fresh-session", c.Key); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	if err != nil || !state.CancelRequested || state.CancelAcknowledged {
		t.Fatalf("unknown owner falsely acknowledged %+v %v", state, err)
	}
	if _, err = s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "cancel-acknowledged"}); err == nil {
		t.Fatal("caller minted global ack")
	}
}
