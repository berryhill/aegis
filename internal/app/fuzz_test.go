package app

import (
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func FuzzStanzaSelector(f *testing.F) {
	f.Add("principal", true)
	f.Add("", false)
	f.Fuzz(func(t *testing.T, requested string, duplicate bool) {
		s := testService(t)
		c := testCharter(s.Now())
		var cc core.CanonicalCharter
		if duplicate {
			c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{PrincipalIDs: []string{"principal-1"}}}
			cc = core.CanonicalCharter{Charter: c, Digest: "sha256:ambiguous-fuzz"}
		} else {
			var err error
			cc, err = core.Canonicalize(c)
			if err != nil {
				return
			}
		}
		sub := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
		d, err := s.Select(cc, sub, requested, core.Environment{Name: "local"})
		if err == nil && (!d.Allowed || d.Selected == nil || d.MatchingCount != 1) {
			t.Fatal("successful selection did not bind exactly one stanza")
		}
		if d.MatchingCount > 1 && err == nil {
			t.Fatal("ambiguous selection allowed")
		}
	})
}
