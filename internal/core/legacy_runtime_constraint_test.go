package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestLegacyRuntimeConstraintPreservesCanonicalAuthority(t *testing.T) {
	charter := completePolicyCharter()
	charter.Runtime.VersionConstraint = ">=0.18.0,<0.19.0"
	// Compute the historic canonical representation independently of validation.
	original, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	wantDigest := "sha256:" + hex.EncodeToString(hash[:])
	if err := ValidateCharter(charter); err != nil {
		t.Fatalf("legacy charter rejected: %v", err)
	}
	canonical, err := Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, canonical.Canonical) || canonical.Digest != wantDigest || canonical.Charter.Runtime.VersionConstraint != ">=0.18.0,<0.19.0" {
		t.Fatal("legacy charter bytes, digest, or runtime authority changed")
	}
	charter.Runtime.VersionConstraint = HermesVersionConstraint
	current, err := Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	if current.Digest == canonical.Digest {
		t.Fatal("broadening runtime authority must change the approved digest")
	}
}
