package delivery

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"strings"
	"testing"
)

// This independent pre-bridge wire struct is the unreleased Badger convention.
// In particular Record's embedded Outcome must never replace either Outcome.
func TestHistoryEnvelopeLegacyWire(t *testing.T) {
	type legacyWire struct {
		Intent                 Intent
		Outcome                Outcome
		Observation            Observation
		Revision               uint64
		PreviousDigest, Digest string
	}
	old := legacyWire{Intent: Intent{ID: "legacy"}, Outcome: Unknown, Observation: Observation{Outcome: Unknown}, Revision: 1}
	preimage, _ := json.Marshal(old)
	old.Digest = contribution.Digest(preimage)
	wire, _ := json.Marshal(old)
	got, err := ParseHistoryEnvelope(wire)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, _ := json.Marshal(got)
	if !bytes.Equal(wire, roundtrip) {
		t.Fatalf("legacy bytes changed: %s", roundtrip)
	}
	for name, b := range map[string][]byte{
		"duplicate": bytes.Replace(wire, []byte(`"Revision":1`), []byte(`"Revision":1,"Revision":1`), 1),
		"unknown":   append([]byte(`{"extra":0,`), wire[1:]...),
		"trailing":  append(append([]byte(nil), wire...), []byte(`{}`)...),
		"hash":      bytes.Replace(wire, []byte(old.Digest), []byte("sha256:"+strings.Repeat("0", 64)), 1),
		"alternate": append([]byte(" "), wire...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseHistoryEnvelope(b); err == nil {
				t.Fatal("accepted malformed wire")
			}
		})
	}
	collision := got
	collision.Record.Observation.Outcome = Confirmed
	collision.Digest = ""
	b, _ := json.Marshal(collision)
	collision.Digest = contribution.Digest(b)
	if ValidateHistoryEnvelope(collision) == nil {
		t.Fatal("accepted embedded Outcome collision")
	}
}
