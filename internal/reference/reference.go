// Package reference owns immutable cross-domain identity bindings.
//
// References identify an exact persisted record. They carry no authority and
// must be resolved and revalidated by the domain performing an operation.
package reference

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	DigestRefSchemaVersion   = "aegis.reference.digest.v1"
	RevisionRefSchemaVersion = "aegis.reference.revision.v1"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// DigestRef binds a stable identity to the exact digest of a create-only
// record, such as a run snapshot or authority context.
type DigestRef struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Digest        string `json:"digest"`
}

// RevisionRef binds a stable identity to one exact positive revision and its
// canonical digest. Consumers must never substitute a current/latest revision.
type RevisionRef struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Revision      uint64 `json:"revision"`
	Digest        string `json:"digest"`
}

func (ref DigestRef) Validate() error {
	if ref.SchemaVersion != DigestRefSchemaVersion {
		return errors.New("unsupported digest reference schema version")
	}
	if err := validateID(ref.ID); err != nil {
		return err
	}
	return validateDigest(ref.Digest)
}

func (ref RevisionRef) Validate() error {
	if ref.SchemaVersion != RevisionRefSchemaVersion {
		return errors.New("unsupported revision reference schema version")
	}
	if err := validateID(ref.ID); err != nil {
		return err
	}
	if ref.Revision == 0 {
		return errors.New("reference revision must be positive")
	}
	return validateDigest(ref.Digest)
}

func validateID(id string) error {
	if !utf8.ValidString(id) || strings.TrimSpace(id) != id || !identifierPattern.MatchString(id) {
		return errors.New("reference id is malformed")
	}
	return nil
}

func validateDigest(digest string) error {
	if !digestPattern.MatchString(digest) {
		return errors.New("reference digest must be lowercase sha256:<64-hex>")
	}
	return nil
}
