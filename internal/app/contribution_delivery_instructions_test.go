package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
	"time"
)

// Synthetic admission port: this does not prove native #377 authority.
type instructionAdmissionPort struct {
	writes, reads int
	deny          bool
	ctx           context.Context
}

func (a *instructionAdmissionPort) Admit(ctx context.Context, _ delivery.Intent, write bool) error {
	if ctx != a.ctx {
		return delivery.ErrDenied
	}
	if write {
		a.writes++
		if a.deny {
			return delivery.ErrDenied
		}
	} else {
		a.reads++
	}
	return nil
}

type unavailableInstructionReader struct{}

func (unavailableInstructionReader) Read(context.Context, contribution.IssueURL) (contribution.Source, error) {
	return contribution.Source{}, errors.New("PRIVATE upstream")
}

func TestContributionInstructionAdmission(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "native-private-context")
	s, repo, subject, loop, reader := contributionFixture(t)
	reader.source.Discussion = []contribution.Comment{{ID: 12, Author: contribution.Author{ID: 1, Login: "owner"}, CreatedAt: s.Now(), UpdatedAt: s.Now(), Body: "approved instruction", ContentDigest: contribution.Digest([]byte("approved instruction"))}}
	c, err := s.admitContributionAs(ctx, subject, "session", loop, reader.source.URL.URL, "instructions", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	s.ContributionReader = reader
	base := &instructionAdmissionPort{ctx: ctx}
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest}
	a := &contributionInstructionAuthority{s, subject, c.Key, ref, base}
	i := delivery.Intent{Reference: ref, Repository: c.Repository, RepositoryID: c.Source.RepositoryID}
	original := reader.source
	reader.source.ObservedAt = original.ObservedAt.Add(time.Second)
	reader.source.UpdatedAt = original.UpdatedAt.Add(time.Second)
	reader.source.Revision = "new-etag"
	reader.source.ContentDigest = contribution.Digest([]byte("new wire"))
	if err := a.Admit(ctx, i, true); err != nil || base.writes != 2 {
		t.Fatal("semantic reread failed", err)
	}
	for _, kind := range []string{"title", "body", "comment-add", "comment-edit", "repo", "issue", "url", "creator", "closed", "truncated", "incomplete", "reader"} {
		t.Run(kind, func(t *testing.T) {
			reader.source = original
			s.ContributionReader = reader
			switch kind {
			case "title":
				reader.source.Title += " changed"
			case "body":
				reader.source.Body += " changed"
			case "comment-add":
				reader.source.Discussion = append(append([]contribution.Comment(nil), original.Discussion...), contribution.Comment{ID: 17, Body: "not owner authority"})
			case "comment-edit":
				reader.source.Discussion = append([]contribution.Comment(nil), original.Discussion...)
				reader.source.Discussion[0].Body = "changed instruction"
			case "repo":
				reader.source.RepositoryID++
			case "issue":
				reader.source.IssueID++
			case "url":
				reader.source.URL.Number++
			case "creator":
				reader.source.Author.ID++
			case "closed":
				reader.source.State = "closed"
			case "truncated":
				reader.source.DiscussionTruncated = true
			case "incomplete":
				reader.source.Discussion = nil
			case "reader":
				s.ContributionReader = unavailableInstructionReader{}
			}
			writes := 0
			if err := a.Admit(ctx, i, true); err == nil {
				writes++
			} else if !errors.Is(err, ErrContributionInstructionsHeld) {
				t.Fatal(err)
			}
			if writes != 0 {
				t.Fatal("held source reached effect")
			}
			control, err := repo.GetContributionControl(ctx, c.QueueItemID)
			if err != nil || control.Stop || control.Sequence != 0 {
				t.Fatal("source minted owner control")
			}
		})
	}
	s.ContributionReader = reader
	reader.source = original
	base.deny = true
	before := reader.calls
	if a.Admit(ctx, i, true) == nil || reader.calls != before {
		t.Fatal("real actor authority bypassed")
	}
	if err = s.ControlContributionAs(ctx, subject, "owner-session", c.Key, true, ""); err != nil {
		t.Fatal(err)
	}
	base.deny = false
	if !errors.Is(a.Admit(ctx, i, true), ErrContributionInstructionsHeld) {
		t.Fatal("typed stop bypassed")
	}
	reader.source.State = "closed"
	base.deny = true
	before = reader.calls
	if a.Admit(ctx, i, false) != nil || reader.calls != before || base.reads != 1 {
		t.Fatal("read authority replaced by stale instruction gate")
	}
}
