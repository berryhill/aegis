package core

import (
	"bytes"
	"testing"
	"time"
)

func canonicalCommand(id string, kind AuthorityCommandKind, sequence uint64, previous string, issued time.Time) AuthorityCommand {
	command := AuthorityCommand{
		ID: id, Kind: kind, MandateID: "mandate-1", AuthorityContextID: "context-1",
		ExpectedSequence: sequence, ExpectedPreviousDigest: previous,
		ActorSubjectID: "principal-1", ActorAuthentication: "authn-session-1",
		IssuedAt: issued, ExpiresAt: issued.Add(time.Minute), Reason: "operator decision",
	}
	command.Digest = AuthorityCommandDigest(command)
	return command
}

func canonicalFact(id string, command AuthorityCommand, from, to AuthorityState, previous string) AuthorityFact {
	fact := AuthorityFact{
		ID: id, Sequence: command.ExpectedSequence, CommandID: command.ID, CommandDigest: command.Digest,
		MandateID: command.MandateID, AuthorityContextID: command.AuthorityContextID,
		From: from, To: to, OccurredAt: command.IssuedAt.Add(time.Second), RecordedBy: "aegis-controller", PreviousDigest: previous,
	}
	fact.Digest = AuthorityFactDigest(fact)
	return fact
}

func TestCanonicalAuthorityRecordCodecsAreDeterministicAndStrict(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	command := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	fact := canonicalFact("fact-1", command, "", AuthorityStateActive, "")
	projection, err := ReplayCanonicalAuthority([]AuthorityCommand{command}, []AuthorityFact{fact})
	if err != nil {
		t.Fatal(err)
	}
	receipt := AuthorityReceipt{
		ID: "receipt-1", CommandID: command.ID, CommandDigest: command.Digest, Accepted: true,
		FactID: fact.ID, FactDigest: fact.Digest, ProjectionDigest: projection.Digest,
		ReasonCode: "accepted", RecordedAt: fact.OccurredAt, RecordedBy: "aegis-controller",
	}
	receipt.Digest = AuthorityReceiptDigest(receipt)
	replay := AuthorityReplay{Commands: []AuthorityCommand{command}, Facts: []AuthorityFact{fact}, Receipts: []AuthorityReceipt{receipt}}

	encoded, err := EncodeAuthorityReplayCanonical(replay)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeAuthorityReplayCanonical(replay)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, again) {
		t.Fatal("canonical replay encoding is nondeterministic")
	}
	decoded, err := DecodeAuthorityReplayCanonical(encoded)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := EncodeAuthorityReplayCanonical(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, roundTrip) {
		t.Fatal("canonical replay changed across round trip")
	}
	unknown := bytes.Replace(encoded, []byte(`"commands":`), []byte(`"unknown":true,"commands":`), 1)
	if _, err = DecodeAuthorityReplayCanonical(unknown); err == nil {
		t.Fatal("unknown replay field was accepted")
	}
	if err = ValidateAuthorityReceipt(receipt); err != nil {
		t.Fatal(err)
	}
}

func TestReplayCanonicalAuthorityBindsCommandsFactsAndProjection(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	activate := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	active := canonicalFact("fact-1", activate, "", AuthorityStateActive, "")
	revoke := canonicalCommand("command-2", AuthorityCommandRevoke, 2, active.Digest, issued.Add(2*time.Minute))
	revoked := canonicalFact("fact-2", revoke, AuthorityStateActive, AuthorityStateRevoked, active.Digest)

	projection, err := ReplayCanonicalAuthority([]AuthorityCommand{activate, revoke}, []AuthorityFact{active, revoked})
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != AuthorityStateRevoked || projection.SourceSequence != 2 || projection.SourceFactDigest != revoked.Digest || projection.Digest != AuthorityProjectionDigest(projection) {
		t.Fatalf("unexpected projection: %+v", projection)
	}
	replayed, err := ReplayCanonicalAuthority([]AuthorityCommand{activate, revoke}, []AuthorityFact{active, revoked})
	if err != nil || replayed.Digest != projection.Digest || replayed.SourceOccurredAt != projection.SourceOccurredAt {
		t.Fatalf("replay was not deterministic: %+v, %v", replayed, err)
	}
	fromFacts, err := ReplayAuthorityFacts([]AuthorityFact{active, revoked})
	if err != nil || fromFacts.Digest != projection.Digest {
		t.Fatalf("facts did not reconstruct the same projection: %+v, %v", fromFacts, err)
	}
	encoded, err := EncodeAuthorityProjectionCanonical(projection)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAuthorityProjectionCanonical(encoded)
	if err != nil || decoded.Digest != projection.Digest {
		t.Fatalf("projection round trip: %+v, %v", decoded, err)
	}
}

