package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
		missing    bool
		removed    bool
		notPresent string
	}{
		{name: "label and value", input: `store this secret: "test" with a value of "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "basic trusted local interaction", input: `let's store a cred named test with a value of 1234`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "named credential value is", input: `i want to store a new cred named test.. value is 1234`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "named credential secret of", input: `I want to make a new cred named "test" with a secret of "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "unquoted secret of", input: `I want to make a new cred named test with a secret of 1234`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "missing space before named", input: `I want to store a new secretnamed test with a value of 1234`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "plural names typo", input: `I want to save a test credential names "test" with a value of "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "paired key and secret fields", input: `I want to store a test secret.. key: "test" secret: "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "typo tolerant paired fields", input: `I want to stay a test cred.. key: "test" secret: "1234"`, reference: "test", kind: "opaque", removed: true, notPresent: "1234"},
		{name: "natural google drive key", input: `hey, here's my personal g drive key for person@example.com: "disposable-test-value"`, reference: "google-drive-person-example-com", kind: "api-key", removed: true, notPresent: "disposable-test-value"},
		{name: "missing reference requires intake", input: "I want to store a new credential", reference: "", kind: "opaque", missing: true},
		{name: "terse new cred starts direct intake", input: "new cred", reference: "", kind: "opaque", missing: true},
		{name: "terse add token starts direct intake", input: "add token", reference: "", kind: "api-key", missing: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, ok := ParseCreateIntent(test.input)
			if !ok {
				t.Fatal("create intent not recognized")
			}
			if intent.Arguments.Reference != test.reference || intent.Arguments.Kind != test.kind || intent.Arguments.Disclosure != "protected" || intent.ReferenceMissing != test.missing || intent.ValueRemoved != test.removed {
				t.Fatalf("intent=%#v", intent)
			}
			if test.notPresent != "" && strings.Contains(intent.SafeInput, test.notPresent) {
				t.Fatal("inline value survived safe input")
			}
			if test.removed && string(intent.Value) != test.notPresent {
				t.Fatalf("captured value=%q want %q", intent.Value, test.notPresent)
			}
			guard, err := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			finding := guard.Inspect(context.Background(), ContentEnvelope{Source: SourceUser, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, RouteClass: "local", Content: []byte(intent.SafeInput)})
			if finding.Decision != AllowLocal {
				t.Fatalf("safe metadata blocked: %#v input=%q", finding, intent.SafeInput)
			}
			intent.Wipe()
			if intent.Value != nil {
				t.Fatal("sensitive intent value retained after wipe")
			}
		})
	}
}

func TestParseCreateIntentDoesNotTurnQuestionsIntoMutations(t *testing.T) {
	for _, input := range []string{
		"How do I store a credential?",
		"Can you explain API keys?",
		"Tell me about encrypted storage.",
		"I want to stay a test credential.",
	} {
		if _, ok := ParseCreateIntent(input); ok {
			t.Fatalf("discussion parsed as create intent: %q", input)
		}
	}
}

func TestNormalizeCredentialReferenceAcceptsHumanName(t *testing.T) {
	if got := NormalizeCredentialReference("  Berryhill GHCR token  "); got != "berryhill-ghcr-token" {
		t.Fatalf("reference=%q", got)
	}
}

func TestParseMakeCredentialIntentRedactsInlineValue(t *testing.T) {
	canaryBytes := make([]byte, 16)
	if _, err := rand.Read(canaryBytes); err != nil {
		t.Fatal(err)
	}
	canary := hex.EncodeToString(canaryBytes)
	input := "alright, I want to make a new cred named test with a value of " + canary
	intent, ok := ParseCreateIntent(input)
	if !ok || intent.Arguments.Reference != "test" || !intent.ValueRemoved || string(intent.Value) != canary {
		t.Fatalf("intent=%+v matched=%t", intent.Arguments, ok)
	}
	if strings.Contains(intent.SafeInput, canary) {
		t.Fatal("inline credential value remained in safe presentation input")
	}
	intent.Wipe()
}

func TestUnrecognizedInlineCredentialSyntaxFailsClosed(t *testing.T) {
	input := "frobnicate a credential named test with a value of generated-canary"
	if _, ok := ParseCreateIntent(input); ok {
		t.Fatal("unknown create verb unexpectedly mapped")
	}
	if !ContainsInlineCredentialValue(input) {
		t.Fatal("unknown credential-bearing syntax would reach Hermes")
	}
}
