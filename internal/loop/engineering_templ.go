package loop

import (
	"errors"
	"strings"
)

// EngineeringWritableSelection derives only exact paired outputs inside the same
// independently approved policy. Explicit generated-write permission is separate
// from read-only consistency checking. No role/stanza authorities are unioned.
func EngineeringWritableSelection(selected []string, scope IssueEngineeringPolicy, native *ContributionExecutionPolicy) ([]string, error) {
	out := append([]string{}, selected...)
	seen := map[string]bool{}
	for _, p := range selected {
		seen[p] = true
	}
	for _, p := range selected {
		if !strings.HasSuffix(p, ".templ") {
			continue
		}
		output := strings.TrimSuffix(p, ".templ") + "_templ.go"
		if native == nil || !native.TemplGeneratedWrites || native.TemplConsistency != TemplModuleConsistency || native.Dependencies == nil || !scope.Allows(p) || !scope.Allows(output) {
			return nil, errors.New("approved exact template generated-write policy required")
		}
		if !seen[output] {
			out = append(out, output)
			seen[output] = true
		}
	}
	if len(out) > int(scope.MaxWritableFiles) {
		return nil, errors.New("derived generated writes exceed engineering scope")
	}
	return out, nil
}
