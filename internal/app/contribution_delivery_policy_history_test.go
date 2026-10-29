package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/store"
)

func historyPolicy() ContributionDeliveryPolicy {
	d := contribution.Digest([]byte("original"))
	return ContributionDeliveryPolicy{ControllerSession: "controller", Binding: fleetbadger.ContributionDeliveryBinding{QueueItemID: "queue", OwnerID: "owner", Key: "history", Reference: delivery.Reference{Run: "run", Contract: d, Candidate: d}, FenceEpoch: 3, RoleBinding: d, Base: "main"}, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop", Revision: 1, Digest: d}, Actor: "actor", Author: delivery.Identity{Name: "Author", Email: "a@example.invalid"}, Committer: delivery.Identity{Name: "Committer", Email: "c@example.invalid"}, AllowedFiles: []string{"value.go"}, RequiredChecks: []string{"original-ci"}, RequiredReviewers: []string{"original-reviewer"}, PostMergeChecks: []string{"original-post"}, Tree: "tree", BaseHead: "base", Branch: "feature", ExpectedOld: "old", Timestamp: "timestamp", Message: "message", PR: 19, PRTitle: "title", PRBody: "body", ProtectionPolicy: "original-protection", MergeMethod: "squash"}
}
func TestContributionDeliveryPolicyHistoryReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	blobs, e := store.Open(filepath.Join(root, "canonical"))
	if e != nil {
		t.Fatal(e)
	}
	db, e := fleetbadger.Open(ctx, filepath.Join(root, "fleet-v1"))
	if e != nil {
		t.Fatal(e)
	}
	s := &Service{Store: blobs, FleetRepository: db}
	p := historyPolicy()
	p.CredentialPin = &ContributionDeliveryCredentialPin{Key: credentials.CredentialBindingKey{AgentID: "a", StanzaID: "s", DeploymentID: "d", Scope: "g"}, BindingRevision: 1, SecretRecord: "record", Version: 1, Destination: "github", Actor: "actor", RepositoryID: 7, Operations: []delivery.Operation{delivery.Merge}}
	digest := p.Digest()
	if e = s.retainContributionDeliveryPolicy(ctx, p); e != nil {
		t.Fatal(e)
	}
	wire, _ := json.Marshal(p)
	got, e := blobs.GetBlob(digest)
	if e != nil || string(got) != string(wire) {
		t.Fatal("original bytes not retained", e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	blobs, e = store.Open(filepath.Join(root, "canonical"))
	if e != nil {
		t.Fatal(e)
	}
	db, e = fleetbadger.Open(ctx, filepath.Join(root, "fleet-v1"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s = &Service{Store: blobs, FleetRepository: db}
	h, e := NewContributionDeliveryPolicyHistory(s)
	if e != nil {
		t.Fatal(e)
	}
	current := p.snapshot()
	current.RequiredChecks[0] = "changed-ci"
	current.RequiredReviewers[0] = "changed-reviewer"
	current.PostMergeChecks[0] = "changed-post"
	current.Binding.FenceEpoch++
	if e = s.retainContributionDeliveryPolicy(ctx, current); e != nil {
		t.Fatal(e)
	}
	old, e := h.ResolveContributionDeliveryPolicyDigest(ctx, digest)
	if e != nil || !reflect.DeepEqual(old, p) || old.Digest() != digest {
		t.Fatalf("retconned original: %+v %v", old, e)
	}
	old.RequiredChecks[0] = "mutated"
	again, e := h.ResolveContributionDeliveryPolicyDigest(ctx, digest)
	if e != nil || !reflect.DeepEqual(again, p) {
		t.Fatal("aliased history", e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = h.ResolveContributionDeliveryPolicyDigest(cancelled, digest); e == nil {
		t.Fatal("cancel accepted")
	}
	for _, bad := range []string{"", strings.ToUpper(digest), contribution.Digest([]byte("missing"))} {
		if _, e = h.ResolveContributionDeliveryPolicyDigest(ctx, bad); e == nil {
			t.Fatal("invalid/missing digest accepted")
		}
	}
	if e = os.WriteFile(filepath.Join(blobs.Root(), "blobs", "sha256", strings.TrimPrefix(digest, "sha256:")), []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = h.ResolveContributionDeliveryPolicyDigest(ctx, digest); e == nil {
		t.Fatal("corruption accepted")
	}
	if e = s.retainContributionDeliveryPolicy(ctx, p); e == nil {
		t.Fatal("corrupt write accepted")
	}
}

func TestContributionDeliveryCredentialPinEncoding(t *testing.T) {
	p := historyPolicy()
	legacy, _ := json.Marshal(p)
	if strings.Contains(string(legacy), "CredentialPin") {
		t.Fatal("legacy encoding retconned")
	}
	original := p.Digest()
	p.CredentialPin = &ContributionDeliveryCredentialPin{Key: credentials.CredentialBindingKey{AgentID: "a", StanzaID: "s", DeploymentID: "d", Scope: "g"}, BindingRevision: 1, SecretRecord: "record", Version: 1, Destination: "github", Actor: "actor", RepositoryID: 7, Operations: []delivery.Operation{delivery.Merge}}
	if p.Digest() == original {
		t.Fatal("pin not bound")
	}
	q := p.snapshot()
	q.CredentialPin.Operations[0] = delivery.PullRequest
	q.CredentialPin.Version++
	if p.CredentialPin.Operations[0] != delivery.Merge || p.CredentialPin.Version != 1 {
		t.Fatal("pin aliased")
	}
	wire, _ := json.Marshal(p)
	got, e := decodeContributionDeliveryPolicy(wire, p.Digest())
	if e != nil || !reflect.DeepEqual(got, p) {
		t.Fatal("pin roundtrip", e)
	}
}

func TestContributionDeliveryPolicyHistoryStrict(t *testing.T) {
	p := historyPolicy()
	wire, _ := json.Marshal(p)
	cases := [][]byte{[]byte("null"), append(append([]byte{}, wire...), []byte(" {}")...), []byte(strings.Replace(string(wire), `"ControllerSession":`, `"unknown":0,"ControllerSession":`, 1)), []byte(strings.Replace(string(wire), `"ControllerSession":`, `"ControllerSession":"duplicate","ControllerSession":`, 1)), []byte(strings.Replace(string(wire), `"FenceEpoch":3`, `"FenceEpoch":3,"FenceEpoch":3`, 1)), []byte(strings.Replace(string(wire), `"Author":{"Name":"Author"`, `"Author":{"Name":"Author","Name":"Author"`, 1)), []byte(strings.Replace(string(wire), `"ControllerSession":"controller"`, `"ControllerSession":null`, 1)), make([]byte, contributionDeliveryPolicyMaxBytes+1)}
	for i, b := range cases {
		if _, e := decodeContributionDeliveryPolicy(b, contribution.Digest(b)); e == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	if _, e := decodeContributionDeliveryPolicy(wire, contribution.Digest([]byte("wrong"))); e == nil {
		t.Fatal("wrong digest accepted")
	}
	p.Binding.Contract = "bad"
	wire, _ = json.Marshal(p)
	if _, e := decodeContributionDeliveryPolicy(wire, p.Digest()); e == nil {
		t.Fatal("bad binding accepted")
	}
	if _, e := NewContributionDeliveryPolicyHistory(&Service{}); e == nil {
		t.Fatal("missing canonical reader accepted")
	}
}
