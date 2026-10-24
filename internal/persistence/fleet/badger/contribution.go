package badger

import (
	"bytes"
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const (
	familyContribution        byte = 0x70
	familyContributionRequest byte = 0x71
	familyContributionIssue   byte = 0x72
)

func issueKey(c contribution.Contract) []byte {
	return key(familyContributionIssue, fmt.Sprint(c.Source.RepositoryID), fmt.Sprint(c.Source.IssueID))
}
func loadContribution(txn *badgerdb.Txn, id string) (contribution.Contract, error) {
	b, e := get(txn, key(familyContribution, id))
	if e != nil {
		return contribution.Contract{}, e
	}
	c, e := contribution.Unmarshal(b)
	if e != nil {
		return c, corrupt(e)
	}
	if c.QueueItemID != id {
		return c, fleet.ErrCorrupt
	}
	return c, nil
}
func (s *Store) GetContributionForQueue(ctx context.Context, id string) (c contribution.Contract, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { var e error; c, e = loadContribution(txn, id); return e })
	return
}
func (s *Store) GetContribution(ctx context.Context, owner, k string) (c contribution.Contract, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		id, e := get(txn, key(familyContributionRequest, owner, k))
		if e != nil {
			return e
		}
		c, e = loadContribution(txn, string(id))
		if e != nil {
			return e
		}
		if c.OwnerID != owner || c.Key != k {
			return fleet.ErrCorrupt
		}
		return nil
	})
	return
}

// putContribution shares the admission transaction. The reservation deliberately
// survives terminal runs: another key needs explicit reconciliation, never an
// inferred fresh writer merely because wording or Queue state changed.
func putContribution(txn *badgerdb.Txn, a fleet.AcceptedSubmission) error {
	if len(a.Snapshot.Loops) != 1 {
		if a.Contribution != nil {
			return fleet.ErrConflict
		}
		return nil
	}
	ref := a.Snapshot.Loops[0]
	b, e := get(txn, key(familyLoopRevision, ref.ID, revisionPart(ref.Revision)))
	if e != nil {
		return e
	}
	r, e := loop.UnmarshalRevision(b)
	if e != nil {
		return corrupt(e)
	}
	if r.SchemaVersion != loop.ContributionRevisionSchemaVersion {
		if a.Contribution != nil {
			return fleet.ErrConflict
		}
		return nil
	}
	if a.Contribution == nil || r.Digest != ref.Digest {
		return fleet.ErrConflict
	}
	c := *a.Contribution
	wire, e := contribution.Marshal(c)
	if e != nil {
		return e
	}
	if c.QueueItemID != a.QueueItem.ItemID || c.SubmissionID != a.Submission.SubmissionID || c.RunID != a.GraphRun.GraphRunID || c.OwnerID != a.Submission.OwnerID || c.Loop != ref || c.Controller.ID != a.Submission.OwnerAgentID || a.QueueItem.MaxAttempts != 1 {
		return fleet.ErrConflict
	}
	if _, e = contribution.BindLoop(r, c); e != nil {
		return e
	}
	requestKey := key(familyContributionRequest, c.OwnerID, c.Key)
	if _, found, e := optional(txn, requestKey); e != nil {
		return e
	} else if found {
		return fleet.ErrConflict
	}
	if prior, found, e := optional(txn, issueKey(c)); e != nil {
		return e
	} else if found && !bytes.Equal(prior, []byte(c.QueueItemID)) {
		return fleet.ErrConflict
	}
	if e = create(txn, key(familyContribution, c.QueueItemID), wire); e != nil {
		return e
	}
	if e = create(txn, requestKey, []byte(c.QueueItemID)); e != nil {
		return e
	}
	return create(txn, issueKey(c), []byte(c.QueueItemID))
}
