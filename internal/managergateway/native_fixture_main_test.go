package managergateway

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

// Native fixtures are independently isolated and bounded by the journey slots.
// Allow three to overlap without changing GOMAXPROCS, Go build parallelism or
// the external CPU/memory/task/deadline limits. Explicit caller policy wins.
func TestMain(m *testing.M) {
	flag.Parse()
	if err := configureNativeFixtureParallelism(flag.CommandLine); err != nil {
		fmt.Fprintln(os.Stderr, "invalid native fixture scheduling flags")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func configureNativeFixtureParallelism(flags *flag.FlagSet) error {
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "test.parallel" {
			explicit = true
		}
	})
	if explicit {
		return nil
	}
	return flags.Set("test.parallel", "3")
}

func TestNativeFixtureParallelismPreservesExplicitPolicy(t *testing.T) {
	for _, args := range [][]string{nil, {"-test.parallel=1"}, {"-test.parallel=2"}, {"-test.parallel=4"}} {
		flags := flag.NewFlagSet("fixture", flag.ContinueOnError)
		parallel := flags.Int("test.parallel", 2, "fixture concurrency")
		deadline := flags.Duration("test.timeout", 0, "package alarm")
		if err := flags.Parse(args); err != nil {
			t.Fatal(err)
		}
		before := *parallel
		if err := configureNativeFixtureParallelism(flags); err != nil {
			t.Fatal(err)
		}
		if len(args) == 0 {
			if *parallel != 3 {
				t.Fatal("default fixture scheduling did not permit three journeys")
			}
		} else if *parallel != before {
			t.Fatal("explicit caller concurrency was overwritten")
		}
		if *deadline != 0 {
			t.Fatal("fixture scheduling changed the package alarm")
		}
	}
}
