package app

import (
	"context"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// Strict short-subject capping is qualified without coupling a 60s issued
// authority to repeated cold native compiler work. The native continuation
// journey separately proves the unchanged claim cap and expired-claim resume.
func TestContributionInitialSubjectCap(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	now := s.Now()
	s.Now = func() time.Time { return now }
	charter := testCharter(now)
	charter.Stanzas[0].Session.MaximumLifetimeSec = 300
	canonical, err := core.Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "short-cap", core.Receipt{ID: "short-cap", CharterDigest: canonical.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	subject, err := s.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !subject.ExpiresAt.Equal(now.Add(5 * time.Minute)) {
		t.Fatal("principal policy changed")
	}
	subject.ExpiresAt = now.Add(time.Minute) // Narrow only this isolated test subject.
	mandate, _, err := s.PreviewSessionAs(ctx, subject, charter.AgentID, 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetMandate(mandate.ID)
	if err != nil || !stored.ExpiresAt.Equal(subject.ExpiresAt) {
		t.Fatal("issued mandate escaped 60s subject cap", err)
	}
	session, err := s.StartSessionAs(ctx, subject, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Hermes.Terminate(context.Background(), session.RuntimeSessionID, false) })
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil || !authority.ExpiresAt.Equal(subject.ExpiresAt) || !session.Mandate.ExpiresAt.Equal(subject.ExpiresAt) {
		t.Fatal("issued session/authority escaped subject cap", err)
	}
	before := core.Digest(authority)
	now = subject.ExpiresAt.Add(time.Second)
	if s.RequirePrincipal(subject) == nil {
		t.Fatal("expired original subject accepted")
	}
	fresh, err := s.Authenticate(ctx)
	if err != nil || !fresh.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatal("fresh authentication changed policy", err)
	}
	decision, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, now)
	if err != nil || decision.Admitted {
		t.Fatal("fresh login revived old expired authority", err)
	}
	again, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil || core.Digest(again) != before || !again.ExpiresAt.Equal(subject.ExpiresAt) {
		t.Fatal("expiry inspection rewrote issued authority", err)
	}
}
