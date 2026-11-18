package recoveryphysical

import (
	"context"
	"errors"
)

// RecoveryPair identifies a receipt's exact pre/post-exchange pair. Only the
// side whose bytes equal the original may use original-engine mode admission;
// the candidate is always independently rechecked through strict Directory.
func RecoveryPair(ctx context.Context, selected, work, original, candidate string) (post bool, err error) {
	a, err := OriginalDirectory(ctx, selected, "")
	if err != nil {
		return false, err
	}
	w, err := OriginalDirectory(ctx, work, "")
	if err != nil {
		return false, err
	}
	if a == candidate && w == original {
		if strict, e := Directory(ctx, selected, ""); e == nil && strict == candidate {
			return true, nil
		}
	}
	if a == original && w == candidate {
		if strict, e := Directory(ctx, work, ""); e == nil && strict == candidate {
			return false, nil
		}
	}
	return false, errors.New("recovery pair does not prove a strict candidate and original evidence")
}
