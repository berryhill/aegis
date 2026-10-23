package app

import (
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestReusableApprovalContinuationRequiresExactCanonicalLineage(t *testing.T) {
	for _, name := range []string{"valid", "missing", "wrong-owner", "wrong-agent", "wrong-loop", "wrong-parent", "budget-reset", "zero-consumed", "tampered-fact", "v4-record"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			digest := "sha256:" + strings.Repeat("a", 64)
			agent := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: digest}
			candidate := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop", Revision: 1, Digest: digest}
			record := hostapproval.Record{PrincipalID: "owner", Agent: agent, Candidate: candidate, RunRequestID: "previous", Contract: loop.DoerContract{MaxAttempts: 2}, DoerReusable: &loop.DoerReusableContract{MaxAttempts: 2}}
			fact := LoopReexecutionFact{RequestID: "next", SubjectID: "subject", PrincipalID: "owner", Agent: agent, Loop: candidate, Consumed: 1, Remaining: 1, Previous: LoopReexecution{PreviousQueue: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "previous-queue", Digest: digest}, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "previous-disposition", Digest: digest}}}
			switch name {
			case "wrong-owner":
				fact.PrincipalID = "foreign"
			case "wrong-agent":
				fact.Agent.ID = "foreign"
			case "wrong-loop":
				fact.Loop.ID = "foreign"
			case "wrong-parent":
				fact.Previous.PreviousQueue.ID = "other-queue"
			case "budget-reset":
				fact.Remaining = 2
			case "zero-consumed":
				fact.Consumed = 0
			case "v4-record":
				record.DoerReusable = nil
			}
			fact.Digest = reexecutionDigest(fact)
			if name == "tampered-fact" {
				fact.Remaining = 2
			}
			if name != "missing" {
				if err := s.Store.Create("queue-loop-reexecution", "next", fact); err != nil {
					t.Fatal(err)
				}
			}
			got := s.reusableApprovalCoversRun(record, "next", candidate, agent)
			if got != (name == "valid") {
				t.Fatalf("scope covered=%v", got)
			}
		})
	}
}
