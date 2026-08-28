package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPostActivationPresentsConsoleBeforeEnteringManager(t *testing.T) {
	configPath, _ := validConfigWithoutOperationalAuthority(t)
	var output, diagnostics bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&diagnostics)
	cmd.SetContext(context.Background())
	order := []string{}
	err := enterPostActivationSurfaces(cmd, &rootOptions{configFile: configPath}, func(_ context.Context, target string) error {
		if !strings.HasSuffix(target, "/console") {
			t.Fatalf("console target = %q", target)
		}
		order = append(order, "console")
		return nil
	}, func() error {
		order = append(order, "manager")
		return nil
	})
	if err != nil || strings.Join(order, ",") != "console,manager" {
		t.Fatalf("err=%v order=%v output=%s diagnostics=%s", err, order, output.String(), diagnostics.String())
	}
	if !strings.Contains(output.String(), `"browser_opened": true`) || !strings.Contains(output.String(), "/console") {
		t.Fatalf("console was not reported before manager handoff: %s", output.String())
	}
}

func TestPostActivationBrowserFailureStillEntersManagerAndReportsManualURL(t *testing.T) {
	configPath, _ := validConfigWithoutOperationalAuthority(t)
	var output, diagnostics bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&diagnostics)
	cmd.SetContext(context.Background())
	managerEntered := false
	err := enterPostActivationSurfaces(cmd, &rootOptions{configFile: configPath}, func(context.Context, string) error {
		return errors.New("synthetic opener failure")
	}, func() error {
		managerEntered = true
		return nil
	})
	if err != nil || !managerEntered {
		t.Fatalf("err=%v manager_entered=%t", err, managerEntered)
	}
	if !strings.Contains(output.String(), `"browser_opened": false`) || !strings.Contains(output.String(), `"manual_url"`) || !strings.Contains(diagnostics.String(), "continuing with the authenticated terminal manager") {
		t.Fatalf("output=%s diagnostics=%s", output.String(), diagnostics.String())
	}
}
