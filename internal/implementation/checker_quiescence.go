package implementation

import (
	"fmt"

	"github.com/berryhill/aegis/internal/execution"
)

// CheckerQuiescenceFailure classifies an unknown native join without printing
// the arbitrary underlying error. Both checker/native identities and the typed
// origin remain available to errors.Is/As; this grants no completion proof.
func CheckerQuiescenceFailure(cause error) error {
	return fmt.Errorf("%w: %w", ErrCheckerQuiescenceUnavailable,
		execution.NativeQuiescenceFailure(execution.NativeQuiescenceUnknown, cause))
}
