package loop

import (
	"errors"
	"strings"
)

// SourceInventoryPolicy is approved with the immutable host execution profile,
// never proposed by the issue/model. Nil retains the legacy 8 MiB envelope and
// heading-only secret denial. Fixture exceptions bind complete exact preimages.
type SourceInventoryPolicy struct {
	MaxFiles          uint32                  `json:"max_files"`
	MaxFileBytes      int64                   `json:"max_file_bytes"`
	MaxTotalBytes     int64                   `json:"max_total_bytes"`
	SyntheticFixtures []SourceFixtureIdentity `json:"synthetic_fixtures,omitempty"`
}
type SourceFixtureIdentity struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

func (p SourceInventoryPolicy) Validate() error {
	if p.MaxFiles < 1 || p.MaxFiles > 8192 || p.MaxFileBytes < 1 || p.MaxFileBytes > 8<<20 || p.MaxTotalBytes < p.MaxFileBytes || p.MaxTotalBytes > 32<<20 || len(p.SyntheticFixtures) > 32 {
		return errors.New("bounded approved source profile required")
	}
	seen := map[string]bool{}
	for _, f := range p.SyntheticFixtures {
		if !SafeRepositoryContextPath(f.Path) || !strings.HasSuffix(f.Path, "_test.go") || !contextDigestPattern.MatchString(f.Digest) || seen[f.Path] {
			return errors.New("exact synthetic test identity required")
		}
		seen[f.Path] = true
	}
	return nil
}
