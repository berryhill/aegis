package manager

import (
	"regexp"
	"strings"
)

var (
	createActionPattern   = regexp.MustCompile(`(?i)\b(store|save|stash|add|create|make|remember|keep|here(?:'s| is)|want to store)\b`)
	createStayTypoPattern = regexp.MustCompile(`(?i)\bwant[	 ]+to[	 ]+stay\b`)
	createObjectPattern   = regexp.MustCompile(`(?i)\b(secret|credential|credentials|cred|password|passphrase|token|api[ _-]*key|key|g[ _-]*drive|google[ _-]*drive|(?:secret|credential|cred)name(?:d|s)?)\b`)
	createQuestionPattern = regexp.MustCompile(`(?i)^\s*(how|what|why|when|where|can|could|should|would|do|does|is|are)\b`)
	createLabelPattern    = regexp.MustCompile(`(?i)\b(?:secret|credential|cred)\s*(?::|=|name(?:d|s)?\s+|called\s+)[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._-]{0,254}))`)
	keyFieldPattern       = regexp.MustCompile(`(?i)\bkey[	 ]*:[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([^\s,;]{1,255}))`)
	secretFieldPattern    = regexp.MustCompile(`(?i)\bsecret[	 ]*:[	 ]*(?:"([^"\r\n]{1,1000})"|'([^'\r\n]{1,1000})'|([^\s,;]{1,1000}))`)
	inlineValuePattern    = regexp.MustCompile(`(?i)\b(value\s+of|password|passphrase|token|api[\s_-]*key|key)(\s+(?:for|to)\s+[^:\r\n]{1,160})?\s*(?:is|=|:)?\s*(["'])([^"'\r\n]{1,1000})(["'])`)
	unquotedValuePattern  = regexp.MustCompile(`(?i)\b(?:with[	 ]+)?(?:a[	 ]+)?value(?:[	 ]+of|[	 ]+is|[	 ]*[=:])[	 ]+([^\s,;]{1,1000})`)
	emailPattern          = regexp.MustCompile(`(?i)\b[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)
)

// CreateIntent is a deterministic interpretation of a clear operator request
// to create a credential. SafeInput replaces any inline value for retained
// presentation/history; Value is the session-scoped copy admitted only to the
// deterministic authenticated authority create path.
type CreateIntent struct {
	Arguments    CreateArguments
	SafeInput    string
	ValueRemoved bool
	Value        []byte
}

func ParseCreateIntent(input string) (CreateIntent, bool) {
	trimmed := strings.TrimSpace(input)
	typoAction := createStayTypoPattern.MatchString(trimmed) && keyFieldPattern.MatchString(trimmed) && secretFieldPattern.MatchString(trimmed)
	if trimmed == "" || createQuestionPattern.MatchString(trimmed) || (!createActionPattern.MatchString(trimmed) && !typoAction) || !createObjectPattern.MatchString(trimmed) {
		return CreateIntent{}, false
	}

	safe, value, removed := redactInlineValues(input)
	reference := "new-credential"
	pairedReference, pairedValue, pairedSafe, paired := pairedKeySecretFields(input)
	if paired {
		reference, value, safe, removed = identifierSlug(pairedReference), pairedValue, pairedSafe, true
	} else if match := createLabelPattern.FindStringSubmatch(input); len(match) == 4 {
		label := match[1]
		if label == "" {
			label = match[2]
		}
		if label == "" {
			label = match[3]
		}
		reference = identifierSlug(label)
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
	if !paired && (strings.Contains(lower, "key") || strings.Contains(lower, "token")) {
		kind = "api-key"
	}
	return CreateIntent{
		Arguments:    CreateArguments{Reference: reference, Kind: kind, Disclosure: "protected"},
		SafeInput:    safe,
		ValueRemoved: removed,
		Value:        value,
	}, true
}

// pairedKeySecretFields supports the common operator shorthand
// `key: "record-name" secret: "credential-value"`. The pair is recognized only
// when both fields are unique; ambiguity falls back to protected intake.
func pairedKeySecretFields(input string) (string, []byte, string, bool) {
	keys := keyFieldPattern.FindAllStringSubmatchIndex(input, -1)
	secrets := secretFieldPattern.FindAllStringSubmatchIndex(input, -1)
	if len(keys) != 1 || len(secrets) != 1 {
		return "", nil, "", false
	}
	key, _, _, keyOK := fieldValue(input, keys[0])
	secret, start, end, secretOK := fieldValue(input, secrets[0])
	if !keyOK || !secretOK || start < 0 || end < start {
		return "", nil, "", false
	}
	safe := input[:start] + "[protected session value]" + input[end:]
	return key, []byte(secret), safe, true
}

func fieldValue(input string, match []int) (string, int, int, bool) {
	for group := 1; group <= 3; group++ {
		startIndex := group * 2
		if len(match) <= startIndex+1 || match[startIndex] < 0 {
			continue
		}
		start, end := match[startIndex], match[startIndex+1]
		return input[start:end], start, end, true
	}
	return "", -1, -1, false
}

func redactInlineValues(input string) (string, []byte, bool) {
	matches := inlineValuePattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		unquoted := unquotedValuePattern.FindAllStringSubmatchIndex(input, -1)
		if len(unquoted) == 1 && len(unquoted[0]) == 4 {
			match := unquoted[0]
			value := append([]byte(nil), input[match[2]:match[3]]...)
			return input[:match[2]] + "[protected session value]" + input[match[3]:], value, true
		}
		if len(unquoted) > 1 {
			return unquotedValuePattern.ReplaceAllString(input, "value of [protected session value]"), nil, true
		}
		return input, nil, false
	}
	var output strings.Builder
	start := 0
	var value []byte
	for _, match := range matches {
		valueStart, valueEnd := match[8], match[9]
		if value == nil {
			value = append([]byte(nil), input[valueStart:valueEnd]...)
		}
		output.WriteString(input[start:valueStart])
		output.WriteString("[protected session value]")
		start = valueEnd
	}
	output.WriteString(input[start:])
	if len(matches) > 1 {
		for index := range value {
			value[index] = 0
		}
		return output.String(), nil, true
	}
	return output.String(), value, true
}

func (intent *CreateIntent) Wipe() {
	for index := range intent.Value {
		intent.Value[index] = 0
	}
	intent.Value = nil
}

// ContainsInlineCredentialValue identifies explicit credential-value syntax
// that must fail closed instead of reaching Hermes when no create grammar maps it.
func ContainsInlineCredentialValue(input string) bool {
	return createObjectPattern.MatchString(input) && (inlineValuePattern.MatchString(input) || unquotedValuePattern.MatchString(input) || secretFieldPattern.MatchString(input))
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
