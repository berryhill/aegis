package api

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCodingRequeueStageRefreshesRealDeadlineWithoutExtendingTTL(t *testing.T) {
	s := apiService(t)
	ttl := s.Config.Principal.AuthTTL
	clock := newRequeueStageClock(time.Now().UTC().Add(-2 * ttl))
	s.Now = clock.Now
	old, err := s.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	// A frozen service clock alone cannot make an absolute claim deadline live.
	expired, cancelExpired := context.WithDeadline(context.Background(), old.ExpiresAt)
	defer cancelExpired()
	if expired.Err() != context.DeadlineExceeded {
		t.Fatal("negative control did not reproduce expired wall deadline")
	}
	fresh := beginCodingRequeueStage(t, s, clock)
	if s.Config.Principal.AuthTTL != ttl || fresh.ExpiresAt.Sub(fresh.AuthenticatedAt) != ttl || fresh.PrincipalID != old.PrincipalID || fresh.Method != "local-os" {
		t.Fatal("stage refresh changed authentication policy or identity")
	}
	if err := s.RequirePrincipalIdentity(old); err == nil {
		t.Fatal("expired old subject accepted after stage boundary")
	}
	if err := s.RequirePrincipalIdentity(fresh); err != nil {
		t.Fatal("fresh real Unix subject denied", err)
	}
	live, cancelLive := context.WithDeadline(context.Background(), fresh.ExpiresAt)
	defer cancelLive()
	if live.Err() != nil {
		t.Fatal("new stage inherited expired wall deadline", live.Err())
	}
	// Stage time is immutable during execution, not a moving clock or TTL renewal.
	if !s.Now().Equal(fresh.AuthenticatedAt) || !clock.Now().Equal(fresh.AuthenticatedAt) {
		t.Fatal("clock consumers diverged")
	}
}
