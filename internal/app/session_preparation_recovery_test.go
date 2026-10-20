package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestLegacySessionPreparationProofRequiresExactReproducibleMandate(t *testing.T) {
	for _, mode := range []string{"empty-slice-defect", "valid-mandate", "unrelated-drift", "ambiguous", "outside-interval"} {
		t.Run(mode, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			sub, err := s.Authenticate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			c := testCharter(s.Now())
			c.Stanzas = c.Stanzas[:1]
			c.Stanzas[0].Grant = core.Grant{Capabilities: []string{}, Tools: []string{}}
			c.Stanzas[0].Scopes = core.Scopes{Memory: []string{}, Credentials: []string{}}
			c.Stanzas[0].Hermes = core.HermesConfig{Model: "proof-no-key", Provider: "none"}
			raw, _ := json.Marshal(c)
			canonical, err := s.ImportCharterAs(ctx, sub, raw)
			if err != nil {
				t.Fatal(err)
			}
			m, _, err := s.previewSessionAs(ctx, sub, c.AgentID, 1, "", core.Environment{Name: "local"}, true)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.validateMandate(m); err != nil {
				t.Fatal("new issuer corrupted exact empties", err)
			}
			m.Capabilities = nil
			m.Tools = nil
			m.ID = "legacy-fixture"
			if mode == "valid-mandate" {
				m.Capabilities = []string{}
				m.Tools = []string{}
			}
			if mode == "unrelated-drift" {
				m.Hermes.Model = "different"
			}
			// Original valid preview is outside the legacy failure interval. Neither
			// stored record is rewritten; the old invalid issuer record is append-only.
			start := s.Now().Add(time.Second)
			m.IssuedAt = start
			m.ExpiresAt = start.Add(30 * time.Second)
			if mode == "outside-interval" {
				m.IssuedAt = start.Add(-2 * time.Second)
			}
			if err = s.Authority.CreateMandate(ctx, m); err != nil {
				t.Fatal(err)
			}
			if mode == "ambiguous" {
				duplicate := m
				duplicate.ID = "other-legacy"
				if err = s.Authority.CreateMandate(ctx, duplicate); err != nil {
					t.Fatal(err)
				}
			}
			agent := registry.AgentRevision{AgentID: c.AgentID, Charter: reference.RevisionRef{ID: c.AgentID, Revision: 1, Digest: canonical.Digest}}
			v := QueueExecutionView{Item: queue.Item{EnqueuedAt: start}}
			tr := queue.QueueTransition{TransitionID: "failed-transition", Digest: "fixture-digest", OccurredAt: start.Add(time.Second)}
			in := QueueLoopInput{Agent: reference.RevisionRef{ID: c.AgentID, Revision: 1, Digest: "agent"}, Loop: reference.RevisionRef{Digest: "loop"}}
			proof, err := s.legacyPreparationFailure(ctx, sub, in, QueueLoopResult{RequestID: "request", QueueItemID: "queue"}, "reservation", agent, v, tr)
			if mode == "empty-slice-defect" {
				if err != nil || proof.Stage != "legacy_mandate_empty_slice_mismatch" || proof.MandateID != m.ID || proof.Digest != preparationFactDigest(proof) {
					t.Fatalf("proof: %+v %v", proof, err)
				}
				s.Now = func() time.Time { return start }
				_, err = s.StartSessionAs(ctx, sub, m.ID)
				var failure *sessionStartFailure
				if !errors.As(err, &failure) || failure.Stage != "mandate_validation" || !failure.RetrySafe || failure.AuthorityID != "" {
					t.Fatalf("wrong boundary: %v", err)
				}
				contexts, e := s.Authority.ListAuthorityContexts(ctx)
				if e != nil || len(contexts) != 0 {
					t.Fatal("invalid mandate crossed launch boundary", e)
				}
			} else if err == nil {
				t.Fatal("unknown or ambiguous failure was called retry-safe")
			}
		})
	}
}

func TestPreparationReservationsRetainOriginalAndBoundGeneration(t *testing.T) {
	s := testService(t)
	root := "root"
	f := map[string]string{"queue_item_id": "queue", "agent_digest": "agent"}
	if err := s.Store.Create("queue-session-preparation", root, f); err != nil {
		t.Fatal(err)
	}
	next := preparationNextID(root)
	child := map[string]string{"queue_item_id": "queue", "agent_digest": "agent", "parent_reservation": root}
	if err := s.Store.Create("queue-session-preparation", next, child); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Create("queue-session-preparation", next, child); err == nil {
		t.Fatal("duplicate winner")
	}
	got, _, err := s.preparationReservation(root)
	if err != nil || got != next {
		t.Fatal("did not resolve child", err)
	}
	var retained map[string]string
	if err = s.Store.Load("queue-session-preparation", root, &retained); err != nil || retained["queue_item_id"] != "queue" {
		t.Fatal("original fence lost")
	}
	if _, _, err = s.preparationReservation("missing"); !os.IsNotExist(err) {
		t.Fatal("missing != unavailable", err)
	}
	secret := errors.New("synthetic-private-error-not-for-output")
	if strings.Contains((&sessionStartFailure{Stage: "runtime_launch", cause: secret}).Error(), secret.Error()) {
		t.Fatal("raw error disclosed")
	}
}
