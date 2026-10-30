package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
)

// ContributionServiceIdentity describes the running image, not the executable
// currently at its launch path. It never attests a provider or contribution PASS.
type ContributionServiceIdentity struct {
	ExecutableDigest string `json:"executable_digest"`
	SourceRevision   string `json:"source_revision"`
	SourceModified   bool   `json:"source_modified"`
}

var contributionServiceImage = sync.OnceValue(func() ContributionServiceIdentity {
	var id ContributionServiceIdentity
	path, err := os.Executable()
	if runtime.GOOS == "linux" {
		path = "/proc/self/exe"
		err = nil
	}
	if err == nil {
		f, e := os.Open(path)
		if e == nil {
			defer f.Close()
			h := sha256.New()
			if _, e = io.Copy(h, f); e == nil {
				id.ExecutableDigest = "sha256:" + hex.EncodeToString(h.Sum(nil))
			}
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, v := range info.Settings {
			switch v.Key {
			case "vcs.revision":
				id.SourceRevision = v.Value
			case "vcs.modified":
				id.SourceModified = v.Value == "true"
			}
		}
	}
	return id
})

type contributionExpectedServiceKey struct{}

// WithContributionExpectedService carries a caller observation only. A matching
// image is never authentication, approval, or proof of task qualification.
func WithContributionExpectedService(ctx context.Context, expected *ContributionServiceIdentity) context.Context {
	if expected == nil {
		return ctx
	}
	value := *expected
	return context.WithValue(ctx, contributionExpectedServiceKey{}, value)
}
func ContributionExpectedService(ctx context.Context) *ContributionServiceIdentity {
	if ctx == nil {
		return nil
	}
	v, ok := ctx.Value(contributionExpectedServiceKey{}).(ContributionServiceIdentity)
	if !ok {
		return nil
	}
	return &v
}
func CurrentContributionServiceIdentity() ContributionServiceIdentity {
	return contributionServiceImage()
}
func contributionReadinessScope(effect, scope string) (string, error) {
	return contribution.RestrictDelivery(effect, scope)
}

// The first preparation pins the actual owning image to the sealed run. Restart
// readback preserves it. A different image may observe history, not silently
// resume/qualify a fresh candidate using another executable's receipts.
type ContributionRunProvenance struct {
	ContractDigest string                      `json:"contract_digest"`
	RunID          string                      `json:"run_id"`
	Loop           reference.RevisionRef       `json:"loop"`
	Service        ContributionServiceIdentity `json:"service"`
}

func (s *Service) retainContributionRunProvenance(c contribution.Contract) error {
	want := ContributionRunProvenance{c.Digest, c.RunID, c.Loop, CurrentContributionServiceIdentity()}
	var prior ContributionRunProvenance
	if err := s.Store.Load("contribution-image", c.RunID, &prior); err == nil {
		if prior != want {
			return ErrConflict
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.Store.Create("contribution-image", c.RunID, want); err != nil {
		if e := s.Store.Load("contribution-image", c.RunID, &prior); e != nil || prior != want {
			return ErrConflict
		}
	}
	return nil
}
func (s *Service) ReadContributionRunProvenanceAs(ctx context.Context, sub core.Subject, key string) (*ContributionRunProvenance, error) {
	c, _, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return nil, err
	}
	var p ContributionRunProvenance
	if err = s.Store.Load("contribution-image", c.RunID, &p); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if p.ContractDigest != c.Digest || p.RunID != c.RunID || p.Loop != c.Loop || p.Service.ExecutableDigest == "" {
		return nil, ErrDenied
	}
	return &p, nil
}
