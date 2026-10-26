package app

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionWholeCancelMissingOwnerAndTimeout(t *testing.T) {
	for _, mode := range []string{"missing-owner", "wrong-owner", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _, sub, ref, reader := contributionFixtureAt(t, time.Now().UTC())
			c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "pending-"+mode, "local-only", reader)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "checkpoint", Stage: "prepare", Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.Key, Digest: c.Digest}})
			if err != nil {
				t.Fatal(err)
			}
			if mode != "missing-owner" {
				run, finish, e := s.beginContributionRun(ctx, c)
				if e != nil {
					t.Fatal(e)
				}
				child := exec.CommandContext(run, "sleep", "60")
				if e = child.Start(); e != nil {
					finish()
					t.Fatal(e)
				}
				// A reaped child alone is not the invocation done/cleanup proof.
				if mode == "wrong-owner" {
					s.loopRunMu.Lock()
					s.contributionRuns[c.QueueItemID].contract = "wrong-contract"
					s.loopRunMu.Unlock()
				}
				t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait(); finish() })
			}
			if err = s.StopContributionInSessionAs(ctx, sub, "fresh-session", c.Key); err != nil {
				t.Fatal(err)
			}
			_, state, _, err = s.ObserveContributionContinuityAs(ctx, sub, c.Key)
			if err != nil || !state.CancelRequested || state.CancelAcknowledged {
				t.Fatalf("unproved stop acknowledged: %+v %v", state, err)
			}
			_, view, err := s.ReadContributionAs(ctx, sub, c.Key)
			if err != nil || view.Projection.State == queue.StateCancelled {
				t.Fatal("unproved stop terminalized", err)
			}
		})
	}
}
