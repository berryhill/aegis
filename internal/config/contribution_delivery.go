package config

import (
	"errors"
	"strings"

	"github.com/berryhill/aegis/internal/credentials"
)

// ContributionDelivery is trusted operational configuration, never a manager or
// model request. Nil disables composition. It contains no secret or binding grant.
type ContributionDelivery struct {
	Convention        string                  `mapstructure:"convention" json:"convention" yaml:"convention"`
	Actor             string                  `mapstructure:"actor" json:"actor" yaml:"actor"`
	Author            ContributionGitIdentity `mapstructure:"author" json:"author" yaml:"author"`
	Committer         ContributionGitIdentity `mapstructure:"committer" json:"committer" yaml:"committer"`
	RequiredChecks    []string                `mapstructure:"required_checks" json:"required_checks" yaml:"required_checks"`
	RequiredReviewers []string                `mapstructure:"required_reviewers" json:"required_reviewers" yaml:"required_reviewers"`
	PostMergeChecks   []string                `mapstructure:"post_merge_checks" json:"post_merge_checks" yaml:"post_merge_checks"`
	MergeMethod       string                  `mapstructure:"merge_method" json:"merge_method" yaml:"merge_method"`
	ProtectionPolicy  string                  `mapstructure:"protection_policy" json:"protection_policy" yaml:"protection_policy"`
	// CredentialBinding is an exact existing authority key, not a copy of its grant.
	// Omission permits local construction only; it never grants remote access.
	CredentialBinding *ContributionCredentialReference `mapstructure:"credential_binding" json:"credential_binding,omitempty" yaml:"credential_binding,omitempty"`
}

type ContributionGitIdentity struct {
	Name  string `mapstructure:"name" json:"name" yaml:"name"`
	Email string `mapstructure:"email" json:"email" yaml:"email"`
}

type ContributionCredentialReference struct {
	DeploymentID string `mapstructure:"deployment_id" json:"deployment_id" yaml:"deployment_id"`
	AgentID      string `mapstructure:"agent_id" json:"agent_id" yaml:"agent_id"`
	StanzaID     string `mapstructure:"stanza_id" json:"stanza_id" yaml:"stanza_id"`
	Scope        string `mapstructure:"scope" json:"scope" yaml:"scope"`
}

func (r ContributionCredentialReference) Key() credentials.CredentialBindingKey {
	return credentials.CredentialBindingKey{DeploymentID: r.DeploymentID, AgentID: r.AgentID, StanzaID: r.StanzaID, Scope: r.Scope}
}

func (p *ContributionDelivery) Validate() error {
	if p == nil {
		return nil
	}
	deny := errors.New("invalid contribution delivery policy")
	text := func(s string) bool {
		return s != "" && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
	}
	identity := func(i ContributionGitIdentity) bool {
		return text(i.Name) && text(i.Email) && strings.Contains(i.Email, "@") && !strings.ContainsAny(i.Name+i.Email, "<>")
	}
	if p.Convention != "contribution-publication.v1" || !text(p.Actor) || !identity(p.Author) || !identity(p.Committer) || !text(p.ProtectionPolicy) {
		return deny
	}
	if p.MergeMethod != "merge" && p.MergeMethod != "squash" && p.MergeMethod != "rebase" {
		return deny
	}
	for _, list := range [][]string{p.RequiredChecks, p.RequiredReviewers, p.PostMergeChecks} {
		if len(list) == 0 {
			return deny
		}
		seen := map[string]bool{}
		for _, v := range list {
			if !text(v) || seen[v] {
				return deny
			}
			seen[v] = true
		}
	}
	if p.CredentialBinding != nil {
		if err := p.CredentialBinding.Key().Validate(); err != nil {
			return deny
		}
	}
	return nil
}
