package doerbinding

import (
	"encoding/json"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
)

func TestManagedNodeExactContractBinding(t *testing.T) {
	text := "hello"
	// Known SHA-256 of hello pins expected bytes independently of run source.
	c := loop.ManagedDoerContract{Doer: loop.DoerContract{Task: "Write source", Workspace: "/isolated/producer", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}, SourceType: loop.TypeString, ExpectedDigest: "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"}
	r, _, err := loop.NewManagedDoerRevision("producer", 1, "", c)
	if err != nil {
		t.Fatal(err)
	}
	n := graph.Node{ID: "producer", Loop: loopReference(r), Inputs: []graph.Port{{ID: "source", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeArtifact, Required: true}}}
	inputs := []graph.NormalizedInput{{PortID: "source", Type: graph.TypeString, Value: json.RawMessage(`"event-data"`)}}
	bound, source, err := BindManagedDoerNode(r, n, inputs)
	if err != nil || source.Text == nil || *source.Text != "event-data" || *bound.ExpectedText != "hello" {
		t.Fatalf("binding: %+v %+v %v", bound, source, err)
	}
	bound.WritableFiles[0] = "substituted"
	*bound.ExpectedText = "substituted"
	if r.ManagedDoer.Doer.WritableFiles[0] != "result.txt" || *r.ManagedDoer.Doer.ExpectedText != "hello" {
		t.Fatal("binding aliases immutable definition")
	}
	n.Outputs[0].Required = false
	if _, _, err := BindManagedDoerNode(r, n, inputs); err == nil {
		t.Fatal("weakened node output accepted")
	}
}
