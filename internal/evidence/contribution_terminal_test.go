package evidence

import (
	"encoding/json"
	"testing"
)

func TestContributionTerminalFailureHasNoDummySuccessProof(t *testing.T) {
	r := ContributionTerminalReport{Schema: ContributionFinalPolicy, Owner: "owner", Key: "key", Run: "run", Contract: sha256Reference([]byte("contract")), Scope: "verified-protected-base-merge", Outcome: "denied", Reason: "authority_denied", MergeRefs: []string{}}
	b, e := MarshalContributionTerminalReport(r)
	if e != nil {
		t.Fatal(e)
	}
	got, e := DecodeContributionTerminalReport(sha256Reference(b), b)
	if e != nil || got.Issue != "" || got.Native != "" {
		t.Fatal("failure requires fabricated success refs", e)
	}
	r.MergeRefs = []string{sha256Reference([]byte("merge"))}
	if r.Validate() == nil {
		t.Fatal("known merge erased into ordinary denial")
	}
	r.Outcome = "merged-unresolved"
	if r.Validate() != nil {
		t.Fatal("irreversible merge cannot be retained")
	}
	r.Outcome = "succeeded"
	if r.Validate() == nil {
		t.Fatal("preclosure or empty evidence became terminal success")
	}
	b, _ = json.Marshal(map[string]string{"outcome": "PASS"})
	if _, e = DecodeContributionTerminalReport(sha256Reference(b), b); e == nil {
		t.Fatal("model PASS accepted")
	}
}
func TestGenericProvenanceCannotBecomeContributionFinal(t *testing.T) {
	if ValidateContributionFinalBinding(CompletionProvenance{}, RuntimeArtifact{}, nil) {
		t.Fatal("zero provenance accepted")
	}
	a := RuntimeArtifact{Digest: sha256Reference([]byte("fake"))}
	if ValidateContributionFinalBinding(completionProvenance(a, nil), a, nil) {
		t.Fatal("generic integrity provenance promoted")
	}
}
