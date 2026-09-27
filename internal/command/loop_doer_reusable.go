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

type doerReusableDraftInput struct {
	AgentID        string                    `json:"agent_id"`
	LoopID         string                    `json:"loop_id"`
	Revision       uint64                    `json:"revision"`
	PreviousDigest string                    `json:"previous_digest,omitempty"`
	IdempotencyKey string                    `json:"idempotency_key"`
	DoerReusable   loop.DoerReusableContract `json:"doer_reusable"`
}

func loopDoerReusableCmd() *cobra.Command {
	var destination string
	cmd := &cobra.Command{Use: "doer-reusable FILE", Short: "Build a reusable typed Doer Loop v5 draft (no authority or execution)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input doerReusableDraftInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		if strings.TrimSpace(input.AgentID) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
			return usage(errors.New("existing agent_id and idempotency_key are required; online validation resolves the Agent"))
		}
		revision, _, err := loop.NewDoerReusableRevision(input.LoopID, input.Revision, input.PreviousDigest, input.DoerReusable)
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
			var file *os.File
			file, err = os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err == nil {
				_, err = file.Write(data)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Draft only; no task inputs, authority, publication, activation or execution.")
		return nil
	}}
	cmd.Flags().StringVar(&destination, "output", "", "Create a new publication JSON file (default stdout; never overwrite)")
	return cmd
}
