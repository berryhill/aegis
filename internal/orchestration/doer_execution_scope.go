package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/reference"
)

type doerExecutionScopeKey struct{}
type doerExecutionScope struct {
	requestID                                              string
	candidate                                              reference.RevisionRef
	queueItemID, submissionID, graphID, snapshotID, nodeID string
}

func DoerExecutionIntentMatches(ctx context.Context, intent string) bool {
	s, ok := ctx.Value(doerExecutionScopeKey{}).(doerExecutionScope)
	return ok && intent != "" && s.nodeID == "loop-"+intent
}

// DoerExecutionScope exposes only the worker-derived immutable execution identity.
// There is deliberately no public setter or request-wire field.
func DoerExecutionScope(ctx context.Context) (string, reference.RevisionRef, bool) {
	s, present := ctx.Value(doerExecutionScopeKey{}).(doerExecutionScope)
	if !present {
		return "", reference.RevisionRef{}, false
	}
	// Compare the actual persisted identities with the exact controller emission,
	// never trust a caller marker, worker ID, or a guessed name prefix.
	if s.requestID == "" || s.queueItemID != s.requestID+"-queue" || s.submissionID != s.requestID+"-submission" || s.graphID != s.requestID || s.snapshotID != s.requestID+"-snapshot" {
		return "", reference.RevisionRef{}, true
	}
	return s.requestID, s.candidate, true
}
