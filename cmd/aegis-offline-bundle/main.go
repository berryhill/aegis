// aegis-offline-bundle prepares a locked dependency input at definition time.
// It never publishes a definition, invokes a provider, or runs repository code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/berryhill/aegis/internal/implementation"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	repository := flag.String("repository", "", "absolute definition repository")
	cache := flag.String("module-cache", "", "absolute existing Go module cache (untrusted transport)")
	goBinary := flag.String("go", "", "absolute resolved trusted Go driver")
	output := flag.String("output", "", "absolute create-only dependency archive")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "no positional arguments supported")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	bundle, err := implementation.PrepareEngineeringDependencyBundle(ctx, *repository, *cache, *goBinary, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(bundle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
