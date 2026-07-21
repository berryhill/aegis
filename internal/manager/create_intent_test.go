package manager

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseCreateIntentProducesSafeMetadataOnlyProposal(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		reference  string
		kind       string
		removed    bool
		notPresent string
	}{
		{name: "label and value", input: `store this secret: "test" with a value of "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "natural google drive key", input: `hey, here's my personal g drive key for person@example.com: "disposable-test-value"`, reference: "google-drive-person-example-com", kind: "api-key", removed: true, notPresent: "disposable-test-value"},
		{name: "no schema vocabulary required", input: "I want to store a new credential", reference: "new-credential", kind: "opaque"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, ok := ParseCreateIntent(test.input)
			if !ok {
				t.Fatal("create intent not recognized")
			}
			if intent.Arguments.Reference != test.reference || intent.Arguments.Kind != test.kind || intent.Arguments.Disclosure != "protected" || intent.ValueRemoved != test.removed {
				t.Fatalf("intent=%#v", intent)
			}
			if test.notPresent != "" && strings.Contains(intent.SafeInput, test.notPresent) {
				t.Fatal("inline value survived safe input")
			}
			guard, err := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			finding := guard.Inspect(context.Background(), ContentEnvelope{Source: SourceUser, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, RouteClass: "local", Content: []byte(intent.SafeInput)})
			if finding.Decision != AllowLocal {
				t.Fatalf("safe metadata blocked: %#v input=%q", finding, intent.SafeInput)
			}
		})
	}
}

func TestParseCreateIntentDoesNotTurnQuestionsIntoMutations(t *testing.T) {
	for _, input := range []string{
		"How do I store a credential?",
		"Can you explain API keys?",
		"Tell me about encrypted storage.",
	} {
		if _, ok := ParseCreateIntent(input); ok {
			t.Fatalf("discussion parsed as create intent: %q", input)
		}
	}
}
