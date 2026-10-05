package command

import (
	"errors"
	"os"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/spf13/cobra"
)

func TestDoerServiceCommands(t *testing.T) {
	sentinel := errors.New("builder reached")
	for _, name := range []string{"readiness", "draft-save", "draft-show", "draft-continue"} {
		t.Run(name, func(t *testing.T) {
			root := fleetLoopsCmd(func(*cobra.Command) (*app.Service, error) { return nil, sentinel })
			cmd, _, err := root.Find([]string{name})
			if err != nil || cmd == root {
				t.Fatalf("command missing: %v", err)
			}
			if err := cmd.Args(cmd, nil); err == nil {
				t.Fatal("missing argument accepted")
			}
			if name == "readiness" && cmd.Flags().Lookup("probe") == nil {
				t.Fatal("explicit probe flag missing")
			}
			if name == "draft-show" {
				if err := cmd.RunE(cmd, []string{"doerdraft-00000000000000000000000000000000"}); !errors.Is(err, sentinel) {
					t.Fatalf("builder not used: %v", err)
				}
			}
		})
	}
}

func TestDoerServiceInputsRejectUnknownFields(t *testing.T) {
	for _, name := range []string{"readiness", "draft-save", "draft-continue"} {
		t.Run(name, func(t *testing.T) {
			root := fleetLoopsCmd(func(*cobra.Command) (*app.Service, error) { t.Fatal("invalid input reached service"); return nil, nil })
			cmd, _, err := root.Find([]string{name})
			if err != nil || cmd == root {
				t.Fatal("command missing")
			}
			path := t.TempDir() + "/input.json"
			if err := os.WriteFile(path, []byte(`{"approval":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := cmd.RunE(cmd, []string{path}); err == nil {
				t.Fatal("unknown approval accepted")
			}
		})
	}
}
