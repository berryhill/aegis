//go:build !linux

package hermes

import "context"

func (a *Adapter) stopContributionRole(context.Context, ContributionStopBinding) (ContributionStopProof, error) {
	return ContributionStopProof{}, ErrContributionStopUnavailable
}
