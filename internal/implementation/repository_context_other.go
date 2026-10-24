//go:build !unix

package implementation

import (
	"errors"
	"os"
)

func openRepositoryContextFile(root *os.Root, name string) (*os.File, error) {
	return nil, errors.New("repository context rejected")
}