func TestReplayCanonicalAuthoritySupportsExpiration(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	activate := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	active := canonicalFact("fact-1", activate, "", AuthorityStateActive, "")
	expire := canonicalCommand("command-2", AuthorityCommandExpire, 2, active.Digest, issued.Add(2*time.Minute))
	expired := canonicalFact("fact-2", expire, AuthorityStateActive, AuthorityStateExpired, active.Digest)

	projection, err := ReplayCanonicalAuthority([]AuthorityCommand{activate, expire}, []AuthorityFact{active, expired})
	if err != nil {
		t.Fatal(err)
	}
	if projection.State != AuthorityStateExpired || projection.SourceFactDigest != expired.Digest {
		t.Fatalf("unexpected expiration projection: %+v", projection)
	}
}

func TestReplayCanonicalAuthorityFailsClosedOnSubstitutionAndMalformedInput(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	activate := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	active := canonicalFact("fact-1", activate, "", AuthorityStateActive, "")
	revoke := canonicalCommand("command-2", AuthorityCommandRevoke, 2, active.Digest, issued.Add(2*time.Minute))
	revoked := canonicalFact("fact-2", revoke, AuthorityStateActive, AuthorityStateRevoked, active.Digest)

	cases := map[string]struct {
		commands []AuthorityCommand
		facts    []AuthorityFact
	}{
		"missing":             {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active}},
		"reordered":           {[]AuthorityCommand{revoke, activate}, []AuthorityFact{revoked, active}},
		"duplicate":           {[]AuthorityCommand{activate, activate}, []AuthorityFact{active, active}},
		"unknown command":     {[]AuthorityCommand{activate, mutateCommand(revoke, func(c *AuthorityCommand) { c.Kind = "delegate"; c.Digest = AuthorityCommandDigest(*c) })}, []AuthorityFact{active, revoked}},
		"substituted command": {[]AuthorityCommand{activate, mutateCommand(revoke, func(c *AuthorityCommand) { c.ActorSubjectID = "other" })}, []AuthorityFact{active, revoked}},
		"substituted fact":    {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active, mutateFact(revoked, func(f *AuthorityFact) { f.CommandID = activate.ID; f.Digest = AuthorityFactDigest(*f) })}},
		"cross context":       {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active, mutateFact(revoked, func(f *AuthorityFact) { f.AuthorityContextID = "context-2"; f.Digest = AuthorityFactDigest(*f) })}},
		"fact after expiry": {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active, mutateFact(revoked, func(f *AuthorityFact) {
			f.OccurredAt = revoke.ExpiresAt.Add(time.Nanosecond)
			f.Digest = AuthorityFactDigest(*f)
		})}},
		"kind result mismatch":  {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active, mutateFact(revoked, func(f *AuthorityFact) { f.To = AuthorityStateExpired; f.Digest = AuthorityFactDigest(*f) })}},
		"previous substitution": {[]AuthorityCommand{activate, revoke}, []AuthorityFact{active, mutateFact(revoked, func(f *AuthorityFact) { f.PreviousDigest = "sha256:other"; f.Digest = AuthorityFactDigest(*f) })}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReplayCanonicalAuthority(tc.commands, tc.facts); err == nil {
				t.Fatal("invalid replay was accepted")
			}
		})
	}
}

func TestCanonicalAuthorityDecodersRejectWrongEnvelopeAndTrailingData(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	command := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	encoded, err := EncodeAuthorityCommandCanonical(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeAuthorityFactCanonical(encoded); err == nil {
		t.Fatal("command envelope decoded as a fact")
	}
	if _, err = DecodeAuthorityCommandCanonical(append(encoded, []byte("\n{}")...)); err == nil {
		t.Fatal("trailing canonical record was accepted")
	}
	wrongSchema := bytes.Replace(encoded, []byte(AuthorityRecordSchema), []byte("aegis.authority/v2"), 1)
	if _, err = DecodeAuthorityCommandCanonical(wrongSchema); err == nil {
		t.Fatal("unknown authority schema was accepted")
	}
}

func TestRejectedAuthorityReceiptCannotClaimAuthority(t *testing.T) {
	issued := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	command := canonicalCommand("command-1", AuthorityCommandActivate, 1, "", issued)
	if err := ValidateAuthorityCommandAt(command, command.ExpiresAt.Add(time.Nanosecond)); err == nil {
		t.Fatal("expired authority command was accepted")
	}

	receipt := AuthorityReceipt{
		ID: "receipt-1", CommandID: "command-1", CommandDigest: "sha256:command", Accepted: false,
		FactID: "fact-1", ReasonCode: "denied", RecordedAt: time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC), RecordedBy: "aegis-controller",
	}
	receipt.Digest = AuthorityReceiptDigest(receipt)
	if err := ValidateAuthorityReceipt(receipt); err == nil {
		t.Fatal("rejected receipt claimed an authority result")
	}
}

func mutateCommand(command AuthorityCommand, mutate func(*AuthorityCommand)) AuthorityCommand {
	mutate(&command)
	return command
}
func mutateFact(fact AuthorityFact, mutate func(*AuthorityFact)) AuthorityFact {
	mutate(&fact)
	return fact
}
