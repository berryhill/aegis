package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"

	"github.com/berryhill/aegis/internal/reference"
)

// NativeBinding carries neutral lineage only. Domain admission remains external.
type NativeBinding = reference.NativeCommandBinding

var ErrNativeQuiescenceUnavailable = reference.ErrNativeQuiescenceUnavailable
var ErrInvalidNativeCommand = errors.New("invalid native command")

// NativeCommandDigest preserves the canonical command JSON byte digest.
func NativeCommandDigest(cmd *exec.Cmd) string {
	if cmd == nil {
		return ""
	}
	b, _ := json.Marshal(struct {
		Path string
		Args []string
		Dir  string
		Env  []string
	}{cmd.Path, cmd.Args, cmd.Dir, cmd.Env})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
