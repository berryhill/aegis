package app

import "testing"

func TestContributionIssueRecoveryPolicyOnlyControllerProvenanceMayChange(t *testing.T) {
	original := ContributionDeliveryPolicy{ControllerSession: "original", Actor: "actor", Branch: "contribution/one", RequiredChecks: []string{"test"}, AllowedFiles: []string{"value.go"}}
	original.Binding.FenceEpoch = 1
	original.Binding.RoleBinding = "original-role"
	current := original.snapshot()
	current.ControllerSession = "renewed"
	current.Binding.FenceEpoch = 2
	current.Binding.RoleBinding = "renewed-role"
	if !compatibleIssueRecoveryPolicy(original, current) {
		t.Fatal("compatible controller continuation denied")
	}
	for name, mutate := range map[string]func(*ContributionDeliveryPolicy){
		"actor":     func(p *ContributionDeliveryPolicy) { p.Actor = "foreign" },
		"branch":    func(p *ContributionDeliveryPolicy) { p.Branch = "different" },
		"checks":    func(p *ContributionDeliveryPolicy) { p.RequiredChecks = []string{"different"} },
		"scope":     func(p *ContributionDeliveryPolicy) { p.AllowedFiles = append(p.AllowedFiles, "other.go") },
		"candidate": func(p *ContributionDeliveryPolicy) { p.Binding.Candidate = "different" },
		"owner":     func(p *ContributionDeliveryPolicy) { p.Binding.OwnerID = "foreign" },
		"preimage":  func(p *ContributionDeliveryPolicy) { p.ExpectedOld = "different" },
		"payload":   func(p *ContributionDeliveryPolicy) { p.PRBody = "different" },
		"source":    func(p *ContributionDeliveryPolicy) { p.Source = "different" },
		"credential": func(p *ContributionDeliveryPolicy) {
			p.CredentialPin = &ContributionDeliveryCredentialPin{Actor: "other"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := current.snapshot()
			mutate(&changed)
			if compatibleIssueRecoveryPolicy(original, changed) {
				t.Fatal("changed authority/payload policy accepted as recovery")
			}
		})
	}
}
