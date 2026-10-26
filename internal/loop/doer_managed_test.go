package loop

import (
	"encoding/json"
	"testing"
)

func managedFixture() ManagedDoerContract {
	text := "hello"
	return ManagedDoerContract{Doer: DoerContract{Task: "Write the supplied source", Workspace: "/isolated/producer", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}, SourceType: TypeString, ExpectedDigest: sha256Digest([]byte(text))}
}

func TestManagedDoerSchemaAndCodec(t *testing.T) {
	r, _, err := NewManagedDoerRevision("producer", 1, "", managedFixture())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalRevision(wire)
	if err != nil || got.Digest != r.Digest {
		t.Fatalf("roundtrip: %v", err)
	}
	if len(r.Inputs) != 1 || r.Inputs[0] != (Port{ID: "source", Type: TypeString, Required: true}) || len(r.Outputs) != 1 || r.Outputs[0] != (Port{ID: "result", Type: TypeArtifact, Required: true}) {
		t.Fatal("wrong managed ports")
	}
	for name, mutate := range map[string]func(*ManagedDoerContract){
		"missing assertion": func(c *ManagedDoerContract) { c.Doer.ExpectedText = nil },
		"trimmed assertion": func(c *ManagedDoerContract) { c.Doer.ExactBytes = false },
		"wrong digest":      func(c *ManagedDoerContract) { c.ExpectedDigest = sha256Digest([]byte("other")) },
		"missing digest":    func(c *ManagedDoerContract) { c.ExpectedDigest = "" },
		"zero passes":       func(c *ManagedDoerContract) { c.Doer.MaxAttempts = 0 },
		"three passes":      func(c *ManagedDoerContract) { c.Doer.MaxAttempts = 3 },
		"extra write":       func(c *ManagedDoerContract) { c.Doer.WritableFiles = append(c.Doer.WritableFiles, "extra.txt") },
		"wrong source":      func(c *ManagedDoerContract) { c.SourceType = TypeObject },
	} {
		t.Run(name, func(t *testing.T) {
			c := managedFixture()
			mutate(&c)
			if _, _, err := NewManagedDoerRevision("bad", 1, "", c); err == nil {
				t.Fatal("invalid managed contract accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*LoopRevision){
		"legacy schema":      func(r *LoopRevision) { r.SchemaVersion = DoerRevisionSchemaVersion },
		"optional output":    func(r *LoopRevision) { r.Outputs[0].Required = false },
		"wrong input":        func(r *LoopRevision) { r.Inputs[0].Type = TypeArtifact },
		"extra port":         func(r *LoopRevision) { r.Inputs = append(r.Inputs, Port{ID: "extra", Type: TypeString}) },
		"changed controller": func(r *LoopRevision) { r.Steps[0].Executable.Authority = "model" },
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(r)
			var c LoopRevision
			_ = json.Unmarshal(b, &c)
			mutate(&c)
			if _, _, err := NewRevision(c); err == nil {
				t.Fatal("invalid managed shape accepted")
			}
		})
	}
}

func TestManagedDoerTypedSourceBinding(t *testing.T) {
	r, _, err := NewManagedDoerRevision("producer", 1, "", managedFixture())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NormalizeManagedDoerBinding(r, []DoerInput{{PortID: "source", Type: TypeString, Value: json.RawMessage(`"event-derived"`)}})
	if err != nil || c.Text == nil || *c.Text != "event-derived" {
		t.Fatalf("binding: %+v %v", c, err)
	}
	if *r.ManagedDoer.Doer.ExpectedText != "hello" {
		t.Fatal("source mutated assertion")
	}
	for _, input := range []DoerInput{{PortID: "source", Type: TypeArtifact, Value: json.RawMessage(`"hello"`)}, {PortID: "source", Type: TypeString, Value: json.RawMessage(`null`)}, {PortID: "source", Type: TypeString, Value: json.RawMessage(`{}`)}} {
		if _, err := NormalizeManagedDoerBinding(r, []DoerInput{input}); err == nil {
			t.Fatal("wrong typed source accepted")
		}
	}
	contract := managedFixture()
	contract.SourceType = TypeArtifact
	r, _, err = NewManagedDoerRevision("consumer", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"hello"`, `null`, `{}`, `{"id":"forged","digest":"hello"}`} {
		if _, err := NormalizeManagedDoerBinding(r, []DoerInput{{PortID: "source", Type: TypeArtifact, Value: json.RawMessage(value)}}); err == nil {
			t.Fatal("wrong artifact accepted")
		}
	}
}
