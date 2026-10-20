package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

func TestDoerWorkerExactExecutionIdentity(t *testing.T) {
	ref := reference.RevisionRef{ID: "loop", Revision: 1, Digest: "exact"}
	for _, change := range []string{"none", "key", "queue", "submission", "graph", "snapshot", "empty"} {
		t.Run(change, func(t *testing.T) {
			s := doerExecutionScope{requestID: "run", candidate: ref, queueItemID: "run-queue", submissionID: "run-submission", graphID: "run", snapshotID: "run-snapshot"}
			switch change {
			case "key":
				s.requestID = "other"
			case "queue":
				s.queueItemID = "other-queue"
			case "submission":
				s.submissionID = "other-submission"
			case "graph":
				s.graphID = "other"
			case "snapshot":
				s.snapshotID = "other-snapshot"
			case "empty":
				s.requestID = ""
			}
			id, got, present := DoerExecutionScope(context.WithValue(context.Background(), doerExecutionScopeKey{}, s))
			if !present {
				t.Fatal("worker marker disappeared")
			}
			if change == "none" {
				if id != "run" || got != ref {
					t.Fatal("exact context lost")
				}
			} else if id != "" {
				t.Fatal("foreign durable identity reused a Run scope")
			}
		})
	}
}
