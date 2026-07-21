package manager

import (
	"regexp"
	"strings"
)

var (
	createActionPattern   = regexp.MustCompile(`(?i)\b(store|save|add|create|remember|keep|here(?:'s| is)|want to store)\b`)
	createObjectPattern   = regexp.MustCompile(`(?i)\b(secret|credential|credentials|password|passphrase|token|api[ _-]*key|key|g[ _-]*drive|google[ _-]*drive)\b`)
	createQuestionPattern = regexp.MustCompile(`(?i)^\s*(how|what|why|when|where|can|could|should|would|do|does|is|are)\b`)
	createLabelPattern    = regexp.MustCompile(`(?i)\b(?:secret|credential)\s*(?::|=|named\s+|called\s+)[\t ]*["']([^"'\r\n]{1,255})["']`)
	inlineValuePattern    = regexp.MustCompile(`(?i)\b(value\s+of|password|passphrase|token|api[\s_-]*key|key)(\s+(?:for|to)\s+[^:\r\n]{1,160})?\s*(?:is|=|:)?\s*(["'])([^"'\r\n]{1,1000})(["'])`)
	emailPattern          = regexp.MustCompile(`(?i)\b[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)
)

// CreateIntent is a deterministic, metadata-only interpretation of a clear
// operator request to create a credential. Any inline value is replaced before
// the request can enter presentation history, audit, Hermes, or model context.
type CreateIntent struct {
	Arguments    CreateArguments
	SafeInput    string
	ValueRemoved bool
}

func ParseCreateIntent(input string) (CreateIntent, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || createQuestionPattern.MatchString(trimmed) || !createActionPattern.MatchString(trimmed) || !createObjectPattern.MatchString(trimmed) {
		return CreateIntent{}, false
	}

	safe, removed := redactInlineValues(input)
	reference := "new-credential"
	if match := createLabelPattern.FindStringSubmatch(input); len(match) == 2 {
		reference = identifierSlug(match[1])
	} else {
		lower := strings.ToLower(safe)
		service := "credential"
		switch {
		case strings.Contains(lower, "google drive") || strings.Contains(lower, "g drive") || strings.Contains(lower, "g-drive"):
			service = "google-drive"
		case strings.Contains(lower, "gmail"):
			service = "gmail"
		case strings.Contains(lower, "password") || strings.Contains(lower, "passphrase"):
			service = "password"
		case strings.Contains(lower, "token"):
			service = "token"
		case strings.Contains(lower, "key"):
			service = "api-key"
		}
		if email := emailPattern.FindString(safe); email != "" {
			reference = service + "-" + identifierSlug(email)
		} else if service != "credential" {
			reference = service
		}
	}
	if reference == "" {
		reference = "new-credential"
	}
	kind := "opaque"
	lower := strings.ToLower(safe)
	if strings.Contains(lower, "key") || strings.Contains(lower, "token") {
		kind = "api-key"
	}
	return CreateIntent{
		Arguments:    CreateArguments{Reference: reference, Kind: kind, Disclosure: "protected"},
		SafeInput:    safe,
		ValueRemoved: removed,
	}, true
}

func redactInlineValues(input string) (string, bool) {
	matches := inlineValuePattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, false
	}
	var output strings.Builder
	start := 0
	for _, match := range matches {
		valueStart, valueEnd := match[8], match[9]
		output.WriteString(input[start:valueStart])
		output.WriteString("[protected value removed by Aegis]")
		start = valueEnd
	}
	output.WriteString(input[start:])
	return output.String(), true
}

func identifierSlug(input string) string {
	var output strings.Builder
	separator := false
	for _, value := range strings.ToLower(strings.TrimSpace(input)) {
		if value >= 'a' && value <= 'z' || value >= '0' && value <= '9' {
			if separator && output.Len() > 0 {
				output.WriteByte('-')
			}
			separator = false
			output.WriteRune(value)
		} else {
			separator = true
		}
		if output.Len() >= 96 {
			break
		}
	}
	return strings.Trim(output.String(), "-")
}
