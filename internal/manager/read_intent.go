package manager

import (
	"regexp"
	"strings"
)

type AuthorityReadIntent uint8

const (
	AuthorityReadUnknown AuthorityReadIntent = iota
	AuthorityReadCount
	AuthorityReadList
	AuthorityReadSearch
)

var (
	credentialObjectPattern = `(?:secrets?|credentials?|creds?|passwords?|passphrases?|tokens?|keys?)`
	credentialCountPattern  = regexp.MustCompile(`(?i)\b(?:how[	 ]*many|count|number[	 ]+of)\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b`)
	credentialListPattern   = regexp.MustCompile(`(?i)(?:\b(?:list|show)\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b|\bwhat\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b[^\r\n]{0,40}\b(?:have|stored)\b)`)
	credentialSearchBefore  = regexp.MustCompile(`(?i)\b(?:list|show)(?:[	 ]+me)?(?:[	 ]+all)?[	 ]+(?:my[	 ]+)?(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))[	 ]+` + credentialObjectPattern + `\b`)
	credentialSearchAfter   = regexp.MustCompile(`(?i)\b(?:search|find)\b[^\r\n]{0,40}\b` + credentialObjectPattern + `\b[	 ]+(?:for|matching|containing)[	 ]+(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))`)
	credentialValueAction   = regexp.MustCompile(`(?i)\b(?:what[	 ]+is|show|give|retrieve|get|reveal|tell|see|view|display)\b`)
	credentialValueObject   = regexp.MustCompile(`(?i)\b(?:value|password|secret[	 ]+value|credential[	 ]+value)\b`)
	credentialNamedRef      = regexp.MustCompile(`(?i)\b(?:credential|secret|cred)[	 ]*(?::|name(?:d|s)?[	 ]+|called[	 ]+)[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))`)
	credentialValueRef      = regexp.MustCompile(`(?i)\b(?:value|password)[	 ]+(?:for|of)[	 ]+(?:the[	 ]+)?(?:credential|secret|cred)?[	 ]*:?[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))`)
	credentialLeadingRef    = regexp.MustCompile(`(?i)(?:^|[	 ])(?:the[	 ]+)?(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))[	 ]+(?:credential|secret|cred)(?:'s)?[	 ]+(?:value|password)\b`)
)

func ParseAuthorityReadIntent(input string) AuthorityReadIntent {
	if credentialCountPattern.MatchString(input) {
		return AuthorityReadCount
	}
	if _, ok := ParseCredentialSearchIntent(input); ok {
		return AuthorityReadSearch
	}
	if credentialListPattern.MatchString(input) {
		return AuthorityReadList
	}
	return AuthorityReadUnknown
}

// ParseCredentialSearchIntent extracts a user-supplied metadata filter from
// narrow, unambiguous read-only forms. It runs before generic list detection so
// "show me all doppler secrets" cannot silently degrade into an unfiltered list.
func ParseCredentialSearchIntent(input string) (string, bool) {
	for _, pattern := range []*regexp.Regexp{credentialSearchBefore, credentialSearchAfter} {
		match := pattern.FindStringSubmatch(input)
		if len(match) != 4 {
			continue
		}
		for _, candidate := range match[1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" || ambiguousSearchTerm(candidate) {
				continue
			}
			return candidate, true
		}
	}
	return "", false
}

func ambiguousSearchTerm(candidate string) bool {
	switch strings.ToLower(candidate) {
	case "all", "any", "my", "our", "the":
		return true
	default:
		return false
	}
}

func ParseCredentialValueReadIntent(input string) (string, bool) {
	if !credentialValueAction.MatchString(input) || !credentialValueObject.MatchString(input) {
		return "", false
	}
	for _, pattern := range []*regexp.Regexp{credentialNamedRef, credentialValueRef, credentialLeadingRef} {
		matches := pattern.FindAllStringSubmatch(input, -1)
		if len(matches) != 1 {
			continue
		}
		for index, candidate := range matches[0][1:] {
			if index == 2 && ambiguousUnquotedReference(candidate) {
				continue
			}
			if reference := identifierSlug(candidate); reference != "" {
				return reference, true
			}
		}
	}
	return "", false
}

func ambiguousUnquotedReference(candidate string) bool {
	switch strings.ToLower(candidate) {
	case "a", "an", "my", "that", "the", "this":
		return true
	default:
		return false
	}
}
