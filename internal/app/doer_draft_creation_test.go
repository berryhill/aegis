package app

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestDoerDraftInitialIntentReplayAndConflict(t *testing.T) {
	s, subject, input := draftFixture(t)
	input.CreationKey = "server-issued-creation-key"
	original, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.SaveDoerDraftAs(context.Background(), subject, input)
	if err != nil || replay.ID != original.ID || replay.PublicationKey != original.PublicationKey || replay.Version != original.Version {
		t.Fatalf("changed creation identity: %+v %v", replay, err)
	}
	changed := input
	changed.Contract.Task = "Different task"
	if _, err = s.SaveDoerDraftAs(context.Background(), subject, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("same creation key changed payload: %v", err)
	}
	const calls = 8
	results := make(chan DoerDraft, calls)
	errs := make(chan error, calls)
	var workers sync.WaitGroup
	for n := 0; n < calls; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			d, e := s.SaveDoerDraftAs(context.Background(), subject, input)
			results <- d
			errs <- e
		}()
	}
	workers.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.ID != original.ID || result.PublicationKey != original.PublicationKey || result.Version != 1 {
			t.Fatal("concurrent initial replay duplicated the draft")
		}
	}
}
