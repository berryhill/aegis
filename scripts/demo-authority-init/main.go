package main

import (
	"context"
	"fmt"
	"os"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: demo-authority-init AUTHORITY_ROOT")
		os.Exit(2)
	}
	if _, err := authoritybadger.Initialize(context.Background(), os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "initialize disposable demo authority: %v\n", err)
		os.Exit(1)
	}
}
