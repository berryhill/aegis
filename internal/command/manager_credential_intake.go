package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var intakeOperationID = regexp.MustCompile(`^[a-f0-9]{48}$`)
var errProtectedCreation = errors.New("protected creation not confirmed; do not replay the value; inspect credential metadata before starting a new operation")

func gatewayProtectedIntakeAvailable(cmd *cobra.Command) bool {
	capabilities := tui.Detect(cmd.InOrStdin(), cmd.OutOrStdout(), nil)
	return protectedIntakeCancellationSafe && capabilities.StdinTTY && capabilities.StdoutTTY
}

var errProtectedDialogCancelled = errors.New("protected dialog cancelled by operator")

type intakeOutput struct {
	io.Writer
	err error
}

func (w *intakeOutput) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.Writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (c *gatewayManagerClient) intakeRequest(ctx context.Context, input managergateway.CredentialIntakeRequest) (managergateway.TurnResult, error) {
	request, err := c.request(ctx, http.MethodPost, "/v1/manager/sessions/"+c.sessionID+"/credential-intake", input)
	if err != nil {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	return c.intakeResponse(request)
}

func (c *gatewayManagerClient) intakeResponse(request *http.Request) (managergateway.TurnResult, error) {
	request.Header.Set(managergateway.SessionHeader, c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	var result managergateway.TurnResult
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err = decoder.Decode(&result); err != nil || !validGatewayTurnResult(result) {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	if decoder.Decode(new(any)) != io.EOF {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	return result, nil
}

func (c *gatewayManagerClient) intakeValue(ctx context.Context, operation string, value []byte) (managergateway.TurnResult, error) {
	if !intakeOperationID.MatchString(operation) {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/manager/sessions/"+c.sessionID+"/credential-intake/"+operation+"/value", bytes.NewReader(value))
	if err != nil {
		return managergateway.TurnResult{}, errProtectedCreation
	}
	// No redirect or transport replay may resend the secret body.
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+c.transport)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set(managergateway.ProtectedIntakeHeader, managergateway.ProtectedIntakeProtocol)
	return c.intakeResponse(request)
}

func validIntakeHandoff(c *gatewayManagerClient, h *managergateway.CredentialIntakeHandoff, stage string) bool {
	return c.protectedIntake && h != nil && h.Protocol == managergateway.ProtectedIntakeProtocol && intakeOperationID.MatchString(h.OperationID) && h.SessionID == c.sessionID && credentials.ValidateIdentifier(h.PrincipalID) && h.Stage == stage && time.Now().Before(h.ExpiresAt)
}

// The ordinary composer is suspended for this entire boundary. Raw no-echo
// mode spans metadata, review, approval, both value reads and the HTTP result,
// with input flushed before restoring the terminal. Nothing is added to history
// or submitted as a model turn. Decline and intake failure attempt cancellation;
// an uncertain persistence result is never retried automatically.
func runGatewayCredentialIntake(parent context.Context, cmd *cobra.Command, c *gatewayManagerClient, initial managergateway.TurnResult, output io.Writer) (resultErr error) {
	checked := &intakeOutput{Writer: output}
	output = checked
	defer func() {
		if checked.err != nil {
			resultErr = errors.New("protected review output failed; conversation stopped")
		}
	}()
	h := initial.Intake
	if !gatewayProtectedIntakeAvailable(cmd) || !validGatewayTurnResult(initial) || !validIntakeHandoff(c, h, "metadata") {
		return errProtectedCreation
	}
	ctx, cancel := context.WithDeadline(parent, h.ExpiresAt)
	defer cancel()
	file := cmd.InOrStdin().(*os.File)
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return errors.New("protected terminal mode unavailable; no value collected")
	}
	defer func() {
		if err := discardProtectedTerminalInput(file); err != nil {
			resultErr = errors.New("protected input cleanup failed; conversation stopped")
		}
		_, _ = io.WriteString(output, protectedPasteDisable)
		if err := term.Restore(int(file.Fd()), state); err != nil {
			resultErr = errors.New("terminal restoration failed; conversation stopped")
		}
	}()
	_, _ = io.WriteString(output, protectedPasteEnable)
	finished := false
	defer func() {
		if !finished {
			cleanup, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelCleanup()
			_, _ = c.intakeRequest(cleanup, managergateway.CredentialIntakeRequest{OperationID: h.OperationID, Action: "cancel"})
		}
	}()
	read := func(prompt string, maximum int) ([]byte, error) {
		fmt.Fprint(output, prompt)
		if checked.err != nil {
			return nil, errors.New("protected prompt unavailable")
		}
		value, err := readProtectedTerminalLine(ctx, file, maximum)
		fmt.Fprint(output, "\r\n")
		if checked.err != nil {
			clear(value)
			return nil, errors.New("protected prompt unavailable")
		}
		return value, err
	}
	cancelled := func() error {
		fmt.Fprint(output, "Credential creation cancelled before submission; no write attempted. Returning to the conversation.\r\n")
		return nil
	}
	failedRead := func(err error) error {
		if errors.Is(err, errProtectedDialogCancelled) {
			return cancelled()
		}
		// A partial/oversized paste or failed read has no established boundary.
		// Do not expose late-arriving bytes to the ordinary composer.
		return errors.New("protected input interrupted or unsynchronized; no value submitted; conversation stopped")
	}
	fmt.Fprint(output, "AEGIS / protected credential creation\r\nNo input in this dialog is echoed or added to chat history. Ctrl-C or Ctrl-D cancels.\r\nEnter only non-secret identifiers below; value intake follows explicit approval.\r\n")
	referenceBytes, err := read("Non-secret reference: ", 255)
	if err != nil {
		clear(referenceBytes)
		return failedRead(err)
	}
	reference := string(referenceBytes)
	clear(referenceBytes)
	kindBytes, err := read("Non-secret kind (Enter for opaque): ", 255)
	if err != nil {
		clear(kindBytes)
		return failedRead(err)
	}
	kind := string(kindBytes)
	clear(kindBytes)
	if kind == "" {
		kind = "opaque"
	}
	if !credentials.ValidateIdentifier(reference) || !credentials.ValidateIdentifier(kind) {
		return cancelled()
	}
	review, err := c.intakeRequest(ctx, managergateway.CredentialIntakeRequest{OperationID: h.OperationID, Action: "review", Reference: reference, Kind: kind})
	if err != nil {
		fmt.Fprint(output, "Credential review denied; no value collected.\r\n")
		return nil
	}
	expected := *h
	expected.Reference, expected.Kind, expected.Stage = reference, kind, "review"
	if !validIntakeHandoff(c, review.Intake, "review") || *review.Intake != expected {
		return errProtectedCreation
	}
	fmt.Fprintf(output, "Review: create credential reference=%s kind=%s principal=%s; no binding or Agent credential rights are granted.\r\n", reference, kind, h.PrincipalID)
	if checked.err != nil {
		return errors.New("protected review unavailable; no approval requested")
	}
	// Discard type-ahead only after rendering the exact review and before
	// announcing the fresh approval prompt.
	if err := discardProtectedTerminalInput(file); err != nil {
		return errors.New("fresh protected approval input unavailable; conversation stopped")
	}
	answer, err := read("Type exactly yes to approve this metadata (default: decline): ", 8)
	approved := err == nil && bytes.Equal(answer, []byte("yes"))
	clear(answer)
	if err != nil {
		return failedRead(err)
	}
	if !approved {
		return cancelled()
	}
	approval, err := c.intakeRequest(ctx, managergateway.CredentialIntakeRequest{OperationID: h.OperationID, Action: "approve"})
	if err != nil {
		fmt.Fprint(output, "Credential approval denied; no value collected.\r\n")
		return nil
	}
	expected.Stage = "intake"
	if !validIntakeHandoff(c, approval.Intake, "intake") || *approval.Intake != expected {
		return errProtectedCreation
	}
	fmt.Fprint(output, "Protected value intake: type a single line, or bracketed-paste multiline bytes then Enter. Repeat exactly at confirmation.\r\n")
	first, err := read("Secret value (no echo): ", managergateway.MaximumProtectedValueBytes)
	defer clear(first)
	if err != nil {
		return failedRead(err)
	}
	if len(first) == 0 {
		return cancelled()
	}
	second, err := read("Confirm secret value (no echo): ", managergateway.MaximumProtectedValueBytes)
	defer clear(second)
	if err != nil {
		return failedRead(err)
	}
	if !bytes.Equal(first, second) {
		return cancelled()
	}
	result, err := c.intakeValue(ctx, h.OperationID, first)
	finished = true // Consumed or uncertain: never resubmit this operation.
	if err != nil {
		fmt.Fprint(output, errProtectedCreation.Error()+"\r\n")
		return nil
	}
	if (result.Kind != "credential_created" && result.Kind != "credential_creation_partial") || result.Origin != managergateway.TurnOriginAuthoritative || result.Data["created"] != true || result.Data["operation_id"] != h.OperationID || result.Data["reference"] != reference || result.Data["kind"] != kind {
		return errProtectedCreation
	}
	// Render only validated metadata, not an arbitrary remote message or value.
	record, ok := result.Data["record_id"].(string)
	if !ok || !credentials.ValidateIdentifier(record) {
		return errProtectedCreation
	}
	audit, auditOK := result.Data["audit_verified"].(bool)
	metadata, metadataOK := result.Data["metadata_verified"].(bool)
	if !auditOK || !metadataOK {
		return errProtectedCreation
	}
	if result.Kind == "credential_creation_partial" {
		if audit && metadata {
			return errProtectedCreation
		}
		fmt.Fprintf(output, "Credential persisted: record=%s reference=%s. Audit verified=%t; metadata verified=%t. Confirmation incomplete; do not replay the value. Inspect credential metadata.\r\n", record, reference, audit, metadata)
		return nil
	}
	if !audit || !metadata {
		return errProtectedCreation
	}
	fmt.Fprintf(output, "Credential created: record=%s reference=%s. Metadata verified; returning to the same conversation.\r\n", record, reference)
	return nil
}
