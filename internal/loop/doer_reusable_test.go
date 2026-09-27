package loop

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReusableDoerDefinitionAndTypedBinding(t *testing.T) {
	r, result, err := NewDoerReusableRevision("reusable", 1, "", DoerReusableContract{MaxAttempts: 2})
	if err != nil || result.Outcome != ValidationValid {
		t.Fatalf("v5 definition: %v %+v", err, result.Issues)
	}
	wire, err := MarshalRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalRevision(wire)
	if err != nil || decoded.Digest != r.Digest || decoded.Doer != nil || decoded.DoerReusable == nil || bytes.Contains(wire, []byte("/workspace")) {
		t.Fatalf("v5 roundtrip: %v", err)
	}
	inputs := []DoerInput{
		{PortID: "task", Type: TypeString, Value: json.RawMessage(`"Make file"`)},
		{PortID: "workspace", Type: TypeString, Value: json.RawMessage(`"/workspace"`)},
		{PortID: "writable_files", Type: TypeArray, Value: json.RawMessage(`["out.txt"]`)},
		{PortID: "verify_file", Type: TypeString, Value: json.RawMessage(`"out.txt"`)},
	}
	bound, err := NormalizeDoerBinding(decoded, inputs)
	if err != nil || bound.MaxAttempts != 2 || bound.ExpectedText != nil {
		t.Fatalf("presence binding: %+v %v", bound, err)
	}
	inputs = append(inputs, DoerInput{PortID: "expected_text", Type: TypeString, Value: json.RawMessage(`""`)})
	bound, err = NormalizeDoerBinding(decoded, inputs)
	if err != nil || bound.ExpectedText == nil || *bound.ExpectedText != "" {
		t.Fatalf("empty binding: %+v %v", bound, err)
	}
	for name, mutate := range map[string]func([]DoerInput) []DoerInput{
		"missing":        func(v []DoerInput) []DoerInput { return v[1:] },
		"wrong type":     func(v []DoerInput) []DoerInput { v[0].Type = TypeBoolean; return v },
		"wrong value":    func(v []DoerInput) []DoerInput { v[0].Value = json.RawMessage(`true`); return v },
		"duplicate keys": func(v []DoerInput) []DoerInput { v[2].Value = json.RawMessage(`[ {"a":1,"a":2} ]`); return v },
		"null optional":  func(v []DoerInput) []DoerInput { v[4].Value = json.RawMessage(`null`); return v },
		"escape":         func(v []DoerInput) []DoerInput { v[3].Value = json.RawMessage(`"../out.txt"`); return v },
		"duplicate":      func(v []DoerInput) []DoerInput { return append(v, v[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			copyInputs := append([]DoerInput(nil), inputs...)
			if _, err := NormalizeDoerBinding(decoded, mutate(copyInputs)); err == nil {
				t.Fatal("unsafe binding accepted")
			}
		})
	}
	broken := decoded
	broken.Inputs = nil
	if ValidateRevision(broken).Outcome != ValidationInvalid {
		t.Fatal("removed required typed ports accepted")
	}
	// Historical v4 decoding and digest remain independent of v5's dynamic inputs.
	old := doerFixture(t)
	oldWire, _ := MarshalRevision(old)
	oldDecoded, err := UnmarshalRevision(oldWire)
	if err != nil || oldDecoded.Digest != old.Digest || oldDecoded.DoerReusable != nil {
		t.Fatalf("v4 historical decode: %v", err)
	}
}
