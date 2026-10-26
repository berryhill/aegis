//go:build !linux

package hermes

import "context"

func (a *Adapter) ContributionProcessIdentity(string, int, string) (string, uint32, error) {
	return "", 0, ErrContributionStopUnavailable
}
func (a *Adapter) RecoverContributionRole(context.Context, ContributionStopBinding, string, uint32) (ContributionStopProof, error) {
	return ContributionStopProof{}, ErrContributionStopUnavailable
}
