package app

import (
	"fmt"
	"slices"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// ContributionIssueResourceCapability names the explicit operation-qualified
// repository grant carried by the existing charter/mandate capability transport.
// Constructing or displaying this string grants nothing. Both this exact value
// and github.issue_disposition.v1 must already exist in ONE admitted authority.
// Repository spelling and immutable numeric ID are retained without normalization.
func ContributionIssueResourceCapability(repository string, repositoryID uint64) string {
	return fmt.Sprintf("%s:repository:%s:%d", delivery.ReconcileIssue, repository, repositoryID)
}

func contributionIssueScopeAllowed(authority core.AuthorityContext, plan delivery.Plan, intent delivery.Intent) bool {
	return intent.Operation == delivery.ReconcileIssue && plan.Repository != "" && plan.RepositoryID != 0 &&
		intent.Repository == plan.Repository && intent.RepositoryID == plan.RepositoryID &&
		hasContributionIssueCapabilities(authority, plan.Repository, plan.RepositoryID)
}

func hasContributionIssueCapabilities(authority core.AuthorityContext, repository string, repositoryID uint64) bool {
	return repository != "" && repositoryID != 0 && slices.Contains(authority.Authority.Capabilities, string(delivery.ReconcileIssue)) &&
		slices.Contains(authority.Authority.Capabilities, ContributionIssueResourceCapability(repository, repositoryID))
}
