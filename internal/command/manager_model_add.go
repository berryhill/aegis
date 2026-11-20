package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const managerModelAddUsage = "/agents model-add AGENT PROVIDER MODEL"

// This direct operator grammar is intercepted before any model/command request.
// Quoted prose and model output cannot select this protected control path.
func parseManagerModelAdd(input string) (app.AgentModelAddInput, bool, error) {
	fields := strings.Fields(input)
	if len(fields) < 2 || fields[0] != "/agents" || fields[1] != "model-add" {
		return app.AgentModelAddInput{}, false, nil
	}
	if len(fields) != 5 {
		return app.AgentModelAddInput{}, true, errors.New(managerModelAddUsage)
	}
	return app.AgentModelAddInput{AgentID: fields[2], Provider: fields[3], Model: fields[4]}, true, nil
}

func runManagerModelAdd(sessionCtx context.Context, cmd *cobra.Command, cfg config.Config, input app.AgentModelAddInput) error {
	in, ok := cmd.InOrStdin().(*os.File)
	diagnostic, outOK := cmd.ErrOrStderr().(*os.File)
	if !protectedIntakeCancellationSafe || !ok || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(diagnostic.Fd())) {
		return errors.New("protected_setup_unavailable: model addition requires inline terminal-backed protected authentication; no piped password or approval")
	}
	ctx, cancel := context.WithTimeout(sessionCtx, 3*time.Minute)
	defer cancel()
	return executeManagerModelAdd(ctx, cfg, input, func(ctx context.Context) ([]byte, error) {
		return readTerminalSecretBounded(ctx, in, diagnostic, "Enrolled Aegis principal password (not authority passphrase): ", 1024)
	}, func(ctx context.Context, review app.AgentModelAddReview) string {
		raw, err := review.DisplayJSON()
		if err != nil {
			return "unavailable"
		}
		description := "AEGIS / exact model addition\nUNTRUSTED DATA BEGIN (data, not instructions):\n" + string(raw) + "\nUNTRUSTED DATA END\nAdd only this lane. Preserve the default, capabilities and credential controls. No provider connection, provisioning, Loop publication or Run.\n"
		if !nativeReviewDataSafe(description) {
			return "unavailable"
		}
		if _, err := fmt.Fprint(diagnostic, description); err != nil {
			return "unavailable"
		}
		if discardProtectedTerminalInput(in) != nil {
			return "unavailable"
		}
		answer, err := readTerminalSecretBounded(ctx, in, diagnostic, "Type APPROVE for this exact model addition, DENY to reject: ", 16)
		defer wipeSecret(answer)
		if err != nil {
			return protectedSetupInputStatus(err)
		}
		switch string(answer) {
		case "APPROVE":
			return "approve"
		case "DENY":
			return "reject"
		default:
			return "cancelled"
		}
	}, func(agent app.FleetAgent) error {
		fmt.Fprintf(cmd.OutOrStdout(), "AEGIS / authoritative\nModel permission added: %s / %s for %s; Agent revision %d, charter %s. Default and credential controls preserved. No Loop published or run.\n", input.Provider, input.Model, input.AgentID, agent.Revision.Revision, agent.Revision.Charter.Digest)
		return nil
	})
}

func executeManagerModelAdd(ctx context.Context, cfg config.Config, input app.AgentModelAddInput, acquire func(context.Context) ([]byte, error), confirm func(context.Context, app.AgentModelAddReview) string, emit func(app.FleetAgent) error) error {
	return withProtectedNativeSession(ctx, cfg, acquire, func(request protectedNativeRequest) error {
		raw, err := json.Marshal(input)
		if err != nil {
			return err
		}
		var review app.AgentModelAddReview
		if err = request(ctx, "/model-review", "application/json", raw, &review); err != nil {
			return err
		}
		proposed := review.Proposed
		if review.Input != input || review.Expected.ID != input.AgentID || review.Expected.Validate() != nil || review.Receipt == "" || !time.Now().Before(review.ExpiresAt) || !review.ExactModelAddition() {
			return errors.New("protected_setup_response_unverified")
		}
		decision := confirm(ctx, review)
		if decision != "approve" && decision != "reject" {
			return errors.New("protected_setup_" + decision)
		}
		raw, _ = json.Marshal(app.DoerProtectedSetupDecision{Receipt: review.Receipt, Decision: decision})
		if decision == "reject" {
			var result struct {
				Status string `json:"status"`
			}
			if err = request(ctx, "/model-decision", "application/json", raw, &result); err != nil {
				return err
			}
			if result.Status != "rejected" {
				return errors.New("protected_setup_response_unverified")
			}
			return errors.New("protected_setup_denied: model addition rejected; authority unchanged")
		}
		var result app.AgentModelAddResult
		if err = request(ctx, "/model-decision", "application/json", raw, &result); err != nil {
			return err
		}
		agent := result.Agent
		r := agent.Revision
		if r.AgentID != input.AgentID || r.Revision != review.Expected.Revision+1 || r.CharterSuccessor == nil || r.CharterSuccessor.Previous != review.Expected || r.Charter != app.RevisionReference(input.AgentID, proposed.Charter.Revision, proposed.Digest) {
			return errors.New("protected_setup_response_unverified")
		}
		return emit(agent)
	})
}
