package loop

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLoopNameIsPinnedInRevisionAndRejectsInvalidDisplayNames(t *testing.T) {
	candidate := branchJoinCandidate()
	candidate.Name = "Review the change"
	named, validation, err := NewRevision(candidate)
	if err != nil || validation.Outcome != ValidationValid {
		t.Fatalf("valid name rejected: %v %+v", err, validation)
	}
	encoded, err := MarshalRevision(named)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalRevision(encoded)
	if err != nil || decoded.Name != candidate.Name {
		t.Fatalf("name lost across canonical revision: %v %+v", err, decoded)
	}
	candidate.Name = "Different name"
	changed, _, err := NewRevision(candidate)
	if err != nil || changed.Digest == named.Digest {
		t.Fatalf("name change did not change revision digest: %v", err)
	}
	for _, bad := range []string{" leading", "trailing ", "two\nlines", strings.Repeat("x", 129)} {
		candidate.Name = bad
		if _, _, err := NewRevision(candidate); err == nil {
			t.Fatalf("invalid display name accepted: %q", bad)
		}
	}
}

// Historical canonical JSON has no name key. Rehash those exact bytes using the
// pre-name schema shape and prove both canonical bytes and digest survive.
func TestLoopAbsentNamePreservesHistoricalDigest(t *testing.T) {
	revision, _, err := NewRevision(branchJoinCandidate())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"name":`) {
		t.Fatal("absent name serialized")
	}
	var historical LoopRevision
	if err = json.Unmarshal(wire, &historical); err != nil {
		t.Fatal(err)
	}
	historical.Digest = ""
	preimage, err := json.Marshal(canonicalRevision(historical))
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("sha256:%x", sha256.Sum256(preimage))
	if revision.Digest != expected {
		t.Fatal("historical digest changed")
	}
}
