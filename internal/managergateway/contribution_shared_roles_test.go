package managergateway

import "testing"

// Real manager dispatch, native RED/repair/GREEN and canonical evidence with
// ordinary separate sessions; only Hermes/provider/provisioning are synthetic.
// Every partial sharing combination also traverses real app role resolution and
// runtime admission in TestContributionSharedRoleAgentAdmission. Existing native
// manager regressions retain the distinct-Agent and expired-resume paths.
func TestContributionSharedRoleAgents(t *testing.T) {
	contributionManagerNativeRepairWithRoles(t, [3]int{0, 0, 0})
}
