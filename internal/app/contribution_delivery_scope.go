package app

import (
	"fmt"
	"slices"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// ContributionDeliveryResourceCapability names an operation-qualified repository
// grant in the existing charter capability transport. Formatting grants nothing.
// Operation and resource must both exist in ONE freshly validated authority.
func ContributionDeliveryResourceCapability(op delivery.Operation, repository string, repositoryID uint64) string {
	return fmt.Sprintf("%s:repository:%s:%d", op, repository, repositoryID)
}

func contributionRemoteOperation(op delivery.Operation) bool {
	switch op {
	case delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue:
		return true
	default:
		return false
	}
}

func hasContributionRemoteCapabilities(authority core.AuthorityContext, op delivery.Operation, repository string, repositoryID uint64) bool {
	return contributionRemoteOperation(op) && repository != "" && repositoryID != 0 &&
		slices.Contains(authority.Authority.Capabilities, string(op)) &&
		slices.Contains(authority.Authority.Capabilities, ContributionDeliveryResourceCapability(op, repository, repositoryID))
}

func contributionRemoteScopeAllowed(authority core.AuthorityContext, plan delivery.Plan, intent delivery.Intent) bool {
	return intent.Repository == plan.Repository && intent.RepositoryID == plan.RepositoryID &&
		hasContributionRemoteCapabilities(authority, intent.Operation, plan.Repository, plan.RepositoryID)
}

// Pin only the operations actually authorized by the selected immutable context.
// A usable token, credential reference, task effect or server policy is not a
// grant. Historical operation pins remain independently retained for reads.
func contributionApprovedRemoteOperations(authority core.AuthorityContext, repository string, repositoryID uint64) []delivery.Operation {
	var approved []delivery.Operation
	for _, op := range []delivery.Operation{delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue} {
		if hasContributionRemoteCapabilities(authority, op, repository, repositoryID) {
			approved = append(approved, op)
		}
	}
	return approved
}
