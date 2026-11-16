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

// One successful decode per Store, bounded independently of database size.
// Oversized values retain the original cold behavior, not a new admission limit.
const contributionDecodeMaxBytes = 2 << 20

type contributionDecodeCache struct {
	wire  []byte
	value contribution.Contract
}

func loadContribution(txn *badgerdb.Txn, id string, caches ...*contributionReplayCache) (contribution.Contract, error) {
	c, _, err := loadContributionBytes(txn, id, caches...)
	return c, err
}

func loadContributionBytes(txn *badgerdb.Txn, id string, caches ...*contributionReplayCache) (contribution.Contract, []byte, error) {
	// Always read the actual current transaction. Neither a key, digest field,
	// prior transaction nor a prior authority decision is a cache hit.
	b, e := get(txn, key(familyContribution, id))
	if e != nil {
		return contribution.Contract{}, nil, e
	}
	var cache *contributionDecodeCache
	if len(caches) != 0 && caches[0] != nil {
		cache = &caches[0].decoded
	}
	if cache != nil && cache.wire != nil && bytes.Equal(cache.wire, b) {
		c := cache.value.Clone()
		if c.QueueItemID != id {
			return c, nil, fleet.ErrCorrupt
		}
		return c, b, nil
	}
	c, e := contribution.Unmarshal(b)
	if e != nil {
		return c, nil, corrupt(e)
	}
	if c.QueueItemID != id {
		return c, nil, fleet.ErrCorrupt
	}
	if cache != nil {
		*cache = contributionDecodeCache{}
		if len(b) <= contributionDecodeMaxBytes {
			*cache = contributionDecodeCache{wire: bytes.Clone(b), value: c.Clone()}
		}
	}
	return c, b, nil
}
func (s *Store) GetContributionForQueue(ctx context.Context, id string) (c contribution.Contract, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		var e error
		c, e = loadContribution(txn, id, &s.contributionReplay)
		return e
	})
	return
}
func (s *Store) GetContribution(ctx context.Context, owner, k string) (c contribution.Contract, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		id, e := get(txn, key(familyContributionRequest, owner, k))
		if e != nil {
			return e
		}
		c, e = loadContribution(txn, string(id), &s.contributionReplay)
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
