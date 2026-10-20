package app

import (
	"context"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"testing"
	"time"
)

func TestControllerCodexConnectionPrincipalDenials(t *testing.T) {
	now := time.Now()
	cfg := config.Defaults()
	cfg.Principal.ID = "owner"
	cfg.Principal.UID = "1000"
	s := &Service{Config: cfg, Now: func() time.Time { return now }}
	valid := core.Subject{ID: "local-uid:1000", Kind: "human", PrincipalID: "owner", Issuer: "linux-so-peercred", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute), Claims: map[string]string{"uid": "1000"}}
	for _, kind := range []string{"expired", "foreign", "workspace", "unknown-issuer", "future", "stale", "unknown-review"} {
		t.Run(kind, func(t *testing.T) {
			sub := valid
			switch kind {
			case "expired":
				sub.ExpiresAt = now
			case "foreign":
				sub.PrincipalID = "other"
			case "workspace":
				sub.Kind = "agent"
			case "unknown-issuer":
				sub.Issuer = "caller"
			case "future":
				sub.AuthenticatedAt = now.Add(time.Second)
			case "stale":
				sub.AuthenticatedAt = now.Add(-time.Hour)
			}
			if _, e := s.PreviewControllerCodexAs(context.Background(), sub, ControllerCodexPreviewInput{ProfileHome: "/must/not/read"}); e == nil {
				t.Fatal("unauthorized preview admitted")
			}
			if _, e := s.InspectControllerCodexCandidatesAs(context.Background(), sub, ControllerCodexCandidatesInput{ProfileHome: "/must/not/read"}); e == nil {
				t.Fatal("unauthorized inspection admitted")
			}
			if _, e := s.ApplyControllerCodexAs(context.Background(), sub, ControllerCodexApplyInput{ReviewID: "unknown"}); e == nil {
				t.Fatal("unauthorized application admitted")
			}
		})
	}
}
