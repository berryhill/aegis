package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestAgentModelAddExactPermissionAndDenials(t *testing.T) {
	for _, mode := range []string{"approve", "peer", "foreign", "expired", "revoked", "drift", "tamper", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, peer, input, old := loopModelEditFixture(t)
			ctx := context.Background()
			human := core.Subject{ID: "password:operator", Kind: "principal", PrincipalID: peer.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: peer.ExpiresAt}
			s.DashboardActionAuthentication = func(got core.Subject) error {
				if !reflect.DeepEqual(got, human) {
					return ErrDenied
				}
				return nil
			}
			selection := AgentModelAddInput{AgentID: input.AgentID, Provider: "openai-codex", Model: "gpt-6.2-sol"}
			if _, err := s.PrepareAgentModelAddAs(ctx, peer, selection); !errors.Is(err, ErrDenied) {
				t.Fatal("peer review admitted", err)
			}
			review, err := s.PrepareAgentModelAddAs(ctx, human, selection)
			if err != nil {
				t.Fatal(err)
			}
			before := review.Original.Charter
			who := human
			switch mode {
			case "peer":
				who = peer
			case "foreign":
				who.PrincipalID = "foreign"
			case "expired":
				now := s.Now()
				s.Now = func() time.Time { return now.Add(4 * time.Minute) }
			case "revoked":
				s.DashboardActionAuthentication = func(core.Subject) error { return ErrDenied }
			case "drift":
				review.Expected.Revision++
			case "tamper":
				review.Input.Model = "unrequested-model"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := s.ConfirmAgentModelAddAs(ctx, who, review)
			if mode != "approve" {
				if err == nil {
					t.Fatal("invalid approval accepted")
				}
				charters, e := s.ListCharters(input.AgentID)
				if e != nil || len(charters) != 1 {
					t.Fatal("denial changed charters", e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := s.GetCharter(result.Revision.Charter.ID, result.Revision.Charter.Revision)
			if err != nil {
				t.Fatal(err)
			}
			// Remove precisely the authorized structural delta; everything else must match.
			got := after.Charter
			h := &got.Stanzas[0].Hermes
			if len(h.AllowedLanes) != 2 || h.AllowedLanes[1].Provider != selection.Provider || h.AllowedLanes[1].Model != selection.Model || h.AllowedLanes[1].ProviderAuthentication.Mode != core.ControllerCodexAccessTokenV1 {
				t.Fatal("lane mismatch")
			}
			h.AllowedLanes = nil
			got.Revision = before.Revision
			if !reflect.DeepEqual(got, before) {
				t.Fatal("unrequested charter change")
			}
			if result.Revision.CharterSuccessor == nil || result.Revision.CharterSuccessor.Previous != review.Expected {
				t.Fatal("approval provenance missing")
			}
			loops, e := s.ListLoopsAs(ctx, peer)
			if e != nil || len(loops) != 1 {
				t.Fatal("model addition created Loop", e)
			}
			retained, e := s.GetLoopAs(ctx, peer, old.Revision.LoopID, 1)
			if e != nil || retained.Digest != old.Revision.Digest {
				t.Fatal("history changed")
			}
			queue, e := s.FleetRepository.ListQueueItems(ctx)
			if e != nil || len(queue) != 0 {
				t.Fatal("execution effect")
			}
			if _, err = s.ConfirmAgentModelAddAs(ctx, human, review); err == nil {
				t.Fatal("replay approved twice")
			}
		})
	}
}
