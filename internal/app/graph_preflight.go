package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// CheckGraphSubmissionShape is a non-authorizing, non-mutating preparation
// check. It deliberately does not resolve definitions or replace fresh service
// admission, typed input normalization, or durable rejection. Callers must
// strictly decode the application request before calling it.
func CheckGraphSubmissionShape(input SubmitGraphInput) error {
	for _, field := range []struct{ name, value string }{
		{"submission_id", input.SubmissionID},
		{"idempotency_key", input.IdempotencyKey},
		{"snapshot_id", input.SnapshotID},
		{"queue_item_id", input.QueueItemID},
		{"graph_run_id", input.GraphRunID},
		{"transition_id", input.TransitionID},
		{"rejection_id", input.RejectionID},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("missing required field: %s", field.name)
		}
	}
	if err := input.Graph.Validate(); err != nil {
		return fmt.Errorf("invalid graph reference: %w", err)
	}
	if input.MaxAttempts == 0 || input.MaxAttempts > queue.MaxAttempts {
		return errors.New("max_attempts is outside the supported positive bound")
	}
	if input.Workspace != nil {
		return errors.New("workspace must be omitted; Aegis derives it after authentication")
	}
	if input.WorkspaceAgentID != "" {
		if strings.TrimSpace(input.WorkspaceAgentID) != input.WorkspaceAgentID {
			return errors.New("invalid agent_id selector")
		}
		if input.Authority != (reference.DigestRef{}) {
			return errors.New("authority must be omitted with agent_id")
		}
	} else if err := input.Authority.Validate(); err != nil {
		return fmt.Errorf("invalid authority reference (or supply agent_id): %w", err)
	}
	return nil
}
