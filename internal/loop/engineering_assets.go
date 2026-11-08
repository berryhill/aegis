package loop

import "strings"

// EngineeringTextAsset is a closed non-command source class. Approval still
// requires the immutable prefix/exact-asset policy and an exact issue selection.
// No shell, executable JS, workflow, environment, module or tool policy is added.
func EngineeringTextAsset(p string) bool {
	switch p {
	case "README.md", "CHANGELOG.md", "CONTRIBUTING.md", "CODE_OF_CONDUCT.md":
		return true
	}
	return (strings.HasPrefix(p, "docs/") && strings.HasSuffix(p, ".md")) || (strings.HasPrefix(p, "web/") && (strings.HasSuffix(p, ".templ") || strings.HasSuffix(p, ".css")))
}
