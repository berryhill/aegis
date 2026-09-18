package command

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/spf13/cobra"
)

// The output contract is fixed UTF-8, with no trailing newline. This is a
// precommitted expectation, never a runtime artifact or verification receipt.
const helloOutput = "hello"

type helloDraftInput struct {
	AgentID        string `json:"agent_id"`
	LoopID         string `json:"loop_id"`
	Revision       uint64 `json:"revision"`
	PreviousDigest string `json:"previous_digest,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

func loopHelloCmd() *cobra.Command {
	var destination string
	cmd := &cobra.Command{Use: "hello FILE", Short: "Build a fixed-output executable v2 hello draft locally (no publication or execution)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input helloDraftInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		if strings.TrimSpace(input.AgentID) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
			return usage(errors.New("named existing agent_id and idempotency_key are required; online validation resolves the enabled Agent"))
		}
		var template app.PublishLoopInput
		if err := json.Unmarshal(loopExample, &template); err != nil {
			return err
		}
		candidate := template.Revision
		candidate.LoopID, candidate.Revision, candidate.PreviousDigest = input.LoopID, input.Revision, input.PreviousDigest
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(helloOutput)))
		candidate.Steps[0].EvidenceClaims = []loop.EvidenceClaim{{Claim: "hello-exact-output", MediaType: "text/plain", ExpectedDigest: digest, VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1}}
		candidate.RequiredEvidence = []loop.EvidenceRequirement{{Claim: "hello-exact-output", ProducerStepID: candidate.Steps[0].ID}}
		revision, _, err := loop.NewRevision(candidate)
		if err != nil {
			return usage(err)
		}
		publication := app.PublishLoopInput{AgentID: input.AgentID, Revision: revision, ExpectedPreviousDigest: input.PreviousDigest, IdempotencyKey: input.IdempotencyKey}
		data, err := json.MarshalIndent(publication, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Draft only; expected UTF-8 output %q without newline (%s). Agent existence requires online validation. Not published, activated, authorized, or queued.\n", helloOutput, digest)
		return nil
	}}
	cmd.Flags().StringVar(&destination, "output", "", "Create a new publication JSON file (required; never overwrite)")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
