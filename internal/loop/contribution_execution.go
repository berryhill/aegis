package loop

import (
	"errors"
	"path/filepath"
	"regexp"
)

// ContributionExecutionPolicy is a closed, explicitly host-approved Go profile.
// All repository files are build inputs; .git is the only source exclusion.
// Native checks are not a sandbox. No arbitrary commands, environment, tools,
// dependency downloads, GitHub operations or model-supplied checks are accepted.
type ContributionCriterion struct {
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
	Test RequiredGoTest `json:"test"`
}

// ContributionReviewAsset is an exact-content review approved with the immutable
// host policy. It is deliberately not a model or builder verdict. Nonapplicable
// entries require an explicit reviewed reason and carry no file exemption.
type ContributionReviewAsset struct {
	CriterionID    string `json:"criterion_id,omitempty"`
	ID             string `json:"id"`
	Scope          string `json:"scope"`
	Path           string `json:"path"`
	BaselineDigest string `json:"baseline_digest"`
	ApprovedDigest string `json:"approved_digest"`
	NonApplicable  string `json:"non_applicable,omitempty"`
}

const TemplModuleConsistency = "templ-module-stdout.v1"

type ContributionExecutionPolicy struct {
	TemplGeneratedWrites bool                      `json:"templ_generated_writes,omitempty"`
	TemplConsistency     string                    `json:"templ_consistency,omitempty"`
	SourceInventory      *SourceInventoryPolicy    `json:"source_inventory,omitempty"`
	ReviewMode           string                    `json:"review_mode,omitempty"`
	Dependencies         *DependencyBundle         `json:"dependencies,omitempty"`
	ReviewAssets         []ContributionReviewAsset `json:"review_assets"`
	Criteria             []ContributionCriterion   `json:"criteria"`
	Kind                 string                    `json:"kind"`
	RepositoryPath       string                    `json:"repository_path"`
	BaseBranch           string                    `json:"base_branch"`
	GitBinary            string                    `json:"git_binary"`
	BuildPackage         string                    `json:"build_package"`
}

func (p ContributionExecutionPolicy) Validate() error {
	if p.TemplGeneratedWrites && (p.Kind != EngineeringExecutionKind || p.TemplConsistency != TemplModuleConsistency || p.Dependencies == nil) {
		return errors.New("generated writes require approved pinned template policy")
	}
	if p.TemplConsistency != "" && (p.Kind != EngineeringExecutionKind || p.TemplConsistency != TemplModuleConsistency || p.Dependencies == nil) {
		return errors.New("unsupported template consistency policy")
	}
	if p.SourceInventory != nil && (p.Kind != EngineeringExecutionKind || p.SourceInventory.Validate() != nil) {
		return errors.New("unsupported source inventory profile")
	}
	if p.ReviewMode != "" && (p.Kind != EngineeringExecutionKind || p.ReviewMode != "issue-journeys.v1") {
		return errors.New("unknown candidate review mode")
	}
	if p.Dependencies != nil && (p.Kind != EngineeringExecutionKind || p.Dependencies.Validate() != nil) {
		return errors.New("unsupported dependency input")
	}
	reviewIDs := map[string]bool{}
	scopes := map[string]bool{}
	if len(p.ReviewAssets) < 2 || len(p.ReviewAssets) > 128 {
		return errors.New("explicit bounded security and launch reviews required")
	}
	for _, a := range p.ReviewAssets {
		if a.ID == "" || reviewIDs[a.ID] || (a.Scope != "security" && a.Scope != "launch") {
			return errors.New("invalid review scope")
		}
		reviewIDs[a.ID] = true
		if a.NonApplicable != "" {
			if a.Path != "" || a.ApprovedDigest != "" || a.BaselineDigest != "" || len(a.NonApplicable) > 1024 {
				return errors.New("invalid explicit nonapplicable review")
			}
			continue
		}
		if !filepath.IsLocal(a.Path) || filepath.Clean(a.Path) != a.Path || (p.ReviewMode == "" && !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(a.ApprovedDigest)) || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(a.BaselineDigest) {
			return errors.New("exact reviewed asset identity required")
		}
		if p.ReviewMode == "issue-journeys.v1" {
			matched := false
			for _, c := range p.Criteria {
				matched = matched || (c.ID == a.CriterionID && c.Kind == a.Scope)
			}
			if !matched || a.ApprovedDigest != "" {
				return errors.New("asset requires independent candidate-bound review journey")
			}
		} else if a.CriterionID != "" {
			return errors.New("journey requires engineering review mode")
		}
		scopes[a.Scope] = true
	}
	if !scopes["security"] || !scopes["launch"] {
		return errors.New("actual security and launch assets required")
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	for _, c := range p.Criteria {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(c.ID) || seen[c.ID] {
			return errors.New("unique required contribution criteria required")
		}
		seen[c.ID] = true
		switch c.Kind {
		case "behavior", "cli", "denial", "persistence", "restart", "recovery", "security", "launch":
		default:
			return errors.New("unsupported contribution journey")
		}
		kinds[c.Kind] = true
	}
	if len(p.Criteria) < 3 || len(p.Criteria) > 128 || !kinds["behavior"] || !kinds["security"] || !kinds["launch"] || !kinds["denial"] {
		return errors.New("behavior, security and launch criteria required")
	}
	if (p.Kind != "go-stdlib-behavior.v1" && p.Kind != EngineeringExecutionKind) || !filepath.IsAbs(p.RepositoryPath) || filepath.Clean(p.RepositoryPath) != p.RepositoryPath || !filepath.IsAbs(p.GitBinary) || filepath.Clean(p.GitBinary) != p.GitBinary || !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`).MatchString(p.BaseBranch) || (p.BuildPackage != "." && (!goPackagePath.MatchString(p.BuildPackage) || regexp.MustCompile(`\.\.`).MatchString(p.BuildPackage))) {
		return errors.New("unsupported managed contribution execution profile")
	}
	return nil
}
