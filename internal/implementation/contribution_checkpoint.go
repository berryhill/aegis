package implementation

import (
	"context"
	"fmt"
)

// ContributionCheckpoint binds pre-effect lineage to the eventual frozen manifest.
// Pass is stable across authority/session changes; Candidate is populated only
// after the actual executable and complete source inventory exist.
type ContributionCheckpoint struct {
	RunID     string
	AttemptID string
	Pass      uint8
	Stage     string
	Candidate string
}

func (e *ManagedContribution) checkpoint(ctx context.Context, stage string, r ContributionRecord, candidate string) error {
	if e.Checkpoint == nil {
		return nil
	}
	pass := r.Consumed
	if stage == "build" {
		pass++
	}
	return e.Checkpoint(ctx, ContributionCheckpoint{RunID: r.RunID, AttemptID: r.QueueAttempt, Pass: pass, Stage: stage, Candidate: candidate})
}

func (c ContributionCheckpoint) Lineage() string { return fmt.Sprintf("%s-pass-%d", c.RunID, c.Pass) }
