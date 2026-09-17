package loop

import (
	"bytes"
	"testing"
)

func TestImplementationRevisionImmutableBinding(t *testing.T) {
	c := ImplementationDraft("implement addition", "native tests pass")
	c.Policy.RequiredTests = []RequiredGoTest{{Package: "synthetic", Name: "TestValue"}}
	c.Workspace = t.TempDir()
	c.WritableFiles = []string{"sum.go"}
	r, _, err := NewImplementationRevision("implementation", 1, "", c)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalRevision(wire)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := MarshalRevision(decoded)
	if !bytes.Equal(wire, again) {
		t.Fatal("readback changed bytes")
	}
	c.WritableFiles[0] = "different.go"
	again, _ = MarshalRevision(r)
	if !bytes.Equal(wire, again) {
		t.Fatal("caller mutated immutable contract")
	}
	r.SchemaVersion = RevisionSchemaVersion
	if _, err = MarshalRevision(r); err == nil {
		t.Fatal("v2 accepted implementation")
	}
	decoded.Steps[1].Implementation.Task = "tampered"
	if _, err = MarshalRevision(decoded); err == nil {
		t.Fatal("contract tamper accepted")
	}
}
func TestImplementationBuilderRequiresResolvedScope(t *testing.T) {
	if _, _, err := NewImplementationRevision("implementation", 1, "", ImplementationDraft("task", "acceptance")); err == nil {
		t.Fatal("unresolved scope accepted")
	}
}
