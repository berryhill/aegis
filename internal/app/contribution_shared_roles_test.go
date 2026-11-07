package app

import (
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

// Real role resolver, clean runtime launch and admission; synthetic Hermes, not provider proof.
func TestContributionSharedRoleAgentAdmission(t *testing.T) {
	for _, roles := range [][3]int{{0, 0, 0}, {0, 0, 2}, {0, 1, 0}, {0, 1, 1}, {0, 1, 2}} {
		t.Run(fmt.Sprint(roles), func(t *testing.T) { testContributionSharedRoleAgentAdmission(t, roles) })
	}
}
func testContributionSharedRoleAgentAdmission(t *testing.T, roles [3]int) {
	ctx := context.Background()
	s, _, sub, ref, reader := contributionAuthorityFixtureRoles(t, roles, func(c *core.Charter) {
		for i := range c.Stanzas {
			c.Stanzas[i].Grant = core.Grant{Capabilities: []string{}, Tools: []string{}}
			c.Stanzas[i].Scopes.Credentials = []string{}
			c.Stanzas[i].Hermes = core.HermesConfig{Model: "synthetic-protocol", Provider: "none"}
		}
	})
	c, err := s.admitContributionAs(ctx, sub, "original", ref, reader.source.URL.URL, "shared-agent", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetLoopViewAs(ctx, sub, ref.ID, ref.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		for _, mutation := range []string{"revision", "digest", "unregistered"} {
			candidate := view.Revision
			profile := *candidate.Contribution
			candidate.Contribution = &profile
			role := []*reference.RevisionRef{&profile.Controller, &profile.Builder, &profile.Tester}[i]
			switch mutation {
			case "revision":
				role.Revision++
			case "digest":
				role.Digest = "sha256:" + strings.Repeat("f", 64)
			case "unregistered":
				role.ID = "not-registered"
			}
			candidate, _, err = loop.NewRevision(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.validateContributionAgents(ctx, sub, candidate); err == nil {
				t.Fatal("substituted exact role admitted", i, mutation)
			}
		}
	}
	state, err := s.RecordContributionContinuityAs(ctx, sub, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := s.PreviewSessionAs(ctx, sub, c.Roles[0].Charter.ID, c.Roles[0].Charter.Revision, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSessionAs(ctx, sub, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.TerminateSession(ctx, session.ID, "cleanup") })
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.BindQueueRuntimeAs(ctx, sub, BindQueueRuntimeInput{AgentID: c.Controller.ID, Authority: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}, QueueItemID: c.QueueItemID, BindingID: "shared-binding", TransitionID: "shared-bound"})
	if err != nil {
		t.Fatal(err)
	}
	input := func() ContributionContinuityInput {
		return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}
	}
	state, err = s.AttachContributionObservationAs(ctx, sub, session.ID, c.Key, input())
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.AdmitContributionRoleAs(ctx, sub, session.ID, c.Key, "controller", input())
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[string]string{"controller": session.ID}
	for i, role := range []string{"builder", "tester"} {
		resolved, e := s.resolveContributionRole(ctx, sub, c, i+1)
		if e != nil {
			t.Fatal(role, e)
		}
		sid := resolved.Launch.AuthorityContext.SessionID
		t.Cleanup(func() { _ = resolved.Close(ctx) })
		sessions[role] = sid
		decision, e := resolved.Admission.CheckRuntimeAdmission(ctx, resolved.Launch, s.Now())
		if e != nil || !decision.Allowed {
			t.Fatal(role, "runtime admission", e)
		}
		if resolved.Agent.Digest != c.Roles[i+1].Agent.Digest {
			t.Fatal("role substitution")
		}
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	for i, role := range []string{"controller", "builder", "tester"} {
		got, err := s.CheckContributionRoleAs(ctx, sub, sessions[role], c.Key, role, input())
		if err != nil || got.Binding.Agent != c.Roles[i].Agent || got.SessionID != sessions[role] {
			t.Fatal(role, got, err)
		}
		for other, sid := range sessions {
			if other == role {
				continue
			}
			if _, err := s.CheckContributionRoleAs(ctx, sub, sid, c.Key, role, input()); err == nil {
				t.Fatal("same-session substitution admitted", other, role)
			}
		}
		stale := input()
		stale.ExpectedSequence--
		if _, err = s.CheckContributionRoleAs(ctx, sub, sessions[role], c.Key, role, stale); err == nil {
			t.Fatal("stale role head admitted")
		}
		foreign := sub
		foreign.PrincipalID = "foreign"
		if _, err = s.CheckContributionRoleAs(ctx, foreign, sessions[role], c.Key, role, input()); err == nil {
			t.Fatal("foreign role admitted")
		}
	}
	for _, sid := range sessions {
		if err := s.RevokeSession(ctx, sid, "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"controller", "builder", "tester"} {
		if _, err = s.CheckContributionRoleAs(ctx, sub, sessions[role], c.Key, role, input()); err == nil {
			t.Fatal("revoked role session admitted", role)
		}
	}
}
