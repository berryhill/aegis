package orchestration

import "github.com/berryhill/aegis/internal/loop"

// Send the immutable acceptance assertion to the implementer as task data, not
// authority or model-authored evidence. The controller remains the only verifier.
// In particular, do not reduce an exact-byte contract to its task prose: the
// prose need not repeat the expected bytes or the no-newline requirement.
type doerImplementationInput struct {
	Task          string   `json:"task"`
	Feedback      string   `json:"feedback"`
	WritableFiles []string `json:"writable_files"`
	VerifyFile    string   `json:"verify_file"`
	ExpectedText  *string  `json:"expected_text,omitempty"`
	ExactBytes    bool     `json:"exact_bytes"`
	MaxAttempts   uint16   `json:"max_attempts"`
}

func newDoerImplementationInput(c loop.DoerContract, feedback string) doerImplementationInput {
	return doerImplementationInput{Task: c.Task, Feedback: feedback, WritableFiles: append([]string(nil), c.WritableFiles...), VerifyFile: c.VerifyFile, ExpectedText: c.ExpectedText, ExactBytes: c.ExactBytes, MaxAttempts: c.MaxAttempts}
}
