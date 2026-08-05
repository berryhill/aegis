package badger

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestBinaryKeyRegistryRoundTrip(t *testing.T) {
	tests := []struct {
		family      KeyFamily
		identifiers []string
		sequence    uint64
	}{
		{family: KeyMetadataStoreID},
		{family: KeyMetadataSchema},
		{family: KeyMetadataCodec},
		{family: KeyMandate, identifiers: []string{"mandate/one"}},
		{family: KeyAuthorityContext, identifiers: []string{"authority:one"}},
		{family: KeyContextBySession, identifiers: []string{"session-one"}},
		{family: KeyTransitionFact, identifiers: []string{"authority-one"}, sequence: 42},
		{family: KeyTransitionRoot, identifiers: []string{"authority-one"}},
	}
	for _, test := range tests {
		encoded, err := encodeKey(test.family, test.identifiers, test.sequence)
		if err != nil {
			t.Fatalf("encode family %x: %v", byte(test.family), err)
		}
		decoded, err := DecodeKey(encoded)
		if err != nil {
			t.Fatalf("decode family %x: %v", byte(test.family), err)
		}
		if decoded.Version != keyVersionV1 || decoded.Family != test.family || decoded.Sequence != test.sequence || !equalStrings(decoded.Identifiers, test.identifiers) {
			t.Fatalf("family %x round trip = %+v", byte(test.family), decoded)
		}
	}
}

func TestBinaryKeyRegistryRejectsMalformedAndUnknownKeys(t *testing.T) {
	valid, err := encodeKey(KeyMandate, []string{"mandate-one"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	oversized := strings.Repeat("a", maxKeyIdentifier+1)
	tests := []struct {
		name string
		key  []byte
		want error
	}{
		{name: "truncated header", key: []byte{keyVersionV1}, want: ErrInvalidKey},
		{name: "unknown version", key: []byte{keyVersionV1 + 1, byte(KeyMandate)}, want: ErrUnknownKeyVersion},
		{name: "unknown family", key: []byte{keyVersionV1, 0xff}, want: ErrUnknownKeyFamily},
		{name: "truncated identifier length", key: []byte{keyVersionV1, byte(KeyMandate), 0}, want: ErrInvalidKey},
		{name: "empty identifier", key: []byte{keyVersionV1, byte(KeyMandate), 0, 0}, want: ErrInvalidKey},
		{name: "truncated identifier", key: []byte{keyVersionV1, byte(KeyMandate), 0, 2, 'a'}, want: ErrInvalidKey},
		{name: "invalid utf8", key: []byte{keyVersionV1, byte(KeyMandate), 0, 1, 0xff}, want: ErrInvalidKey},
		{name: "control character", key: []byte{keyVersionV1, byte(KeyMandate), 0, 1, 0x1f}, want: ErrInvalidKey},
		{name: "trailing bytes", key: append(append([]byte(nil), valid...), 0), want: ErrInvalidKey},
		{name: "zero sequence", key: append([]byte{keyVersionV1, byte(KeyTransitionFact), 0, 1, 'a'}, make([]byte, 8)...), want: ErrInvalidKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeKey(test.key); !errors.Is(err, test.want) {
				t.Fatalf("DecodeKey error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := encodeKey(KeyMandate, []string{oversized}, 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("oversized identifier error = %v, want ErrInvalidKey", err)
	}
	if _, err := encodeKey(KeyTransitionFact, []string{"authority"}, 0); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("zero sequence error = %v, want ErrInvalidKey", err)
	}
	if _, err := encodeKey(KeyMandate, []string{"mandate"}, 1); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("unexpected sequence error = %v, want ErrInvalidKey", err)
	}
}

func TestIdentifierPrefixIsLengthDelimited(t *testing.T) {
	parent, err := identifierPrefix(KeyTransitionFact, "authority")
	if err != nil {
		t.Fatal(err)
	}
	childKey, err := encodeKey(KeyTransitionFact, []string{"authority-child"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	parentKey, err := encodeKey(KeyTransitionFact, []string{"authority"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(parentKey, parent) {
		t.Fatal("exact identifier key does not share its registry prefix")
	}
	if bytes.HasPrefix(childKey, parent) {
		t.Fatal("shared textual prefix crossed a length-delimited identifier boundary")
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func FuzzBinaryKeyDecodeCanonicalRoundTrip(f *testing.F) {
	valid, err := encodeKey(KeyTransitionFact, []string{"authority-fuzz"}, 7)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte{keyVersionV1})
	f.Add([]byte{keyVersionV1, 0xff, 0, 1, 'x'})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxKeyIdentifier+32 {
			t.Skip()
		}
		decoded, err := DecodeKey(input)
		if err != nil {
			return
		}
		reencoded, err := encodeKey(decoded.Family, decoded.Identifiers, decoded.Sequence)
		if err != nil {
			t.Fatalf("decoded key cannot be encoded: %+v: %v", decoded, err)
		}
		if !bytes.Equal(reencoded, input) {
			t.Fatalf("decoder accepted non-canonical key: input=%x canonical=%x", input, reencoded)
		}
	})
}
