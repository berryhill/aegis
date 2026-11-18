package badger

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func recoveryDecisionFixture(t *testing.T, s *Store, c contribution.Contract) contribution.Decision {
	t.Helper()
	ctx := context.Background()
	event, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: 1, Kind: "initialize", At: c.AdmittedAt, Limits: contribution.Counters{Candidates: 1, QueueAttempts: 1, RuntimeTurns: 2, DecisionDeliveries: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendContributionContinuity(ctx, event); err != nil {
		t.Fatal(err)
	}
	control, err := s.GetContributionControl(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	role, ok := contribution.SealedRole(c, "builder")
	if !ok {
		t.Fatal("role")
	}
	d := contribution.Decision{ID: "decision", Key: c.Key, OwnerID: c.OwnerID, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, ControlDigest: control.Digest, Role: "builder", Binding: role, PlanID: "plan", PlanDigest: deliveryTestDigest("plan"), RequestedAt: c.AdmittedAt, ExpiresAt: c.Deadline, Status: "requested"}
	if err = s.ReserveContributionDecision(ctx, d); err != nil {
		t.Fatal(err)
	}
	return d
}

func recoveryDeliveryFixture(t *testing.T, decisionFirst bool) (*Store, contribution.Contract, delivery.Intent, contribution.Decision) {
	t.Helper()
	return recoveryDeliveryFixtureStore(t, newDeliveryStore(t), decisionFirst)
}

func recoveryDeliveryFixtureStore(t *testing.T, store *Store, decisionFirst bool) (*Store, contribution.Contract, delivery.Intent, contribution.Decision) {
	t.Helper()
	s, c := recoveryContributionFixtureStore(t, store)
	ctx := context.Background()
	writeRecoverySharedPreferences(t, s, c)
	var d contribution.Decision
	if decisionFirst {
		d = recoveryDecisionFixture(t, s, c)
	}
	b := ContributionDeliveryBinding{QueueItemID: c.QueueItemID, OwnerID: c.OwnerID, Key: c.Key, Reference: delivery.Reference{Run: c.RunID, Contract: c.Digest, Candidate: deliveryTestDigest("candidate")}, FenceEpoch: 1, RoleBinding: deliveryTestDigest("role"), Base: "main"}
	i := delivery.Intent{Reference: b.Reference, FenceEpoch: 1, RoleBinding: b.RoleBinding, Operation: delivery.Commit, Actor: "fixture", Repository: c.Repository, RepositoryID: c.Source.RepositoryID, Base: b.Base, Branch: "contribution/test", Head: strings.Repeat("a", 40), BaseHead: c.Base, Policy: "policy", Payload: deliveryTestDigest("payload")}
	i.ID = deliveryTestSlot(i)
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, fresh, err := r.Reserve(ctx, i)
	if err != nil || !fresh {
		t.Fatalf("reserve %v %v", fresh, err)
	}
	if !decisionFirst {
		d = recoveryDecisionFixture(t, s, c)
	}
	checkPresentationRecovery(t, s, true)
	if _, err = r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Confirmed, Head: i.Head}); err != nil {
		t.Fatal(err)
	}
	next := d
	next.Status = "unknown"
	if err = s.UpdateContributionDecision(ctx, d, next); err != nil {
		t.Fatal(err)
	}
	d = next
	control, err := s.GetContributionControl(ctx, c.QueueItemID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "stop", Sequence: control.Sequence + 1, PreviousDigest: control.Digest, Stop: true, Delivery: "local-only", At: c.AdmittedAt}); err != nil {
		t.Fatal(err)
	}
	checkPresentationRecovery(t, s, true)
	return s, c, i, d
}

func TestRecoveryDeliveryDecisionNormalHistories(t *testing.T) {
	for _, first := range []bool{false, true} {
		name := "delivery-first"
		if first {
			name = "decision-first"
		}
		t.Run(name, func(t *testing.T) {
			s, _, _, _ := recoveryDeliveryFixture(t, first)
			root := s.root
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			checkPresentationRecovery(t, reopened, true)
		})
	}
}

func TestRecoveryDeliveryPublicationCorruption(t *testing.T) {
	for _, mode := range []string{"missing-head", "missing-history", "missing-root", "missing-active", "missing-index", "missing-local-slot", "missing-mirror", "missing-both-slots", "extra-history-key", "extra-root-key", "extra-slot-key", "released-without-evidence", "decision-key", "decision-reservation", "decision-status", "decision-role", "decision-stage", "decision-control", "head-rollback", "unknown-field"} {
		t.Run(mode, func(t *testing.T) {
			s, c, i, d := recoveryDeliveryFixture(t, true)
			err := s.update(context.Background(), func(tx *badgerdb.Txn) error {
				head := key(familyContributionDelivery, i.ID, "head")
				root := key(familyContributionPublication, "root", c.OwnerID, c.Key)
				local := key(familyContributionPublication, "slots", c.OwnerID, c.Key, i.ID)
				mirror := key(familyContributionPublication, "7", "main", "slot", i.ID)
				switch mode {
				case "missing-head":
					return tx.Delete(head)
				case "missing-history":
					return tx.Delete(key(familyContributionDelivery, i.ID, revisionPart(1)))
				case "missing-root":
					return tx.Delete(root)
				case "missing-active":
					return tx.Delete(key(familyContributionPublication, "7", "main"))
				case "missing-index":
					return tx.Delete(key(familyContributionPublication, "indexed", c.OwnerID, c.Key))
				case "missing-local-slot":
					return tx.Delete(local)
				case "missing-mirror":
					return tx.Delete(mirror)
				case "missing-both-slots":
					if e := tx.Delete(local); e != nil {
						return e
					}
					return tx.Delete(mirror)
				case "extra-history-key":
					b, e := get(tx, head)
					if e != nil {
						return e
					}
					return tx.Set(key(familyContributionDelivery, i.ID, revisionPart(3)), b)
				case "extra-root-key":
					b, e := get(tx, root)
					if e != nil {
						return e
					}
					return tx.Set(key(familyContributionPublication, "root", c.OwnerID, "wrong"), b)
				case "extra-slot-key":
					return tx.Set(key(familyContributionPublication, "slots", c.OwnerID, c.Key, "wrong"), deliveryWire(i))
				case "released-without-evidence":
					b, e := get(tx, root)
					if e != nil {
						return e
					}
					return tx.Set(key(familyContributionPublication, "released", c.OwnerID, c.Key), b)
				case "decision-key":
					return tx.Set(key(familyContributionDecision, c.QueueItemID, "wrong"), deliveryWire(d))
				case "decision-reservation":
					return tx.Delete(key(familyContributionContinuity, c.QueueItemID, "2"))
				case "decision-status":
					d.Status = "invented"
				case "decision-role":
					d.Role = "foreign"
				case "decision-stage":
					d.Stage = "foreign"
				case "decision-control":
					d.ControlDigest = deliveryTestDigest("foreign")
				case "head-rollback":
					b, e := get(tx, key(familyContributionDelivery, i.ID, revisionPart(1)))
					if e != nil {
						return e
					}
					return tx.Set(head, b)
				case "unknown-field":
					b, e := get(tx, root)
					if e != nil {
						return e
					}
					return tx.Set(root, append(b[:len(b)-1], []byte(",\"unknown\":true}")...))
				}
				return tx.Set(key(familyContributionDecision, c.QueueItemID, d.ID), deliveryWire(d))
			})
			if err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, false)
		})
	}
}
