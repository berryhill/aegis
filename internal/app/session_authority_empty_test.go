package app

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func TestStartSessionPreservesEmptyCredentialAuthority(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials []string
	}{
		{"nil", nil}, {"explicit-empty", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			charter := testCharter(s.Now())
			charter.Stanzas[0].Scopes.Credentials = tc.credentials
			charter.Stanzas[0].Hermes.Provider = "none"
			canonical, err := core.Canonicalize(charter)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Store.SaveCharter(canonical); err != nil {
				t.Fatal(err)
			}
			if err = s.Store.Save("receipts", "ready", core.Receipt{ID: "ready", CharterDigest: canonical.Digest, Status: "verified"}); err != nil {
				t.Fatal(err)
			}
			mandate, _, err := s.PreviewSession(ctx, "office", 1, "principal", core.Environment{Name: "local"})
			if err != nil {
				t.Fatal(err)
			}
			session, err := s.StartSession(ctx, mandate.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer s.TerminateSession(ctx, session.ID, "test cleanup")
			contexts, err := s.Authority.ListAuthorityContexts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(contexts) != 1 {
				t.Fatalf("contexts = %d", len(contexts))
			}
			authority := contexts[0]
			if err = core.ValidateAuthorityContext(authority, mandate); err != nil {
				t.Fatal(err)
			}
			if core.Digest(authority.Authority.Credentials) != core.Digest(mandate.Scopes.Credentials) {
				t.Fatal("credential representation changed")
			}
			authority.Authority.Credentials = []string{"provider:unauthorized"}
			authority.Digest = core.AuthorityContextDigest(authority)
			if core.ValidateAuthorityContext(authority, mandate) == nil {
				t.Fatal("credential widening accepted")
			}
		})
	}
}
