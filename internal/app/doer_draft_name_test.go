package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDoerDraftNamePinsCandidateAndRetainedVersion(t *testing.T) {
	s, subject, input := draftFixture(t)
	ctx := context.Background()
	legacy, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil {
		t.Fatal(err)
	}
	original, validation, err := NewDoerLoopRevision(input.LoopID, input.Revision, input.PreviousDigest, input.Contract)
	if err != nil {
		t.Fatal(err)
	}
	candidate, check, err := legacy.Candidate()
	if err != nil || !reflect.DeepEqual(candidate, original) || !reflect.DeepEqual(check, validation) {
		t.Fatalf("legacy canonical identity changed: %v", err)
	}
	input.ID, input.ExpectedVersion = legacy.ID, legacy.Version
	input.Name = "Review the retained change"
	named, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil {
		t.Fatal(err)
	}
	if named.Version != legacy.Version+1 || named.PublicationKey != legacy.PublicationKey {
		t.Fatal("name edit lost CAS/key identity")
	}
	candidate, check, err = named.Candidate()
	if err != nil || candidate.Name != input.Name || candidate.Digest == original.Digest || check.Digest == validation.Digest {
		t.Fatalf("name not bound to exact candidate and validation: %v", err)
	}
	loaded, err := s.ReadDoerDraftAs(ctx, subject, named.ID)
	if err != nil || !reflect.DeepEqual(loaded, named) {
		t.Fatalf("retained name readback: %v", err)
	}
	input.ExpectedVersion = named.Version
	unchanged, err := s.SaveDoerDraftAs(ctx, subject, input)
	if err != nil || !reflect.DeepEqual(unchanged, named) {
		t.Fatalf("unchanged return invalidated review: %v", err)
	}
	for _, invalid := range []string{" ", " leading", strings.Repeat("a", 129), "line\nbreak"} {
		input.Name = invalid
		if _, err := s.SaveDoerDraftAs(ctx, subject, input); err == nil {
			t.Fatalf("accepted invalid display name %q", invalid)
		}
	}
}
