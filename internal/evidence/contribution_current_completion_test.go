package evidence

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestContributionCurrentCompletionBindingClosed(t *testing.T) {
	ref := func(id string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: sha256Reference([]byte(id))}
	}
	r := ContributionTerminalReport{Schema: ContributionFinalPolicy, Owner: "owner", Key: "key", Run: "run", Contract: sha256Reference([]byte("contract")), Scope: "verified-protected-base-merge", Outcome: "denied", Reason: "authority_denied", MergeRefs: []string{}}
	old, err := MarshalContributionTerminalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(old), "current_completion") {
		t.Fatal("legacy canonical report gained a field")
	}
	p := ContributionCurrentCompletion{Authority: ref("current"), OriginalAuthority: ref("original"), Role: ref("role"), Continuation: ref("lease"), Epoch: 2, Mandate: "new", OriginalMandate: "old", Stanza: "principal"}
	r.Current = &p
	wire, err := MarshalContributionTerminalReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeContributionTerminalReport(sha256Reference(wire), wire); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"old-authority", "epoch", "role", "lease", "mandate", "original-mandate", "stanza"} {
		bad := p
		switch kind {
		case "old-authority":
			bad.Authority = bad.OriginalAuthority
		case "epoch":
			bad.Epoch = 0
		case "role":
			bad.Role = reference.DigestRef{}
		case "lease":
			bad.Continuation = reference.DigestRef{}
		case "mandate":
			bad.Mandate = ""
		case "original-mandate":
			bad.OriginalMandate = ""
		case "stanza":
			bad.Stanza = ""
		}
		copy := r
		copy.Current = &bad
		if copy.Validate() == nil {
			t.Fatal("invalid current completion accepted", kind)
		}
	}
	var tree map[string]any
	if json.Unmarshal(old, &tree) != nil {
		t.Fatal("legacy report")
	}
	tree["current_completion"] = nil
	badWire, _ := json.Marshal(tree)
	if _, err := DecodeContributionTerminalReport(sha256Reference(badWire), badWire); err == nil {
		t.Fatal("null current proof accepted")
	}
}
