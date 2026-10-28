package app

import (
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestContributionDeliverySelectedCharterApproval(t *testing.T) {
	for _, name := range []string{"selected", "unselected", "effective", "digest", "missing", "disabled", "changed", "revision"} {
		t.Run(name, func(t *testing.T) {
			c := core.CanonicalCharter{Charter: core.Charter{AgentID: "controller", Revision: 1, Stanzas: []core.TrustStanza{{ID: "selected", Enabled: true}, {ID: "other", Enabled: true, Approval: core.ApprovalPolicy{RequiredOperations: []string{string(delivery.Commit)}}}}}}
			ac := core.AuthorityContext{AgentID: "controller", CharterRevision: 1, Authority: core.EffectiveAuthority{StanzaID: "selected"}}
			want := error(nil)
			switch name {
			case "selected":
				c.Charter.Stanzas[0].Approval.RequiredOperations = []string{string(delivery.Commit)}
				want = ErrContributionDeliveryApprovalUnavailable
			case "effective":
				ac.Authority.Approval.RequiredOperations = []string{string(delivery.Commit)}
				want = ErrContributionDeliveryApprovalUnavailable
			case "missing":
				ac.Authority.StanzaID = "absent"
				want = delivery.ErrDenied
			case "disabled":
				c.Charter.Stanzas[0].Enabled = false
				want = delivery.ErrDenied
			}
			c.Digest = core.Digest(c.Charter)
			ac.CharterDigest = c.Digest
			switch name {
			case "digest":
				ac.CharterDigest = "sha256:wrong"
				want = delivery.ErrDenied
			case "changed":
				c.Charter.Stanzas[0].Name = "changed"
				want = delivery.ErrDenied
			case "revision":
				ac.CharterRevision = 2
				want = delivery.ErrDenied
			}
			if err := contributionDeliveryOperationApproval(ac, c, delivery.Commit); !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			if name == "selected" {
				if err := contributionDeliveryOperationApproval(ac, c, delivery.Publish); err != nil {
					t.Fatal("unrelated operation held", err)
				}
			}
		})
	}
}
