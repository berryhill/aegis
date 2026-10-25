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
	r.Digest = ""
	return NewRevision(r)
}

// ContributionProfile is an independently published, exact local implementation
// profile. It does not grant runtime, host, GitHub write or merge authority.
// GitHub-effect profiles are deliberately not accepted by this local executor.
type ContributionProfile struct {
	Managed         *ContributionExecutionPolicy `json:"managed,omitempty"`
	Controller      reference.RevisionRef        `json:"controller"`
	Builder         reference.RevisionRef        `json:"builder"`
	Tester          reference.RevisionRef        `json:"tester"`
	Repository      string                       `json:"repository"`
	RepositoryID    uint64                       `json:"repository_id"`
	Base            string                       `json:"base"`
	ReadAction      string                       `json:"read_action"`
	Effect          string                       `json:"effect"`
	DeadlineSeconds uint32                       `json:"deadline_seconds"`
	Invariants      []string                     `json:"invariants"`
	Exclusions      []string                     `json:"exclusions"`
}

func (p ContributionProfile) Validate() error {
	if p.Managed != nil {
		if err := p.Managed.Validate(); err != nil {
			return err
		}
	}
	if p.Controller.Validate() != nil || p.Builder.Validate() != nil || p.Tester.Validate() != nil || p.Builder.ID == p.Tester.ID || p.Controller.ID == p.Builder.ID || p.Controller.ID == p.Tester.ID || p.RepositoryID == 0 || p.ReadAction != "github.read_contribution_issue.v1" || !validContributionDelivery(p.Effect) || p.DeadlineSeconds < 1 || p.DeadlineSeconds > 86400 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`).MatchString(p.Repository) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(p.Base) || len(p.Invariants) == 0 || len(p.Invariants) > 32 || len(p.Exclusions) == 0 || len(p.Exclusions) > 32 {
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
