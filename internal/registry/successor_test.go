package registry

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

func TestSuccessorDigestAndValidation(t *testing.T) {
	r := AgentRevision{SchemaVersion: AgentRevisionSchemaVersion, AgentID: "agent", Revision: 2, Source: FleetSource{FleetID: "fleet", Kind: "test", SourceID: "source"}, Runtime: RuntimeBinding{Adapter: "hermes", Runtime: "hermes", Target: "default"}, Ownership: Ownership{OwnerID: "owner", AccountabilityID: "owner"}, Lifecycle: LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 2, Digest: testCharterDigest}}
	r.CharterSuccessor = &CharterSuccessor{Previous: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: testPolicyDigest}, ApprovedBy: "owner"}
	sealed, err := SealRevision(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"remove", "digest", "owner", "id", "revision", "schema"} {
		t.Run(name, func(t *testing.T) {
			changed := sealed
			metadata := *sealed.CharterSuccessor
			changed.CharterSuccessor = &metadata
			switch name {
			case "remove":
				changed.CharterSuccessor = nil
			case "digest":
				metadata.Previous.Digest = testCharterDigest
			case "owner":
				metadata.ApprovedBy = "other"
			case "id":
				metadata.Previous.ID = "other"
			case "revision":
				metadata.Previous.Revision = 2
			case "schema":
				metadata.Previous.SchemaVersion = "invalid"
			}
			wire, _ := json.Marshal(changed)
			if _, err := UnmarshalAgentRevision(wire); err == nil {
				t.Fatal("accepted tampered provenance")
			}
			if name != "remove" && name != "digest" {
				if _, err := SealRevision(changed); err == nil {
					t.Fatal("sealed invalid provenance")
				}
			}
		})
	}
	// Lifecycle successors retain the original approval, not an invented approval.
	r.Revision = 3
	r.Lifecycle = LifecycleDisabled
	if _, err := SealRevision(r); err != nil {
		t.Fatal(err)
	}
}
