package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

type failingConfirmationReader struct{ err error }

func (r failingConfirmationReader) Read([]byte) (int, error) { return 0, r.err }

func confirmationCommand(ctx context.Context, output io.Writer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(output)
	return cmd
}

func TestApproveServicePlanUsesBoundedDefaultYesConfirmation(t *testing.T) {
	plan := userservice.Plan{
		Principal:    "operator",
		UnitPath:     "/unit/aegis.service",
		UnitDigest:   "unit-digest",
		Executable:   "/bin/aegis",
		ConfigPath:   "/config/aegis.yaml",
		Origin:       "http://unix",
		Confirmation: userservice.Confirmation,
	}
	tests := []struct {
		name     string
		input    io.Reader
		ctx      context.Context
		approved bool
		wantErr  bool
	}{
		{name: "empty line", input: strings.NewReader("\n"), ctx: context.Background(), approved: true},
		{name: "lowercase y", input: strings.NewReader("y\n"), ctx: context.Background(), approved: true},
		{name: "uppercase yes", input: strings.NewReader("YES\n"), ctx: context.Background(), approved: true},
		{name: "explicit no", input: strings.NewReader("no\n"), ctx: context.Background()},
		{name: "old phrase", input: strings.NewReader(userservice.Confirmation + "\n"), ctx: context.Background()},
		{name: "unrecognized", input: strings.NewReader("approve it\n"), ctx: context.Background()},
		{name: "eof", input: strings.NewReader(""), ctx: context.Background()},
		{name: "input failure", input: failingConfirmationReader{err: errors.New("read failed")}, ctx: context.Background(), wantErr: true},
		{name: "over limit", input: strings.NewReader(strings.Repeat("y", 129) + "\n"), ctx: context.Background(), wantErr: true},
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name     string
		input    io.Reader
		ctx      context.Context
		approved bool
		wantErr  bool
	}{name: "cancelled", input: strings.NewReader("yes\n"), ctx: cancelled})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			approved, err := approveServicePlan(confirmationCommand(test.ctx, &output), plan, newTerminalInput(test.input))
			if approved != test.approved || (err != nil) != test.wantErr {
				t.Fatalf("approved=%v err=%v output=%q", approved, err, output.String())
			}
			for _, expected := range []string{plan.Principal, plan.UnitPath, plan.UnitDigest, plan.Executable, plan.ConfigPath, plan.Origin, "Install and activate this user service? [Y/n]:"} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("preview omitted %q: %s", expected, output.String())
				}
			}
			if strings.Contains(output.String(), userservice.Confirmation) {
				t.Fatalf("legacy phrase gate remains in preview: %s", output.String())
			}
			if !approved && !strings.Contains(output.String(), "declined; no service state was changed") {
				t.Fatalf("decline did not state the no-mutation result: %s", output.String())
			}
		})
	}
}

func TestReconcileServeTransportUsesBoundedDefaultYesAndMutatesOnlyOnApproval(t *testing.T) {
	tests := []struct {
		name     string
		input    io.Reader
		ctx      context.Context
		approved bool
		wantErr  bool
	}{
		{name: "empty line", input: strings.NewReader("\n"), ctx: context.Background(), approved: true},
		{name: "lowercase yes", input: strings.NewReader("yes\n"), ctx: context.Background(), approved: true},
		{name: "uppercase y", input: strings.NewReader("Y\n"), ctx: context.Background(), approved: true},
		{name: "explicit no", input: strings.NewReader("n\n"), ctx: context.Background()},
		{name: "old phrase", input: strings.NewReader(onboarding.TransportConfirmation + "\n"), ctx: context.Background()},
		{name: "unrecognized", input: strings.NewReader("apply\n"), ctx: context.Background()},
		{name: "eof", input: strings.NewReader(""), ctx: context.Background()},
		{name: "input failure", input: failingConfirmationReader{err: errors.New("read failed")}, ctx: context.Background(), wantErr: true},
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name     string
		input    io.Reader
		ctx      context.Context
		approved bool
		wantErr  bool
	}{name: "cancelled", input: strings.NewReader("yes\n"), ctx: cancelled})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configPath := transportConfirmationFixture(t)
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := onboarding.PreviewTransport(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			applied, err := reconcileServeTransport(confirmationCommand(test.ctx, &output), configPath, newTerminalInput(test.input))
			if applied != test.approved || (err != nil) != test.wantErr {
				t.Fatalf("applied=%v err=%v output=%q", applied, err, output.String())
			}
			for _, expected := range []string{preview.ConfigPath, preview.TokenPath, preview.UnixSocket, preview.OriginalDigest, preview.ResultDigest, "Apply this transport reconciliation? [Y/n]:"} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("preview omitted %q: %s", expected, output.String())
				}
			}
			if strings.Contains(output.String(), onboarding.TransportConfirmation) {
				t.Fatalf("legacy phrase gate remains in preview: %s", output.String())
			}
			if test.approved {
				inspection := config.Inspect(configPath)
				if inspection.State != config.StateValid || inspection.Config.API.Token == "" || inspection.Config.API.UnixSocket == "" {
					t.Fatalf("approved transport was not applied: %+v", inspection)
				}
				if _, statErr := os.Stat(preview.TokenPath); statErr != nil {
					t.Fatalf("approved transport omitted protected token: %v", statErr)
				}
				return
			}
			after, readErr := os.ReadFile(configPath)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("declined transport changed configuration: err=%v", readErr)
			}
			if _, statErr := os.Stat(preview.TokenPath); !os.IsNotExist(statErr) {
				t.Fatalf("declined transport created token: %v", statErr)
			}
			if !strings.Contains(output.String(), "declined; no mutations were performed") {
				t.Fatalf("decline did not state the no-mutation result: %s", output.String())
			}
		})
	}
}

func transportConfirmationFixture(t *testing.T) string {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	state := filepath.Join(root, "state")
	configPath := filepath.Join(root, "aegis.yaml")
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", state, current.Uid, current.Username, filepath.Join(state, "audit-checkpoints"))
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
