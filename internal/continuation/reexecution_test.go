package continuation

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

func TestReexecutionSelectionWireCompatibility(t *testing.T) {
	r := Reexecution{PreviousQueue: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "queue", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "disposition", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	if r.PreviousQueue.Validate() != nil || r.PreviousDisposition.Validate() != nil {
		t.Fatal("invalid fixture")
	}
	// Freeze the original anonymous run-selection preimage and omitempty behavior.
	type originalRun struct {
		Reexecution *struct {
			PreviousQueue       reference.DigestRef `json:"previous_queue"`
			PreviousDisposition reference.DigestRef `json:"previous_disposition"`
		} `json:"reexecution,omitempty"`
		Agent          reference.RevisionRef `json:"agent"`
		Loop           reference.RevisionRef `json:"loop"`
		IdempotencyKey string                `json:"idempotency_key"`
		QueueItemID    string                `json:"queue_item_id,omitempty"`
		Activate       bool                  `json:"activate,omitempty"`
		Inputs         []json.RawMessage     `json:"inputs,omitempty"`
	}
	for _, present := range []bool{false, true} {
		current := Run{IdempotencyKey: "stable"}
		old := originalRun{IdempotencyKey: "stable"}
		if present {
			current.Reexecution = &r
			old.Reexecution = &struct {
				PreviousQueue       reference.DigestRef `json:"previous_queue"`
				PreviousDisposition reference.DigestRef `json:"previous_disposition"`
			}{r.PreviousQueue, r.PreviousDisposition}
		}
		a, _ := json.Marshal(current)
		b, _ := json.Marshal(old)
		if string(a) != string(b) {
			t.Fatalf("canonical wire drift: %s != %s", a, b)
		}
		var decoded Run
		if json.Unmarshal(a, &decoded) != nil {
			t.Fatal("decode failed")
		}
		if present && (decoded.Reexecution == nil || *decoded.Reexecution != r) {
			t.Fatal("exact digest reference drift")
		}
	}
}
