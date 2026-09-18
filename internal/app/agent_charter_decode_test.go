package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExactCharterApprovalRejectsDuplicateKeys(t *testing.T) {
	for _, wire := range []string{`{"expected":{},"expected":{},"charter":{}}`, `{"expected":{"id":"a","id":"b"},"charter":{}}`, `{"expected":{},"charter":{},"unknown":true}`} {
		var input ApproveAgentCharterInput
		if err := json.Unmarshal([]byte(wire), &input); err == nil {
			t.Fatalf("accepted ambiguous approval: %s", wire)
		}
	}
	var input ApproveAgentCharterInput
	ref := `{"schema_version":"aegis.reference.revision.v1","id":"agent","revision":1,"digest":"sha256:` + strings.Repeat("a", 64) + `"}`
	if err := json.Unmarshal([]byte(`{"expected":`+ref+`,"charter":`+ref+`}`), &input); err != nil {
		t.Fatal(err)
	}
}
