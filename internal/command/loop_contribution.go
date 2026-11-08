package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/spf13/cobra"
)

// contributionDraftInput is authoring input, never runtime authority.
type contributionDraftInput struct {
	AgentID        string                      `json:"agent_id"`
	LoopID         string                      `json:"loop_id"`
	Revision       uint64                      `json:"revision"`
	PreviousDigest string                      `json:"previous_digest,omitempty"`
	IdempotencyKey string                      `json:"idempotency_key"`
	Implementation loop.VerifiedImplementation `json:"implementation"`
	Profile        loop.ContributionProfile    `json:"profile"`
}

func loopContributionCmd() *cobra.Command {
	var destination string
	cmd := &cobra.Command{Use: "contribution FILE", Short: "Build a typed URL-only contribution publication proposal locally (no publication or execution)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input contributionDraftInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		if strings.TrimSpace(input.AgentID) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
			return usage(errors.New("named existing agent_id and idempotency_key are required; online validation resolves the enabled Agent"))
		}
		if input.Profile.Engineering == nil {
			return usage(errors.New("approved Engineering policy required for URL-only authoring"))
		}
		revision, _, err := loop.NewContributionRevision(input.LoopID, input.Revision, input.PreviousDigest, input.Implementation, input.Profile)
		if err != nil {
			return usage(err)
		}
		publication := app.PublishLoopInput{AgentID: input.AgentID, Revision: revision, ExpectedPreviousDigest: input.PreviousDigest, IdempotencyKey: input.IdempotencyKey}
		data, err := json.MarshalIndent(publication, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if destination == "" {
			_, err = cmd.OutOrStdout().Write(data)
		} else {
			var f *os.File
			f, err = os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err == nil {
				_, err = f.Write(data)
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			return err
		}
		digest, _ := input.Implementation.Digest()
		fmt.Fprintf(cmd.ErrOrStderr(), "Proposal only; Agent existence requires online validation. Contract digest: %s. Not published, activated, authorized, or queued.\n", digest)
		return nil
	}}
	cmd.Flags().StringVar(&destination, "output", "", "Create a new publication JSON file (default stdout; never overwrite)")
	return cmd
}
