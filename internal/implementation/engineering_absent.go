package implementation

import (
	"errors"
	"os"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// verifyAbsentRegression distinguishes a missing basename from a missing,
// symlinked or replaced ancestor. New files use existing package directories.
func verifyAbsentRegression(root *os.Root, name string) error {
	reject := errors.New("new regression preimage rejected")
	if !loop.SafeRepositoryContextPath(name) || !strings.HasSuffix(name, "_test.go") {
		return reject
	}
	parent := root
	var owned []*os.Root
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].Close()
		}
	}()
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		info, err := parent.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return reject
		}
		child, err := parent.OpenRoot(part)
		if err != nil {
			return reject
		}
		owned = append(owned, child)
		actual, err := child.Stat(".")
		if err != nil || !os.SameFile(info, actual) {
			return reject
		}
		parent = child
	}
	if _, err := parent.Lstat(parts[len(parts)-1]); !errors.Is(err, os.ErrNotExist) {
		return reject
	}
	return nil
}
