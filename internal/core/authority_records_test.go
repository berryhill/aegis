package core

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCanonicalAuthorityCodecRoundTripsDeterministically(t *testing.T) {
	mandate, authority := testAuthorityBinding()

	first, err := EncodeMandateCanonical(mandate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeMandateCanonical(mandate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical mandate encoding is nondeterministic")
	}
	if bytes.Contains(first, []byte("\n")) || !bytes.Contains(first, []byte(`"schema":"aegis.authority/v1","kind":"mandate"`)) {
		t.Fatalf("canonical mandate envelope is not compact and discriminated: %s", first)
	}
	decodedMandate, err := DecodeMandateCanonical(first)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := EncodeMandateCanonical(decodedMandate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, reencoded) {
		t.Fatal("canonical mandate bytes changed across a round trip")
	}

	encodedAuthority, err := EncodeAuthorityContextCanonical(authority)
	if err != nil {
		t.Fatal(err)
	}
	decodedAuthority, err := DecodeAuthorityContextCanonical(encodedAuthority)
	if err != nil {
		t.Fatal(err)
	}
	if decodedAuthority.Digest != authority.Digest || decodedAuthority.SessionID != authority.SessionID {
		t.Fatal("authority context changed across canonical round trip")
	}
	if CanonicalAuthorityDigest(first) != CanonicalAuthorityDigest(second) {
		t.Fatal("identical canonical records produced different identities")
	}
}

func TestCanonicalAuthorityCodecRejectsWrongEnvelopeUnknownFieldsAndTrailingData(t *testing.T) {
	mandate, _ := testAuthorityBinding()
	encoded, err := EncodeMandateCanonical(mandate)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]byte{
		"wrong schema":   bytes.Replace(encoded, []byte(AuthorityRecordSchema), []byte("aegis.authority/v2"), 1),
		"wrong kind":     bytes.Replace(encoded, []byte(`"kind":"mandate"`), []byte(`"kind":"authority_context"`), 1),
		"unknown field":  bytes.Replace(encoded, []byte(`"kind":"mandate"`), []byte(`"unexpected":true,"kind":"mandate"`), 1),
		"nested unknown": bytes.Replace(encoded, []byte(`"id":"subject-1"`), []byte(`"id":"subject-1","unexpected":true`), 1),
		"trailing data":  append(append([]byte(nil), encoded...), []byte(` {}`)...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeMandateCanonical(data); err == nil {
				t.Fatal("malformed authority envelope was accepted")
			}
		})
	}
}

func TestReplayAuthorityTransitionsBuildsVerifiableRoot(t *testing.T) {
	issuedAt := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	active := transitionFact("fact-1", 1, "mandate-1", "authority-1", "", AuthorityStateActive, issuedAt, "")
	revoked := transitionFact("fact-2", 2, "mandate-1", "authority-1", AuthorityStateActive, AuthorityStateRevoked, issuedAt.Add(time.Minute), active.Digest)

	root, err := ReplayAuthorityTransitions([]AuthorityTransitionFact{active, revoked})
	if err != nil {
		t.Fatal(err)
	}
	if root.State != AuthorityStateRevoked || root.Sequence != 2 || root.LastFactID != revoked.ID || root.LastFactDigest != revoked.Digest {
		t.Fatalf("unexpected transition root: %+v", root)
	}
	if root.Digest == "" || root.Digest != AuthorityTransitionRootDigest(root) {
		t.Fatal("transition root has no verifiable canonical identity")
	}
}

func TestReplayAuthorityTransitionsFailsClosedOnBrokenHistory(t *testing.T) {
	issuedAt := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	active := transitionFact("fact-1", 1, "mandate-1", "authority-1", "", AuthorityStateActive, issuedAt, "")
	revoked := transitionFact("fact-2", 2, "mandate-1", "authority-1", AuthorityStateActive, AuthorityStateRevoked, issuedAt.Add(time.Minute), active.Digest)

	cases := map[string][]AuthorityTransitionFact{
		"empty":         nil,
		"missing first": {revoked},
		"reordered":     {revoked, active},
		"duplicated":    {active, active},
		"broken link":   {active, withTransitionMutation(revoked, func(f *AuthorityTransitionFact) { f.PreviousDigest = "sha256:wrong" })},
		"digest tamper": {active, withTransitionMutation(revoked, func(f *AuthorityTransitionFact) { f.Reason = "changed without re-signing" })},
		"context switch": {active, withTransitionMutation(revoked, func(f *AuthorityTransitionFact) {
			f.AuthorityContextID = "authority-2"
			f.Digest = AuthorityTransitionFactDigest(*f)
		})},
		"reactivation": {active, withTransitionMutation(revoked, func(f *AuthorityTransitionFact) {
			f.To = AuthorityStateActive
			f.Digest = AuthorityTransitionFactDigest(*f)
		})},
	}
	for name, facts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReplayAuthorityTransitions(facts); err == nil {
				t.Fatal("invalid authority transition history was accepted")
			}
		})
	}
}

func transitionFact(id string, sequence uint64, mandateID, contextID string, from, to AuthorityState, occurredAt time.Time, previousDigest string) AuthorityTransitionFact {
	fact := AuthorityTransitionFact{
		ID: id, Sequence: sequence, MandateID: mandateID, AuthorityContextID: contextID,
		From: from, To: to, OccurredAt: occurredAt, RecordedBy: "principal", Reason: "operator decision", PreviousDigest: previousDigest,
	}
	fact.Digest = AuthorityTransitionFactDigest(fact)
	return fact
}

func withTransitionMutation(fact AuthorityTransitionFact, mutate func(*AuthorityTransitionFact)) AuthorityTransitionFact {
	mutate(&fact)
	return fact
}

func TestCanonicalAuthorityDigestUsesQualifiedSHA256Identity(t *testing.T) {
	digest := CanonicalAuthorityDigest([]byte("authority"))
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("unexpected canonical authority digest %q", digest)
	}
}
