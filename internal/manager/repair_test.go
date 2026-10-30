package manager

import (
	"encoding/json"
	"testing"
)

func TestRepairLegacyAuditNotAdvertised(t *testing.T) {
	if _, err := decodeArguments(Proposal{Operation: AuditQuery, Arguments: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("legacy audit accepted")
	}
	for _, schema := range managerProposalSchemas() {
		b, _ := json.Marshal(schema)
		var out any
		_ = json.Unmarshal(b, &out)
		if string(b) == "audit.query" {
			t.Fatal("advertised")
		}
	}
}
func TestRepairPlatformConformanceRejectsStringBudget(t *testing.T) {
	for _, c := range ConformanceCorpus() {
		if c.ID != "platform-integer-budget" {
			continue
		}
		r := Response{Kind: "proposal", Proposal: &Proposal{Operation: PlatformCall, Arguments: json.RawMessage(`{"operation":"graphs.submit","input":{"agent_id":"agent-example","max_attempts":"2"}}`)}}
		if ok, _ := evaluateConformance(c, r); ok {
			t.Fatal("string budget certified")
		}
		r.Proposal.Arguments = json.RawMessage(`{"operation":"graphs.submit","input":{"agent_id":"agent-example","max_attempts":2}}`)
		if ok, why := evaluateConformance(c, r); !ok {
			t.Fatal(why)
		}
		return
	}
	t.Fatal("missing corpus case")
}
