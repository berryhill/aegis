package slash

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaximumInputBytes = 64 << 10
	MaximumArguments  = 32
	MaximumValueBytes = 4096
)

type Detection int

const (
	Ordinary Detection = iota
	Command
	LiteralSlash
)

type Request struct {
	Raw        string
	Name       string
	Canonical  string
	Alias      string
	Arguments  []string
	Definition Definition
}

type ParseError struct {
	Reason      string
	Message     string
	Suggestions []string
}

func (e *ParseError) Error() string { return e.Message }

func Detect(input string) Detection {
	trimmed := strings.TrimLeftFunc(input, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "//") {
		return LiteralSlash
	}
	if strings.HasPrefix(trimmed, "/") {
		return Command
	}
	return Ordinary
}

func UnescapeLiteral(input string) string {
	index := 0
	for index < len(input) {
		r, size := utf8.DecodeRuneInString(input[index:])
		if !unicode.IsSpace(r) {
			break
		}
		index += size
	}
	if strings.HasPrefix(input[index:], "//") {
		return input[:index] + input[index+1:]
	}
	return input
}

func (r *Registry) Parse(input string) (Request, error) {
	if len(input) > MaximumInputBytes {
		return Request{}, &ParseError{Reason: "input_too_large", Message: "slash command exceeds the bounded input size"}
	}
	if Detect(input) != Command {
		return Request{}, &ParseError{Reason: "not_command", Message: "input is not a slash command"}
	}
	trimmed := strings.TrimLeftFunc(input, unicode.IsSpace)
	tokens, err := tokenize(trimmed[1:])
	if err != nil {
		return Request{}, err
	}
	if len(tokens) == 0 {
		return Request{}, &ParseError{Reason: "missing_command", Message: "slash command name is required"}
	}
	if len(tokens)-1 > MaximumArguments {
		return Request{}, &ParseError{Reason: "too_many_arguments", Message: "slash command has too many arguments"}
	}
	name := tokens[0]
	if name != strings.ToLower(name) {
		return Request{}, &ParseError{Reason: "noncanonical_case", Message: "slash command matching is exact lowercase"}
	}
	definition, ok := r.Lookup(name)
	if !ok {
		return Request{}, &ParseError{Reason: "unknown_command", Message: "unknown local slash command /" + name, Suggestions: r.suggest("/" + name)}
	}
	request := Request{Raw: input, Name: name, Canonical: definition.Name, Arguments: tokens[1:], Definition: definition}
	if name != definition.Name {
		request.Alias = name
		request.Arguments = canonicalAliasArguments(name, request.Arguments)
	}
	if err := validateGrammar(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func canonicalAliasArguments(alias string, arguments []string) []string {
	profile := map[string]string{"scan-secrets": "secrets", "scan-processes": "processes", "scan-network": "network", "scan-files": "files"}[alias]
	if profile == "" {
		return arguments
	}
	return append([]string{profile}, arguments...)
}

func tokenize(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false
	started := false
	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}
	for _, r := range input {
		if escaped {
			if quote == 0 || (r != '\\' && r != quote) {
				return nil, &ParseError{Reason: "invalid_escape", Message: "only a quote or backslash may be escaped inside a quoted literal"}
			}
			current.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		if r == '\\' {
			if quote == 0 {
				return nil, &ParseError{Reason: "shell_syntax_denied", Message: "backslash shell escaping is not supported; quote a literal value"}
			}
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			if unicode.IsControl(r) && r != '\t' {
				return nil, &ParseError{Reason: "control_character", Message: "control characters are not allowed in slash arguments"}
			}
			current.WriteRune(r)
			started = true
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
			started = true
		case unicode.IsSpace(r):
			flush()
		case strings.ContainsRune("|;&<>!$`", r):
			return nil, &ParseError{Reason: "shell_syntax_denied", Message: fmt.Sprintf("shell operator %q is not supported in slash commands", r)}
		case unicode.IsControl(r):
			return nil, &ParseError{Reason: "control_character", Message: "control characters are not allowed in slash commands"}
		default:
			current.WriteRune(r)
			started = true
			if current.Len() > MaximumValueBytes {
				return nil, &ParseError{Reason: "value_too_large", Message: "slash argument exceeds the bounded value size"}
			}
		}
	}
	if escaped || quote != 0 {
		return nil, &ParseError{Reason: "unterminated_quote", Message: "unterminated quoted slash argument"}
	}
	flush()
	return tokens, nil
}

func validateGrammar(request Request) error {
	args := request.Arguments
	usage := func() error { return &ParseError{Reason: "usage", Message: "usage: " + request.Definition.Usage} }
	noArgs := map[string]bool{"status": true, "context": true, "limits": true, "clear": true, "exit": true}
	if noArgs[request.Canonical] && len(args) != 0 {
		return usage()
	}
	switch request.Canonical {
	case "help":
		if len(args) > 1 {
			return usage()
		}
	case "authority":
		if len(args) > 1 {
			return usage()
		}
	case "scan":
		if len(args) == 0 {
			return nil
		}
		if args[0] == "list" && len(args) == 1 {
			return nil
		}
		if args[0] == "status" && len(args) == 2 {
			return nil
		}
		if len(args) == 1 && contains([]string{"core", "quick", "full", "secrets", "processes", "network", "files", "persistence", "permissions", "runtime", "dependencies", "configuration", "sensors"}, args[0]) {
			return nil
		}
		return usage()
	case "watch":
		if len(args) == 0 {
			return nil
		}
		if contains([]string{"list", "start"}, args[0]) && len(args) <= 2 {
			return nil
		}
		if contains([]string{"status", "events", "stop"}, args[0]) && len(args) <= 2 {
			return nil
		}
		return usage()
	case "findings", "investigate", "report", "cancel":
		if len(args) > 1 {
			return usage()
		}
	case "timeline":
		if len(args) != 0 {
			return usage()
		}
	case "audit":
		if len(args) > 1 || (len(args) == 1 && args[0] != "verify") {
			return usage()
		}
	case "complete":
		if len(args) != 1 {
			return usage()
		}
	case "secret":
		if len(args) < 1 || !contains([]string{"list", "show"}, args[0]) {
			return usage()
		}
		if args[0] == "show" && len(args) != 2 {
			return usage()
		}
		if args[0] == "list" && len(args) > 2 {
			return usage()
		}
	}
	return nil
}

func (r *Registry) suggest(input string) []string {
	var candidates []string
	for _, definition := range r.definitions {
		candidate := "/" + definition.Name
		if strings.HasPrefix(candidate, input) || distance(candidate, input) <= 2 {
			candidates = append(candidates, candidate)
		}
		for _, alias := range definition.Aliases {
			candidate = "/" + alias
			if strings.HasPrefix(candidate, input) || distance(candidate, input) <= 2 {
				candidates = append(candidates, candidate)
			}
		}
	}
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	return candidates
}

func distance(a, b string) int {
	if a == b {
		return 0
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ar := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, br := range b {
			cost := 0
			if ar != br {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

var ErrUnavailable = errors.New("command unavailable")
