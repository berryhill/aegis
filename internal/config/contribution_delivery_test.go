package config

import (
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestContributionDeliveryTrustedPolicy(t *testing.T) {
	valid := ContributionDelivery{Convention: "contribution-publication.v1", Actor: "controller", Author: ContributionGitIdentity{Name: "Author", Email: "author@example.invalid"}, Committer: ContributionGitIdentity{Name: "Committer", Email: "committer@example.invalid"}, RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash", ProtectionPolicy: "exact-protection"}
	b, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var got ContributionDelivery
	if err = json.Unmarshal(b, &got); err != nil || got.Validate() != nil {
		t.Fatalf("typed roundtrip: %v", err)
	}
	for name, mutate := range map[string]func(*ContributionDelivery){
		"actor": func(p *ContributionDelivery) { p.Actor = "" }, "author": func(p *ContributionDelivery) { p.Author.Email = "" }, "committer": func(p *ContributionDelivery) { p.Committer.Name = "" }, "checks": func(p *ContributionDelivery) { p.RequiredChecks = nil }, "review": func(p *ContributionDelivery) { p.RequiredReviewers = nil }, "postmerge": func(p *ContributionDelivery) { p.PostMergeChecks = nil }, "duplicate": func(p *ContributionDelivery) { p.RequiredChecks = []string{"ci", "ci"} }, "method": func(p *ContributionDelivery) { p.MergeMethod = "force" }, "protection": func(p *ContributionDelivery) { p.ProtectionPolicy = "" }, "binding": func(p *ContributionDelivery) { p.CredentialBinding = &ContributionCredentialReference{} },
	} {
		t.Run(name, func(t *testing.T) {
			p := valid
			mutate(&p)
			if p.Validate() == nil {
				t.Fatal("accepted malformed explicit policy")
			}
		})
	}
	for _, text := range []string{string(b[:len(b)-1]) + `,"secret":"forbidden"}`, "actor: one\nactor: two\n"} {
		decoder := yaml.NewDecoder(strings.NewReader(text))
		decoder.KnownFields(true)
		var p ContributionDelivery
		if decoder.Decode(&p) == nil {
			t.Fatal("accepted unknown or duplicate field")
		}
	}
	var absent *ContributionDelivery
	if absent.Validate() != nil {
		t.Fatal("absence must remain disabled")
	}
}
