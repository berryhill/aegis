package loop

import (
	"bytes"
	"strings"
	"testing"
)

func doerFixture(t *testing.T) LoopRevision {
	t.Helper()
	expected := "hello"
	r, result, err := NewDoerRevision("doer-test", 1, "", DoerContract{Task: "Create result.txt containing hello", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &expected, MaxAttempts: 3})
	if err != nil || result.Outcome != ValidationValid {
		t.Fatalf("fixture: %v %+v", err, result.Issues)
	}
	return r
}

func TestDoerEmptyExpectedTextDiffersFromPresenceOnly(t *testing.T) {
	c := DoerContract{Task: "Create empty result", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}
	presence, _, err := NewDoerRevision("doer-test", 1, "", c)
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	c.ExpectedText = &empty
	exact, result, err := NewDoerRevision("doer-test", 1, "", c)
	if err != nil || result.Outcome != ValidationValid || exact.Digest == presence.Digest {
		t.Fatalf("empty expected text collapsed: %v %+v", err, result.Issues)
	}
	data, err := MarshalRevision(exact)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := UnmarshalRevision(data)
	if err != nil || parsed.Doer.ExpectedText == nil || *parsed.Doer.ExpectedText != "" {
		t.Fatalf("empty exact text lost: %v", err)
	}
}

func TestDoerControllerFailureAndMissingInputRoutes(t *testing.T) {
	r := doerFixture(t)
	edges := map[string]Transition{}
	for _, e := range r.Transitions {
		edges[e.ID] = e
	}
	if r.Steps[0].ID == "" {
		t.Fatal("empty steps")
	}
	kinds := map[string]StepKind{}
	for _, s := range r.Steps {
		kinds[s.ID] = s.Kind
	}
	if kinds["judgment"] != StepAction || kinds["verify"] != StepGate || edges["judged"].ToStepID != "verify" || edges["verification-retry"].FromStepID != "verify" || edges["verification-retry"].ToStepID != "diagnosis" || edges["verification-exhausted"].ToStepID != "failed" || edges["needs-input"].ToStepID != "missing-input" || edges["missing-input-failed"].ToStepID != "failed" {
		t.Fatalf("missing failure paths: %+v", edges)
	}
	if edges["verification-retry"].MaxTraversals == 0 {
		t.Fatal("unbounded verifier retry")
	}
}

func TestDoerRevisionRoundTripAndCanonicalDigest(t *testing.T) {
	r := doerFixture(t)
	data, err := MarshalRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalRevision(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := MarshalRevision(decoded)
	if err != nil || !bytes.Equal(data, again) || decoded.Digest != r.Digest {
		t.Fatalf("roundtrip: %v", err)
	}
	if !strings.Contains(string(data), `"schema_version":"aegis.loop.revision.v4"`) {
		t.Fatal("wrong version")
	}
	copy := r
	contract := *r.Doer
	contract.Task = "Different task"
	copy.Doer = &contract
	if ValidateRevision(copy).Outcome != ValidationInvalid {
		t.Fatal("changed task retained digest")
	}
	copy = r
	binding := *r.Steps[0].Executable
	binding.Operation = DoerOperation("shell")
	copy.Steps = append([]Step(nil), r.Steps...)
	copy.Steps[0].Executable = &binding
	if ValidateRevision(copy).Outcome != ValidationInvalid {
		t.Fatal("changed binding retained digest")
	}
}

func TestDoerRevisionRejectsInvalidTaskPathAndAttempts(t *testing.T) {
	for name, change := range map[string]func(*DoerContract){
		"empty task":            func(c *DoerContract) { c.Task = " " },
		"long task":             func(c *DoerContract) { c.Task = strings.Repeat("x", 4097) },
		"absolute":              func(c *DoerContract) { c.VerifyFile = "/result.txt" },
		"escape":                func(c *DoerContract) { c.VerifyFile = "a/../result.txt" },
		"backslash":             func(c *DoerContract) { c.VerifyFile = `a\b` },
		"missing path":          func(c *DoerContract) { c.VerifyFile = "" },
		"missing workspace":     func(c *DoerContract) { c.Workspace = "" },
		"root workspace":        func(c *DoerContract) { c.Workspace = "/" },
		"unlisted verifier":     func(c *DoerContract) { c.WritableFiles = []string{"other.txt"} },
		"escaping writable":     func(c *DoerContract) { c.WritableFiles = []string{"../result.txt"} },
		"git metadata writable": func(c *DoerContract) { c.WritableFiles = []string{".git/config", "result.txt"} },
		"env writable":          func(c *DoerContract) { c.WritableFiles = []string{".env", "result.txt"} },
		"zero attempts":         func(c *DoerContract) { c.MaxAttempts = 0 },
		"excess attempts":       func(c *DoerContract) { c.MaxAttempts = 4 },
	} {
		t.Run(name, func(t *testing.T) {
			c := DoerContract{Task: "make file", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 3}
			change(&c)
			if _, _, err := NewDoerRevision("doer-test", 1, "", c); err == nil {
				t.Fatal("unsafe contract accepted")
			}
		})
	}
}

func TestDoerRevisionRejectsShapeAndPolicySubstitutions(t *testing.T) {
	for name, change := range map[string]func(*LoopRevision){
		"missing binding":    func(r *LoopRevision) { r.Steps[0].Executable = nil },
		"unknown backend":    func(r *LoopRevision) { r.Steps[0].Executable.Backend = "shell" },
		"unsealed authority": func(r *LoopRevision) { r.Steps[0].Executable.Authority = "ambient" },
		"wrong producer":     func(r *LoopRevision) { r.RequiredEvidence[0].ProducerStepID = "implement" },
		"wrong verifier": func(r *LoopRevision) {
			for i := range r.Steps {
				if r.Steps[i].ID == "verify" {
					r.Steps[i].EvidenceClaims[0].VerifierID = "self"
				}
			}
		},
		"extra transition": func(r *LoopRevision) {
			r.Transitions = append(r.Transitions, Transition{ID: "extra", FromStepID: "implement", ToStepID: "done"})
		},
		"branch substitution": func(r *LoopRevision) { r.Transitions[0].Condition = "other" },
		"unbounded retry": func(r *LoopRevision) {
			for i := range r.Transitions {
				if r.Transitions[i].MaxTraversals != 0 {
					r.Transitions[i].MaxTraversals = 0
				}
			}
		},
		"extra step": func(r *LoopRevision) {
			r.Steps = append(r.Steps, Step{ID: "shell", Kind: StepAction, Retry: RetryPolicy{MaxAttempts: 1}})
		},
		"v3 binding":  func(r *LoopRevision) { r.SchemaVersion = ImplementationRevisionSchemaVersion; r.Doer = nil },
		"v2 contract": func(r *LoopRevision) { r.SchemaVersion = RevisionSchemaVersion },
	} {
		t.Run(name, func(t *testing.T) {
			r := doerFixture(t)
			change(&r)
			if len(validateRevision(r, false)) == 0 {
				t.Fatal("unsafe shape accepted")
			}
		})
	}
}
