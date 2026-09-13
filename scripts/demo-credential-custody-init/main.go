// This proof-only helper creates fresh host custody, never records or runtime identity.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/berryhill/aegis/internal/credentials"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: demo-credential-custody-init NEW_DIRECTORY")
		os.Exit(2)
	}
	root := os.Args[1]
	if err := os.Mkdir(root, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "credential proof custody requires a fresh directory")
		os.Exit(1)
	}
	if err := credentials.CreateHostKey(filepath.Join(root, "authority.kek"), "installed-proof-kek"); err != nil {
		fmt.Fprintln(os.Stderr, "credential proof custody initialization failed")
		os.Exit(1)
	}
}
