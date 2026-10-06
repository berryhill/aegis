package app

import (
	"context"
	"testing"
)

func TestDoerSetupProposalRejectsStaleDraft(t *testing.T) {
	s, sub, input := draftFixture(t)
	d, err := s.SaveDoerDraftAs(context.Background(), sub, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProposeDoerSetupSuccessorAs(context.Background(), sub, d.ID, d.Version+1); err == nil {
		t.Fatal("stale draft accepted")
	}
}
