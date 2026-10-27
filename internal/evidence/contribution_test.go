package evidence

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

func contributionManifestFixture() ContributionManifest {
	d := func(s string) string { return sha256Reference([]byte(s)) }
	r := func(s string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: s, Digest: d(s)}
	}
	return ContributionManifest{Schema: ContributionManifestV1, Binding: ContributionBinding{
		Run: "run", QueueItem: r("queue"), Attempt: "attempt", Claim: "claim", Invocation: r("invocation"), Contract: r("contract"), Control: r("control"),
		Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop", Revision: 1, Digest: d("loop")}, Profile: d("profile"), Policy: ContributionPolicyV1, Candidate: d("candidate")},
		Target: "verified-protected-base-merge", CandidateEvidence: d("native"), AuthorityEvidence: d("authority"), DeliveryEvidence: []string{d("commit"), d("pr"), d("merge")}, IssueEvidence: d("issue"), ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestContributionManifestStrictContentAddress(t *testing.T) {
	m := contributionManifestFixture()
	wire, err := MarshalContributionManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeContributionManifest(sha256Reference(wire), wire); err != nil || got.Binding != m.Binding {
		t.Fatalf("roundtrip: %v", err)
	}
	cases := map[string][]byte{
		"unknown":      bytes.Replace(wire, []byte(`"schema":`), []byte(`"extra":true,"schema":`), 1),
		"duplicate":    bytes.Replace(wire, []byte(`"run":"run"`), []byte(`"run":"wrong","run":"run"`), 1),
		"nested-alias": bytes.Replace(wire, []byte(`"run":"run"`), []byte(`"Run":"run"`), 1),
		"null":         bytes.Replace(wire, []byte(`"attempt":"attempt"`), []byte(`"attempt":null`), 1),
		"null-slice":   bytes.Replace(wire, []byte(`"delivery_evidence":[`), []byte(`"delivery_evidence":null,"unused":[`), 1),
		"missing":      bytes.Replace(wire, []byte(`"attempt":"attempt",`), nil, 1),
		"trailing":     append(append([]byte{}, wire...), []byte(` {}`)...),
		"whitespace":   append([]byte(" "), wire...),
		"model-pass":   []byte(`{"outcome":"PASS"}`),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeContributionManifest(sha256Reference(b), b); err == nil {
				t.Fatal("malformed evidence accepted")
			}
		})
	}
	if _, err := DecodeContributionManifest(sha256Reference([]byte("wrong")), wire); err == nil {
		t.Fatal("corrupt content address accepted")
	}
}
func TestContributionManifestClosedBoundaryAndExactSets(t *testing.T) {
	for _, name := range []string{"missing-native", "missing-authority", "duplicate", "substitution", "wrong-policy", "missing-run", "wrong-digest", "nil-effects", "local-publish", "no-merge-close", "unknown-target"} {
		t.Run(name, func(t *testing.T) {
			m := contributionManifestFixture()
			switch name {
			case "missing-native":
				m.CandidateEvidence = ""
			case "missing-authority":
				m.AuthorityEvidence = ""
			case "duplicate":
				m.DeliveryEvidence = append(m.DeliveryEvidence, m.DeliveryEvidence[0])
			case "substitution":
				m.DeliveryEvidence[0] = m.CandidateEvidence
			case "wrong-policy":
				m.Binding.Policy = "aegis.selected-file.v1"
			case "missing-run":
				m.Binding.Run = ""
			case "wrong-digest":
				m.Binding.Candidate = "sha256:" + strings.Repeat("G", 64)
			case "nil-effects":
				m.DeliveryEvidence = nil
			case "local-publish":
				m.Target = "local-only"
			case "no-merge-close":
				m.Target = "no-merge"
			case "unknown-target":
				m.Target = "merge-anywhere"
			}
			if m.Validate() == nil {
				t.Fatal("invalid boundary accepted")
			}
		})
	}
	m := contributionManifestFixture()
	m.Target = "local-only"
	m.DeliveryEvidence = []string{}
	m.IssueEvidence = ""
	if m.Validate() != nil {
		t.Fatal("narrow target rejected")
	}
}
func TestContributionReceiptPreservesIrreversibleMerge(t *testing.T) {
	m := contributionManifestFixture()
	w, _ := MarshalContributionManifest(m)
	r := ContributionReceipt{Schema: ContributionReceiptV1, Binding: m.Binding, Manifest: sha256Reference(w), Boundary: m.Target, Outcome: "merged-unresolved", Reason: "merged_validation_unresolved", Merge: strings.Repeat("b", 40), Evidence: []string{m.CandidateEvidence}, ObservedAt: m.ObservedAt}
	b, err := MarshalContributionReceipt(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeContributionReceipt(sha256Reference(b), b); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"failed", "denied", "cancelled", "expired", "exhausted", "unknown"} {
		x := r
		x.Outcome = outcome
		x.Reason = "effect_unknown"
		if x.Validate() == nil {
			t.Fatalf("merge erased into %s", outcome)
		}
	}
	r.Outcome = "succeeded"
	r.Reason = "contribution_verified"
	if r.Validate() != nil {
		t.Fatal("valid schema rejected")
	}
	r.Boundary = "no-merge"
	if r.Validate() == nil {
		t.Fatal("narrow scope promoted to merge success")
	}
	r.Outcome = "narrower-target-completed"
	r.Reason = "narrower_target_verified"
	r.Merge = ""
	if r.Validate() != nil {
		t.Fatal("narrow result rejected")
	}
	r.Evidence = append(r.Evidence, r.Evidence[0])
	if r.Validate() == nil {
		t.Fatal("duplicate receipt evidence accepted")
	}
}
func TestContributionReceiptDoesNotAcceptUnknownOrNull(t *testing.T) {
	m := contributionManifestFixture()
	w, _ := MarshalContributionManifest(m)
	r := ContributionReceipt{Schema: ContributionReceiptV1, Binding: m.Binding, Manifest: sha256Reference(w), Boundary: "local-only", Outcome: "failed", Reason: "evidence_invalid", Evidence: []string{}, ObservedAt: m.ObservedAt}
	b, _ := json.Marshal(r)
	for _, bad := range [][]byte{bytes.Replace(b, []byte(`"merge":""`), []byte(`"merge":null`), 1), bytes.Replace(b, []byte(`"reason":"evidence_invalid"`), []byte(`"reason":"evidence_invalid","reason":"evidence_invalid"`), 1)} {
		if _, err := DecodeContributionReceipt(sha256Reference(bad), bad); err == nil {
			t.Fatal("malformed receipt accepted")
		}
	}
}
