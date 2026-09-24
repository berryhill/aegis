package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/implementation"
	"testing"
)

func TestPatchProposalStrictDataOnly(t *testing.T) {
	for _, wire := range []string{`{"edits":[],"command":"go test"}`, `{"edits":[],"edits":[]}`, `{"edits":[{"path":"sum.go","content":"eA==","passed":true}]}`, `{"edits":[{"path":"sum.go","path":"other.go","content":"eA=="}]}`, `{"edits":[]} {}`} {
		if _, err := decodePatchProposal([]byte(wire)); err == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	edits, err := decodePatchProposal([]byte(`{"edits":[{"path":"sum.go","content":"eA=="}]}`))
	if err != nil || len(edits) != 1 || string(edits[0].Content) != "x" {
		t.Fatal(edits, err)
	}
	if _, err := (HermesPatchProposer{}).Propose(context.Background(), implementation.Request{}); err == nil {
		t.Fatal("missing adapter accepted")
	}
}

func TestReportedPatchStrictSchema(t *testing.T) {
	for _, wire := range []string{
		`{"edits":[{"path":"sum.go","content":"eA=="}]}`,
		`{"edits":[{"path":"sum.go","content":"eA=="}],"report":""}`,
		`{"edits":[{"path":"sum.go","content":"eA=="}],"report":"done","report":"other"}`,
		`{"edits":[{"path":"sum.go","content":"eA=="}],"report":"done","passed":true}`,
	} {
		if _, err := decodeReportedPatch([]byte(wire)); err == nil {
			t.Fatalf("accepted %s", wire)
		}
	}
	p, err := decodeReportedPatch([]byte(`{"edits":[{"path":"sum.go","content":"eA=="}],"report":"tested"}`))
	if err != nil || p.Report != "tested" || len(p.Edits) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
}
