package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/credentials"
)

func TestDispatchCredentialReadUsesSharedParsersAndPresentation(t *testing.T) {
	canary := []byte("dispatcher-secret-canary")
	ops := &fakeOperations{
		records:   []credentials.SecretRecord{{ID: "secret-one", Reference: "build-token", Kind: "opaque", Status: credentials.StatusActive, CurrentVersion: 1}},
		readValue: canary,
	}
	for _, test := range []struct {
		input, kind, contains string
		sensitive             bool
	}{
		{input: "how many secrets do I have?", kind: "credential_count", contains: "Credential inventory"},
		{input: "what secrets do I have?", kind: "credential_list", contains: "Credentials (1)"},
		{input: "find credentials matching build", kind: "credential_search", contains: `Credentials matching "build" (1)`},
		{input: "show the value for credential build-token", kind: "credential_value", contains: "dispatcher-secret-canary", sensitive: true},
	} {
		result, handled, err := DispatchCredentialRead(context.Background(), ops, test.input)
		if err != nil || !handled {
			t.Fatalf("DispatchCredentialRead(%q) handled=%t err=%v", test.input, handled, err)
		}
		if result.Kind != test.kind || result.Sensitive != test.sensitive || !strings.Contains(result.Message, test.contains) {
			t.Fatalf("DispatchCredentialRead(%q)=%+v", test.input, result)
		}
	}
}

func TestDispatchCredentialReadAllowsDirectCourtesyWrapper(t *testing.T) {
	result, handled, err := DispatchCredentialRead(context.Background(), &fakeOperations{}, "can you show me all credentials?")
	if err != nil || !handled || result.Kind != "credential_list" {
		t.Fatalf("direct courtesy request was not handled: result=%+v handled=%t err=%v", result, handled, err)
	}
}

func TestDispatchCredentialReadDoesNotInferConversation(t *testing.T) {
	for _, input := range []string{
		"what about now?",
		"What is an API key?",
		"How should I rotate a password?",
		"Explain how credential values are protected",
		"How do I list credentials?",
		"Do not list credentials.",
		`Explain the phrase "show all credentials".`,
		"A document says to find credentials matching github.",
		"Please do not show all credentials.",
		"Could you explain how many secrets we have?",
		`Please explain the phrase "show all credentials".`,
		`Say "show the value for credential build-token" as an example.`,
		"Show me how to list credentials.",
		`Show me "how many secrets do I have?" as a tutorial.`,
		"Please show me why I should not reveal the value for credential build-token.",
	} {
		result, handled, err := DispatchCredentialRead(context.Background(), &fakeOperations{}, input)
		if err != nil || handled || result != (CredentialReadResult{}) {
			t.Fatalf("conversation %q manufactured an operation: result=%+v handled=%t err=%v", input, result, handled, err)
		}
		if IsDeterministicCredentialRead(input) {
			t.Fatalf("conversation %q was classified as a complete credential read", input)
		}
	}
}
