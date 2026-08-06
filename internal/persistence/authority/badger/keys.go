package badger

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	keyVersionV1     byte = 1
	maxKeyIdentifier      = 1024
)

// KeyFamily is a versioned Badger key namespace. Every persisted key must be
// represented here; callers must not invent ad-hoc textual key formats.
type KeyFamily byte

const (
	KeyMetadataStoreID KeyFamily = 0x01
	KeyMetadataSchema  KeyFamily = 0x02
	KeyMetadataCodec   KeyFamily = 0x03

	KeyMandate             KeyFamily = 0x10
	KeyAuthorityContext    KeyFamily = 0x11
	KeyContextBySession    KeyFamily = 0x12
	KeyTransitionFact      KeyFamily = 0x13
	KeyTransitionRoot      KeyFamily = 0x14
	KeyAuthorityCommand    KeyFamily = 0x15
	KeyAuthorityFact       KeyFamily = 0x16
	KeyAuthorityReceipt    KeyFamily = 0x17
	KeyAuthorityProjection KeyFamily = 0x18
	KeyAuthorityOutbox     KeyFamily = 0x19
)

var (
	ErrInvalidKey        = errors.New("invalid authority persistence key")
	ErrUnknownKeyVersion = errors.New("unknown authority persistence key version")
	ErrUnknownKeyFamily  = errors.New("unknown authority persistence key family")
)

type keyShape struct {
	identifiers int
	sequence    bool
}

var keyRegistry = map[KeyFamily]keyShape{
	KeyMetadataStoreID:     {identifiers: 0},
	KeyMetadataSchema:      {identifiers: 0},
	KeyMetadataCodec:       {identifiers: 0},
	KeyMandate:             {identifiers: 1},
	KeyAuthorityContext:    {identifiers: 1},
	KeyContextBySession:    {identifiers: 1},
	KeyTransitionFact:      {identifiers: 1, sequence: true},
	KeyTransitionRoot:      {identifiers: 1},
	KeyAuthorityCommand:    {identifiers: 1},
	KeyAuthorityFact:       {identifiers: 1, sequence: true},
	KeyAuthorityReceipt:    {identifiers: 1},
	KeyAuthorityProjection: {identifiers: 1},
	KeyAuthorityOutbox:     {identifiers: 1, sequence: true},
}

// BinaryKey is the strictly decoded representation of one registry key.
type BinaryKey struct {
	Version     byte
	Family      KeyFamily
	Identifiers []string
	Sequence    uint64
}

func encodeKey(family KeyFamily, identifiers []string, sequence uint64) ([]byte, error) {
	shape, ok := keyRegistry[family]
	if !ok {
		return nil, ErrUnknownKeyFamily
	}
	if len(identifiers) != shape.identifiers || (!shape.sequence && sequence != 0) {
		return nil, fmt.Errorf("%w: key shape does not match family", ErrInvalidKey)
	}
	capacity := 2
	for _, identifier := range identifiers {
		if err := validateKeyIdentifier(identifier); err != nil {
			return nil, err
		}
		capacity += 2 + len(identifier)
	}
	if shape.sequence {
		if sequence == 0 {
			return nil, fmt.Errorf("%w: sequence must be positive", ErrInvalidKey)
		}
		capacity += 8
	}
	encoded := make([]byte, 0, capacity)
	encoded = append(encoded, keyVersionV1, byte(family))
	for _, identifier := range identifiers {
		var length [2]byte
		binary.BigEndian.PutUint16(length[:], uint16(len(identifier)))
		encoded = append(encoded, length[:]...)
		encoded = append(encoded, identifier...)
	}
	if shape.sequence {
		var value [8]byte
		binary.BigEndian.PutUint64(value[:], sequence)
		encoded = append(encoded, value[:]...)
	}
	return encoded, nil
}

// DecodeKey rejects unknown registry entries, malformed lengths, invalid
// identifiers, zero transition sequences, and trailing bytes.
func DecodeKey(encoded []byte) (BinaryKey, error) {
	if len(encoded) < 2 {
		return BinaryKey{}, fmt.Errorf("%w: truncated header", ErrInvalidKey)
	}
	if encoded[0] != keyVersionV1 {
		return BinaryKey{}, ErrUnknownKeyVersion
	}
	family := KeyFamily(encoded[1])
	shape, ok := keyRegistry[family]
	if !ok {
		return BinaryKey{}, ErrUnknownKeyFamily
	}
	decoded := BinaryKey{Version: encoded[0], Family: family, Identifiers: make([]string, 0, shape.identifiers)}
	offset := 2
	for range shape.identifiers {
		if len(encoded)-offset < 2 {
			return BinaryKey{}, fmt.Errorf("%w: truncated identifier length", ErrInvalidKey)
		}
		length := int(binary.BigEndian.Uint16(encoded[offset : offset+2]))
		offset += 2
		if length == 0 || length > maxKeyIdentifier || len(encoded)-offset < length {
			return BinaryKey{}, fmt.Errorf("%w: malformed identifier length", ErrInvalidKey)
		}
		identifier := string(encoded[offset : offset+length])
		if err := validateKeyIdentifier(identifier); err != nil {
			return BinaryKey{}, err
		}
		decoded.Identifiers = append(decoded.Identifiers, identifier)
		offset += length
	}
	if shape.sequence {
		if len(encoded)-offset < 8 {
			return BinaryKey{}, fmt.Errorf("%w: truncated sequence", ErrInvalidKey)
		}
		decoded.Sequence = binary.BigEndian.Uint64(encoded[offset : offset+8])
		if decoded.Sequence == 0 {
			return BinaryKey{}, fmt.Errorf("%w: sequence must be positive", ErrInvalidKey)
		}
		offset += 8
	}
	if offset != len(encoded) {
		return BinaryKey{}, fmt.Errorf("%w: trailing bytes", ErrInvalidKey)
	}
	return decoded, nil
}

func validateKeyIdentifier(identifier string) error {
	if identifier == "" || len(identifier) > maxKeyIdentifier || !utf8.ValidString(identifier) {
		return fmt.Errorf("%w: identifier is empty, oversized, or invalid UTF-8", ErrInvalidKey)
	}
	for _, value := range identifier {
		if value < 0x20 || value == 0x7f {
			return fmt.Errorf("%w: identifier contains a control character", ErrInvalidKey)
		}
	}
	return nil
}

func familyPrefix(family KeyFamily) ([]byte, error) {
	if _, ok := keyRegistry[family]; !ok {
		return nil, ErrUnknownKeyFamily
	}
	return []byte{keyVersionV1, byte(family)}, nil
}

func identifierPrefix(family KeyFamily, identifier string) ([]byte, error) {
	shape, ok := keyRegistry[family]
	if !ok {
		return nil, ErrUnknownKeyFamily
	}
	if shape.identifiers != 1 {
		return nil, fmt.Errorf("%w: family has no identifier prefix", ErrInvalidKey)
	}
	if err := validateKeyIdentifier(identifier); err != nil {
		return nil, err
	}
	prefix := []byte{keyVersionV1, byte(family), byte(len(identifier) >> 8), byte(len(identifier))}
	return append(prefix, identifier...), nil
}

func mustMetadataKey(family KeyFamily) []byte {
	key, err := encodeKey(family, nil, 0)
	if err != nil {
		panic(err)
	}
	return key
}
