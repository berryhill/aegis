package implementation

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/berryhill/aegis/internal/loop"
)

// ByteHunk replaces [Start,End) in the exact FULL preimage. Ordered disjoint
// byte ranges are not fuzzy patches; untouched bytes and file mode are retained.
// This closed data operation grants no shell, generator or checker authority.
type ByteHunk struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content []byte `json:"content"`
}

func resolveEngineeringHunks(ctx context.Context, c loop.VerifiedImplementation, edits []Edit) ([]Edit, error) {
	result := append([]Edit(nil), edits...)
	deny := errors.New("engineering asset preimage or byte hunks rejected")
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for i, e := range result {
		reject := func(stage string) error { return fmt.Errorf("%w: %s", deny, stage) }
		if len(e.Hunks) == 0 {
			if e.PreimageDigest != "" || e.PreimageMode != 0 {
				return nil, reject("orphan-preimage")
			}
			continue
		}
		if c.SchemaVersion != loop.RepositoryEngineeringSchema || !loop.EngineeringTextAsset(e.Path) || e.Content != nil || len(e.Hunks) > 64 || e.PreimageDigest == "" || e.PreimageMode == 0 || ctx.Err() != nil {
			return nil, reject("shape")
		}
		allowed := false
		for _, p := range c.WritableFiles {
			allowed = allowed || p == e.Path
		}
		if !allowed {
			return nil, reject("scope")
		}
		info, err := root.Lstat(e.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxBytes {
			return nil, reject("type-or-size")
		}
		if uint32(info.Mode().Perm()) != e.PreimageMode {
			return nil, reject("mode")
		}
		original, err := readRepositoryContextFile(root, e.Path, info.Size())
		if err != nil || digest(original) != e.PreimageDigest {
			return nil, reject("digest")
		}
		next := []byte{}
		offset := 0
		payload := 0
		for index, h := range e.Hunks {
			payload += len(h.Content)
			if (index > 0 && h.Start == e.Hunks[index-1].Start) || h.Start < offset || h.End < h.Start || h.End > len(original) || payload > 65536 {
				return nil, reject("range-or-payload")
			}
			next = append(next, original[offset:h.Start]...)
			next = append(next, h.Content...)
			offset = h.End
			if len(next) > maxBytes {
				return nil, reject("expanded-size")
			}
		}
		next = append(next, original[offset:]...)
		if len(next) > maxBytes {
			return nil, reject("expanded-size")
		}
		result[i] = Edit{Path: e.Path, Content: next, preserveMode: info.Mode().Perm()}
	}
	return result, nil
}
