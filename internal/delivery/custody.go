package delivery

import (
	"context"
	"github.com/berryhill/aegis/internal/credentials"
)

// CredentialCustody consumes an already approved exact binding. It cannot
// create, bind, grant, rotate or widen credentials. Authority admission (#377)
// is separate and still mandatory. Construct only in the trusted owner bridge.
type CredentialCustody struct {
	authority                              *credentials.Authority
	key                                    credentials.CredentialBindingKey
	destination, record, repository, actor string
	version, revision, repositoryID        uint64
	operations                             map[Operation]bool
}

func NewCredentialCustody(a *credentials.Authority, key credentials.CredentialBindingKey, destination, record string, version, revision uint64, repository, actor string, repositoryID uint64, ops []Operation) (*CredentialCustody, error) {
	if a == nil || key.Validate() != nil || !credentials.ValidateIdentifier(destination) || !credentials.ValidateIdentifier(record) || version == 0 || revision == 0 || !repoName.MatchString(repository) || actor == "" || repositoryID == 0 || len(ops) == 0 {
		return nil, ErrDenied
	}
	c := &CredentialCustody{a, key, destination, record, repository, actor, version, revision, repositoryID, map[Operation]bool{}}
	for _, op := range ops {
		switch op {
		case UploadBlob, UploadTree, UploadCommit, Publish, PullRequest, UpdatePullRequest, Merge, ReconcileIssue:
		default:
			return nil, ErrDenied
		}
		if c.operations[op] {
			return nil, ErrDenied
		}
		c.operations[op] = true
	}
	return c, nil
}
func (c *CredentialCustody) Use(ctx context.Context, i Intent, fn func([]byte) error) error {
	if c == nil || fn == nil || !c.operations[i.Operation] || i.Repository != c.repository || i.RepositoryID != c.repositoryID || i.Actor != c.actor {
		return ErrDenied
	}
	var callbackErr error
	err := c.authority.UseResolved(ctx, c.key, c.destination, func(r credentials.ResolvedSecret, b []byte) error {
		if r.Binding.Key != c.key || r.Binding.BindingRevision != c.revision || r.Binding.SecretRecord != c.record || r.Record.ID != c.record || r.Version.RecordID != c.record || r.Version.Version != c.version || !r.Binding.Enabled || r.Record.Status != credentials.StatusActive || len(r.Binding.Destinations) != 1 || r.Binding.Destinations[0] != c.destination || r.Binding.VersionPolicy != credentials.VersionPinned || r.Binding.PinnedVersion != c.version {
			return ErrDenied
		}
		callbackErr = fn(b)
		return callbackErr
	})
	if err != nil {
		if callbackErr == errAbsent || callbackErr == errRemote {
			return callbackErr
		}
		return ErrDenied
	}
	return nil
}
