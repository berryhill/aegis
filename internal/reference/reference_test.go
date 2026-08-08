package reference

import (
	"bytes"
	"strings"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDigestRefCodecRoundTripsDeterministically(t *testing.T) {
	ref := DigestRef{
		SchemaVersion: DigestRefSchemaVersion,
		ID:            "run/run-123",
		Digest:        testDigest,
	}

	first, err := MarshalDigestRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalDigestRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":"aegis.reference.digest.v1","id":"run/run-123","digest":"` + testDigest + `"}`
	if string(first) != want || !bytes.Equal(first, second) {
		t.Fatalf("digest reference wire value is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
	}

	decoded, err := UnmarshalDigestRef(first)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != ref {
		t.Fatalf("digest reference changed across round trip: got %+v, want %+v", decoded, ref)
	}
}

func TestRevisionRefCodecRoundTripsDeterministically(t *testing.T) {
	ref := RevisionRef{
		SchemaVersion: RevisionRefSchemaVersion,
		ID:            "loop:deploy-v1",
		Revision:      42,
		Digest:        testDigest,
	}

	first, err := MarshalRevisionRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalRevisionRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":"aegis.reference.revision.v1","id":"loop:deploy-v1","revision":42,"digest":"` + testDigest + `"}`
	if string(first) != want || !bytes.Equal(first, second) {
		t.Fatalf("revision reference wire value is not deterministic:\nfirst:  %s\nsecond: %s", first, second)
	}

	decoded, err := UnmarshalRevisionRef(first)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != ref {
		t.Fatalf("revision reference changed across round trip: got %+v, want %+v", decoded, ref)
	}
}

func TestReferenceValidationRejectsIncompleteOrMalformedBindings(t *testing.T) {
	validDigest := DigestRef{SchemaVersion: DigestRefSchemaVersion, ID: "agent-1", Digest: testDigest}
	validRevision := RevisionRef{SchemaVersion: RevisionRefSchemaVersion, ID: "graph-1", Revision: 1, Digest: testDigest}

	digestCases := map[string]DigestRef{
		"missing schema":     withDigestRef(validDigest, func(ref *DigestRef) { ref.SchemaVersion = "" }),
		"unsupported schema": withDigestRef(validDigest, func(ref *DigestRef) { ref.SchemaVersion = "aegis.reference.digest.v2" }),
		"missing id":         withDigestRef(validDigest, func(ref *DigestRef) { ref.ID = "" }),
		"surrounding space":  withDigestRef(validDigest, func(ref *DigestRef) { ref.ID = " agent-1" }),
		"invalid id rune":    withDigestRef(validDigest, func(ref *DigestRef) { ref.ID = "agent#1" }),
		"oversized id":       withDigestRef(validDigest, func(ref *DigestRef) { ref.ID = strings.Repeat("a", 256) }),
		"missing digest":     withDigestRef(validDigest, func(ref *DigestRef) { ref.Digest = "" }),
		"unqualified digest": withDigestRef(validDigest, func(ref *DigestRef) { ref.Digest = strings.Repeat("a", 64) }),
		"uppercase digest":   withDigestRef(validDigest, func(ref *DigestRef) { ref.Digest = "sha256:" + strings.Repeat("A", 64) }),
	}
	for name, ref := range digestCases {
		t.Run("digest/"+name, func(t *testing.T) {
			if err := ref.Validate(); err == nil {
				t.Fatal("invalid digest reference was accepted")
			}
			if encoded, err := MarshalDigestRef(ref); err == nil || encoded != nil {
				t.Fatalf("invalid digest reference was marshaled: %s, %v", encoded, err)
			}
		})
	}

	revisionCases := map[string]RevisionRef{
		"missing schema":     withRevisionRef(validRevision, func(ref *RevisionRef) { ref.SchemaVersion = "" }),
		"unsupported schema": withRevisionRef(validRevision, func(ref *RevisionRef) { ref.SchemaVersion = "aegis.reference.revision.v2" }),
		"missing id":         withRevisionRef(validRevision, func(ref *RevisionRef) { ref.ID = "" }),
		"zero revision":      withRevisionRef(validRevision, func(ref *RevisionRef) { ref.Revision = 0 }),
		"missing digest":     withRevisionRef(validRevision, func(ref *RevisionRef) { ref.Digest = "" }),
	}
	for name, ref := range revisionCases {
		t.Run("revision/"+name, func(t *testing.T) {
			if err := ref.Validate(); err == nil {
				t.Fatal("invalid revision reference was accepted")
			}
			if encoded, err := MarshalRevisionRef(ref); err == nil || encoded != nil {
				t.Fatalf("invalid revision reference was marshaled: %s, %v", encoded, err)
			}
		})
	}
}

func TestReferenceDecodersRejectAmbiguousOrMalformedWireValues(t *testing.T) {
	validDigest := `{"schema_version":"aegis.reference.digest.v1","id":"agent-1","digest":"` + testDigest + `"}`
	validRevision := `{"schema_version":"aegis.reference.revision.v1","id":"loop-1","revision":1,"digest":"` + testDigest + `"}`

	digestCases := map[string][]byte{
		"empty":              nil,
		"invalid utf8":       {0xff},
		"malformed json":     []byte(`{"schema_version":`),
		"non-object":         []byte(`[]`),
		"unknown field":      []byte(strings.Replace(validDigest, `"id":`, `"unknown":true,"id":`, 1)),
		"duplicate key":      []byte(strings.Replace(validDigest, `"id":"agent-1"`, `"id":"agent-1","id":"agent-2"`, 1)),
		"trailing value":     []byte(validDigest + ` {}`),
		"trailing malformed": []byte(validDigest + ` !`),
		"missing component":  []byte(`{"schema_version":"aegis.reference.digest.v1","id":"agent-1"}`),
		"wrong schema":       []byte(strings.Replace(validDigest, DigestRefSchemaVersion, "aegis.reference.digest.v2", 1)),
	}
	for name, data := range digestCases {
		t.Run("digest/"+name, func(t *testing.T) {
			if _, err := UnmarshalDigestRef(data); err == nil {
				t.Fatal("invalid digest reference wire value was accepted")
			}
		})
	}

	revisionCases := map[string][]byte{
		"empty":             nil,
		"malformed json":    []byte(`{"schema_version":`),
		"non-object":        []byte(`true`),
		"unknown field":     []byte(strings.Replace(validRevision, `"revision":`, `"unknown":true,"revision":`, 1)),
		"duplicate key":     []byte(strings.Replace(validRevision, `"revision":1`, `"revision":1,"revision":2`, 1)),
		"trailing value":    []byte(validRevision + ` null`),
		"missing revision":  []byte(strings.Replace(validRevision, `,"revision":1`, "", 1)),
		"zero revision":     []byte(strings.Replace(validRevision, `"revision":1`, `"revision":0`, 1)),
		"fraction revision": []byte(strings.Replace(validRevision, `"revision":1`, `"revision":1.5`, 1)),
		"wrong schema":      []byte(strings.Replace(validRevision, RevisionRefSchemaVersion, "aegis.reference.revision.v2", 1)),
	}
	for name, data := range revisionCases {
		t.Run("revision/"+name, func(t *testing.T) {
			if _, err := UnmarshalRevisionRef(data); err == nil {
				t.Fatal("invalid revision reference wire value was accepted")
			}
		})
	}
}

func withDigestRef(ref DigestRef, mutate func(*DigestRef)) DigestRef {
	mutate(&ref)
	return ref
}

func withRevisionRef(ref RevisionRef, mutate func(*RevisionRef)) RevisionRef {
	mutate(&ref)
	return ref
}
