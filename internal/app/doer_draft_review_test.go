package app

import (
	"context"
	"testing"
)

func TestDoerDraftUnchangedReviewRetainsApprovedVersion(t *testing.T) {
	s, subject, input := draftFixture(t)
	original, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	input.ID, input.ExpectedVersion = original.ID, original.Version
	rereview, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil || rereview.Version != original.Version || rereview.PublicationKey != original.PublicationKey || !rereview.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("unchanged draft review invalidated approved binding: %+v %v", rereview, err)
	}
	input.Contract.Task = "A deliberate changed task"
	changed, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil || changed.Version != original.Version+1 {
		t.Fatalf("material edit did not change version: %+v %v", changed, err)
	}
}
