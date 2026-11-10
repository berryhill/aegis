package loop

import (
	"errors"
	"github.com/berryhill/aegis/internal/reference"
	"regexp"
)

const ContributionRevisionSchemaVersion = "aegis.loop.revision.v6"

func NewContributionRevision(id string, revision uint64, previous string, implementation VerifiedImplementation, profile ContributionProfile) (LoopRevision, LoopValidationResult, error) {
	r, _, err := NewImplementationRevision(id, revision, previous, implementation)
	if err != nil {
		return LoopRevision{}, LoopValidationResult{}, err
	}
	r.SchemaVersion = ContributionRevisionSchemaVersion
	r.Contribution = &profile
	// Historical v6 profiles keep their canonical zero-input definition.
	// Engineering successors bind one public datum to the sealed intake.
	if profile.Engineering != nil {
		r.Inputs = []Port{{ID: "github_issue_url", Type: "string", Required: true}}
		for i := range r.Steps {
			if r.Steps[i].ID == r.EntryStepID {
				r.Steps[i].InputPorts = append([]Port(nil), r.Inputs...)
			}
		}
	}
	r.Digest = ""
	return NewRevision(r)
}

// RoleModelLane selects an optional (provider, model) lane from the bound
// Agent Charter's authorized Hermes envelope for a Contribution role or phase.
type RoleModelLane struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
}

func (l RoleModelLane) Validate() error {
	if !validLaneValue(l.Model) || (l.Provider != "" && !validLaneValue(l.Provider)) {
		return errors.New("explicit model lane required")
	}
	return nil
}

// ContributionProfile is an independently published, exact local implementation
// profile. It does not grant runtime, host, GitHub write or merge authority.
// GitHub-effect profiles are deliberately not accepted by this local executor.
type ContributionProfile struct {
	Engineering       *IssueEngineeringPolicy      `json:"engineering,omitempty"`
	FinalVerification string                       `json:"final_verification,omitempty"`
	Managed           *ContributionExecutionPolicy `json:"managed,omitempty"`
	PrepareLane       *RoleModelLane               `json:"prepare_lane,omitempty"`
	ControllerLane    *RoleModelLane               `json:"controller_lane,omitempty"`
	BuilderLane       *RoleModelLane               `json:"builder_lane,omitempty"`
	TesterLane        *RoleModelLane               `json:"tester_lane,omitempty"`
	Controller        reference.RevisionRef        `json:"controller"`
	Builder           reference.RevisionRef        `json:"builder"`
	Tester            reference.RevisionRef        `json:"tester"`
	Repository        string                       `json:"repository"`
	RepositoryID      uint64                       `json:"repository_id"`
	Base              string                       `json:"base"`
	ReadAction        string                       `json:"read_action"`
	Effect            string                       `json:"effect"`
	DeadlineSeconds   uint32                       `json:"deadline_seconds"`
	Invariants        []string                     `json:"invariants"`
	Exclusions        []string                     `json:"exclusions"`
}

func (p ContributionProfile) RoleLane(role string) (provider, model string) {
	switch role {
	case "prepare":
		if p.PrepareLane != nil {
			return p.PrepareLane.Provider, p.PrepareLane.Model
		}
		if p.ControllerLane != nil {
			return p.ControllerLane.Provider, p.ControllerLane.Model
		}
	case "controller":
		if p.ControllerLane != nil {
			return p.ControllerLane.Provider, p.ControllerLane.Model
		}
	case "builder":
		if p.BuilderLane != nil {
			return p.BuilderLane.Provider, p.BuilderLane.Model
		}
	case "tester":
		if p.TesterLane != nil {
			return p.TesterLane.Provider, p.TesterLane.Model
		}
	}
	return "", ""
}

func (p ContributionProfile) Validate() error {
	if p.Engineering != nil {
		if p.Engineering.Validate() != nil || p.Managed == nil || p.Managed.Kind != EngineeringExecutionKind || p.Managed.ReviewMode != "issue-journeys.v1" || p.FinalVerification != "aegis.contribution.final.v1" {
			return errors.New("qualified engineering execution and final verification required")
		}
	}
	if p.FinalVerification != "" && p.FinalVerification != "aegis.contribution.final.v1" {
		return errors.New("unknown contribution final verification policy")
	}
	if p.Managed != nil {
		if err := p.Managed.Validate(); err != nil {
			return err
		}
	}
	for _, lane := range []*RoleModelLane{p.PrepareLane, p.ControllerLane, p.BuilderLane, p.TesterLane} {
		if lane != nil {
			if err := lane.Validate(); err != nil {
				return err
			}
		}
	}
	if p.Controller.Validate() != nil || p.Builder.Validate() != nil || p.Tester.Validate() != nil || p.RepositoryID == 0 || p.ReadAction != "github.read_contribution_issue.v1" || !validContributionDelivery(p.Effect) || p.DeadlineSeconds < 1 || p.DeadlineSeconds > 86400 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`).MatchString(p.Repository) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(p.Base) || len(p.Invariants) == 0 || len(p.Invariants) > 32 || len(p.Exclusions) == 0 || len(p.Exclusions) > 32 {
		return errors.New("exact contribution profile required")
	}
	for _, list := range [][]string{p.Invariants, p.Exclusions} {
		for _, s := range list {
			if s == "" || len(s) > 4096 {
				return errors.New("bounded contribution invariants required")
			}
		}
	}
	return nil
}

// validContributionInputs retains immutable historical v6 acceptance, while
// the additive URL-only form is closed to Engineering and cannot accept scope,
// workspace, authority or executable data through a Graph adapter.
func validContributionInputs(r LoopRevision) bool {
	if len(r.Inputs) == 0 {
		return true
	}
	if r.Contribution == nil || r.Contribution.Engineering == nil || len(r.Inputs) != 1 {
		return false
	}
	p := r.Inputs[0]
	if p.ID != "github_issue_url" || p.Type != "string" || !p.Required {
		return false
	}
	for _, step := range r.Steps {
		if step.ID == r.EntryStepID {
			return len(step.InputPorts) == 1 && step.InputPorts[0] == p
		}
	}
	return false
}

func IsImplementationSchema(s string) bool {
	return s == ImplementationRevisionSchemaVersion || s == ContributionRevisionSchemaVersion
}

// Keep the Loop profile pure: adapters and sealed intake bind outward to it.
func validContributionDelivery(effect string) bool {
	switch effect {
	case "", "verified-protected-base-merge", "no-merge", "no-publish", "local-only":
		return true
	default:
		return false
	}
}
