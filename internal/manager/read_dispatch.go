package manager

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/berryhill/aegis/internal/credentials"
)

// CredentialReadResult is the deterministic, user-facing result of a narrowly
// parsed credential read. Sensitive is true only when Message contains a value
// that must remain on the authenticated local manager presentation path.
type CredentialReadResult struct {
	Kind      string
	Message   string
	Sensitive bool
}

// IsDeterministicCredentialRead reports whether input names a complete read
// operation. It is intentionally stateless: ambiguous follow-ups never inherit
// a prior reference or operation.
func IsDeterministicCredentialRead(input string) bool {
	if !directCredentialReadRequest(input) {
		return false
	}
	if _, ok := ParseCredentialValueReadIntent(input); ok {
		return true
	}
	return ParseAuthorityReadIntent(input) != AuthorityReadUnknown
}

var directCredentialReadPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(?:how many|count(?: all| the)?|number of)(?: active| revoked)? (?:credentials|secrets)(?: do (?:i|we) have)?[?.!]?$`),
	regexp.MustCompile(`^(?:list|show(?: me)?)(?: all| my| the)?(?: active| revoked)? (?:credentials|secrets)[?.!]?$`),
	regexp.MustCompile(`^(?:what|which) (?:credentials|secrets) do (?:i|we) have[?.!]?$`),
	regexp.MustCompile(`^(?:find|search(?: for)?|show(?: me)?|list)(?: all)? [a-z0-9][a-z0-9._:/@+ -]{0,127} (?:credentials|secrets)[?.!]?$`),
	regexp.MustCompile(`^(?:find|search(?: for)?) (?:credentials|secrets)(?: matching| for| named)? [a-z0-9][a-z0-9._:/@+ -]{0,127}[?.!]?$`),
	regexp.MustCompile(`^(?:what is|what's|show(?: me)?|reveal|get) (?:the )?(?:(?:credential|secret) )?value (?:for|of) (?:credential|secret|cred):? .+[?.!]?$`),
	regexp.MustCompile(`^i need to see .+ (?:cred|credential|secret) value[?.!]?$`),
	regexp.MustCompile(`^(?:show|reveal|get) (?:credential|secret|cred) .+ value[?.!]?$`),
}

// directCredentialReadRequest requires a closed whole-input request shape before
// the argument parsers can turn text into an effect. Embedded tutorials,
// quotations, negations, conditionals, conjunctions, and trailing prose do not
// match this grammar.
// This is a context gate, not authorization: Operations must still perform the
// authenticated principal and authority checks for every handled request.
func directCredentialReadRequest(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" || strings.ContainsAny(normalized, "\r\n") {
		return false
	}
	for _, courtesy := range []string{"please ", "can you ", "could you ", "would you "} {
		if strings.HasPrefix(normalized, courtesy) {
			normalized = strings.TrimSpace(strings.TrimPrefix(normalized, courtesy))
			break
		}
	}
	if containsCredentialReadMetaword(normalized) {
		return false
	}
	for _, pattern := range directCredentialReadPatterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

func containsCredentialReadMetaword(normalized string) bool {
	words := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == ' ' || r == '\t' || strings.ContainsRune(`.,?!:;"'()[]{}<>`, r)
	})
	for index, word := range words {
		if word == "how" && index == 0 && len(words) > 1 && words[1] == "many" {
			continue
		}
		switch word {
		case "how", "why", "should", "tutorial", "example", "phrase", "explain", "describe", "quote", "repeat", "not", "never", "don't", "without", "unless", "if":
			return true
		}
	}
	return false
}

// DispatchCredentialRead parses and executes the closed credential-read
// surface without involving a model. Authentication, stanza, mandate, custody,
// persistence, and audit remain the responsibility of Operations.
func DispatchCredentialRead(ctx context.Context, operations Operations, input string) (CredentialReadResult, bool, error) {
	if !directCredentialReadRequest(input) {
		return CredentialReadResult{}, false, nil
	}
	if reference, ok := ParseCredentialValueReadIntent(input); ok {
		if operations == nil {
			return CredentialReadResult{}, true, errors.New(ReasonAuthorityUnavailable)
		}
		var message string
		err := operations.ReadValue(ctx, reference, func(record credentials.SecretRecord, value []byte) error {
			message = credentialValueResult(record, value)
			return nil
		})
		return CredentialReadResult{Kind: "credential_value", Message: message, Sensitive: true}, true, err
	}

	intent := ParseAuthorityReadIntent(input)
	if intent == AuthorityReadUnknown {
		return CredentialReadResult{}, false, nil
	}
	if operations == nil {
		return CredentialReadResult{}, true, errors.New(ReasonAuthorityUnavailable)
	}

	var result CredentialReadResult
	var err error
	switch intent {
	case AuthorityReadCount:
		result.Kind = "credential_count"
		counts, countErr := operations.Counts(ctx)
		result.Message, err = credentialCountResult(counts, countErr)
	case AuthorityReadList:
		result.Kind = "credential_list"
		records, listErr := operations.List(ctx, "", 100)
		result.Message, err = credentialListResult(records, "", listErr)
	case AuthorityReadSearch:
		result.Kind = "credential_search"
		query, ok := ParseCredentialSearchIntent(input)
		if !ok {
			return CredentialReadResult{}, true, errors.New(ReasonProposalInvalid)
		}
		records, searchErr := operations.List(ctx, query, 100)
		result.Message, err = credentialListResult(records, query, searchErr)
	}
	return result, true, err
}
