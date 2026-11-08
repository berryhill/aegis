package contribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// EngineeringPlanningAllowance is fixed by immutable checker context, not a run
// retry. Every reservation (including failed calls) consumes this allocation and
// is imported into the unchanged cumulative runtime budget. At least 32 calls
// remain for builder, independent test, diagnosis, review and bounded repair.
const ContributionRuntimeTurns uint32 = 128
const EngineeringExecutionReserve uint32 = 32

func EngineeringPlanningAllowance(template loop.VerifiedImplementation) (uint32, error) {
	m := template.RepositoryContext
	if m == nil || m.MaxTotalBytes <= 0 || m.MaxFiles <= 0 {
		return 0, errors.New("immutable planning bounds required")
	}
	// A separate final short chunk per selected file, plus discovery/inspect/scope.
	chunks := (m.MaxTotalBytes + EngineeringChunkBytes - 1) / EngineeringChunkBytes
	return uint32(min(int64(ContributionRuntimeTurns-EngineeringExecutionReserve), 3+chunks+int64(m.MaxFiles))), nil
}

// IssueEngineeringScope is untrusted issue data. It can only select a subset of
// the published host-approved policy, never commands, checks or role authority.
// A closed fenced document avoids guessing writable scope from arbitrary prose.
type IssueEngineeringScope struct {
	SchemaVersion string   `json:"schema_version"`
	Outcome       string   `json:"outcome"`
	Acceptance    string   `json:"acceptance"`
	WritableFiles []string `json:"writable_files"`
	ContextFiles  []string `json:"context_files"`
	Exclusions    []string `json:"exclusions"`
}

func ParseEngineeringScope(source Source, policy loop.IssueEngineeringPolicy) (IssueEngineeringScope, error) {
	var scope IssueEngineeringScope
	deny := errors.New("issue requires one bounded aegis-engineering scope within published policy")
	const marker = "```aegis-engineering\n"
	if policy.Validate() != nil || strings.Count(source.Body, marker) != 1 {
		return scope, deny
	}
	tail := strings.SplitN(source.Body, marker, 2)[1]
	end := strings.Index(tail, "```")
	if end < 0 {
		return scope, deny
	}
	return DecodeEngineeringScope([]byte(tail[:end]), policy)
}

// DecodeEngineeringScope validates an internal runtime proposal as task data.
// It cannot replace published checks, role authority or cumulative budgets.
func DecodeEngineeringScope(wire []byte, policy loop.IssueEngineeringPolicy) (IssueEngineeringScope, error) {
	var scope IssueEngineeringScope
	deny := errors.New("bounded engineering proposal within published policy required")
	if policy.Validate() != nil {
		return scope, deny
	}
	if len(wire) > 16384 || reference.RejectDuplicateObjectKeys(wire) != nil {
		return scope, deny
	}
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	if d.Decode(&scope) != nil || d.Decode(new(any)) != io.EOF || scope.SchemaVersion != loop.IssueEngineeringSchema || strings.TrimSpace(scope.Outcome) == "" || len(scope.Outcome) > 4096 || strings.TrimSpace(scope.Acceptance) == "" || len(scope.Acceptance) > 4096 || len(scope.WritableFiles) < 2 || len(scope.WritableFiles) > int(policy.MaxWritableFiles) || len(scope.ContextFiles) > 64 || scope.Exclusions == nil || len(scope.Exclusions) > 16 {
		return scope, deny
	}
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, scope.WritableFiles...), scope.ContextFiles...) {
		if !policy.Allows(p) || seen[p] {
			return scope, deny
		}
		seen[p] = true
	}
	for _, x := range scope.Exclusions {
		if strings.TrimSpace(x) == "" || len(x) > 1024 {
			return scope, deny
		}
	}
	return scope, nil
}

// EngineeringWorktree allocates a task-owned sibling, never the template path.
func EngineeringWorktree(template, run string) string {
	return filepath.Join(filepath.Dir(template), "engineering-"+strings.TrimPrefix(Digest([]byte(run)), "sha256:"))
}

func validateEngineeringBinding(template loop.VerifiedImplementation, profile loop.ContributionProfile, c Contract) error {
	deny := errors.New("exact issue-derived engineering binding required")
	if profile.Engineering == nil || c.Engineering == nil || template.RepositoryContext == nil {
		return deny
	}
	if c.Preparation == nil || c.Preparation.Validate(c.Source, *profile.Engineering, c.TemplateDigest, c.Base) != nil {
		return deny
	}
	spec := c.Preparation.Scope
	writable, err := loop.EngineeringWritableSelection(spec.WritableFiles, *profile.Engineering, profile.Managed)
	if err != nil {
		return deny
	}
	v := *c.Engineering
	if v.SchemaVersion != loop.RepositoryEngineeringSchema || v.Validate() != nil || v.Contribution != nil || v.Workspace != EngineeringWorktree(template.Workspace, c.RunID) || v.Workspace != c.Worktree || v.Task != spec.Outcome || v.Task != c.Outcome || v.Acceptance != spec.Acceptance || v.Acceptance != c.Acceptance || v.MaxPasses != template.MaxPasses || v.MaxPasses != c.CandidateBudget || v.DecisionMode != template.DecisionMode || !reflect.DeepEqual(v.Policy, template.Policy) || !reflect.DeepEqual(v.WritableFiles, writable) || !reflect.DeepEqual(c.Exclusions, append(append([]string{}, profile.Exclusions...), spec.Exclusions...)) {
		return deny
	}
	if v.RepositoryContext == nil {
		return deny
	}
	m := v.RepositoryContext
	old := template.RepositoryContext
	if m.MaxFiles != old.MaxFiles || m.MaxFileBytes != old.MaxFileBytes || m.MaxTotalBytes != old.MaxTotalBytes || m.TaskDigest != Digest([]byte(v.Task)) {
		return deny
	}
	// Every fixed context input, especially trusted acceptance/module/check policy,
	// is preserved exactly. Issue paths may add context but never replace it.
	if len(m.Files) != len(old.Files)+len(writable)+len(spec.ContextFiles) {
		return deny
	}
	for i, f := range old.Files {
		if m.Files[i] != f {
			return deny
		}
	}
	for i, p := range append(append([]string{}, writable...), spec.ContextFiles...) {
		f := m.Files[len(old.Files)+i]
		role := "source"
		if i < len(writable) && strings.HasSuffix(p, "_test.go") {
			role = "regression_test"
		}
		if f.Path != p || f.Role != role {
			return deny
		}
	}
	wire, _ := json.Marshal(struct {
		Kind  string                       `json:"kind"`
		Files []loop.RepositoryContextFile `json:"files"`
	}{"explicit-go-source.v1", m.Files})
	if m.SourceDigest != Digest(wire) {
		return deny
	}
	return nil
}
