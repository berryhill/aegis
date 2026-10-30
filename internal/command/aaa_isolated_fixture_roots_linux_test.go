//go:build linux

package command

import "testing"

// The longest isolated journey must be admitted before serial tests consume
// the package deadline. Its original body still controls all effects/checks.
func TestInstalledUpgradeFullCommand(t *testing.T) {
	installedUpgradeFullCommand(t)
}

func TestRecoveryCLIApprovalRejections(t *testing.T) {
	recoveryCLIApprovalRejections(t)
}
