package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/initialize"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/slash"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func terminalPair(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func managerCmd(build builder, isTerminal func(io.Reader, io.Writer) bool, initializer *initialize.Service, options *rootOptions, logger *slog.Logger) *cobra.Command {
	command := &cobra.Command{Use: "manager", Short: "Start the built-in local Aegis secrets manager", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New(managerdomain.ReasonRequiresTTY + ": interactive manager mode requires stdin and stdout terminals"))
		}
		if config.Inspect(options.configFile).State != config.StateValid {
			launch, err := runBootstrap(cmd, build, initializer, options.configFile, options.stateDir, logger)
			if err != nil || !launch {
				return err
			}
		}
		return runManager(cmd, build)
	}}
	command.AddCommand(managerCertifyCmd(build), managerModelCmd(build, options))
	return command
}

func initCmd(build builder, isTerminal func(io.Reader, io.Writer) bool, initializer *initialize.Service, options *rootOptions, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Inspect or resume deterministic manager initialization", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New(managerdomain.ReasonRequiresTTY + ": initialization requires an interactive terminal"))
		}
		launch, err := runBootstrap(cmd, build, initializer, options.configFile, options.stateDir, logger)
		if err != nil || !launch {
			return err
		}
		return runManager(cmd, build)
	}}
}

func runFirstInitialization(cmd *cobra.Command, initializer *initialize.Service, configPath, statePath string) (bool, error) {
	return runFirstInitializationWithInput(cmd, initializer, configPath, statePath, newTerminalInput(cmd.InOrStdin()))
}

func runFirstInitializationWithInput(cmd *cobra.Command, initializer *initialize.Service, configPath, statePath string, input *terminalInput) (bool, error) {
	plan, err := initializer.Plan(configPath, statePath)
	if err != nil {
		return false, usage(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis first-run initialization\nAuthenticated local operator: UID %s / user %s\nConfiguration path: %s\nState path: %s\nConfiguration mode: 0600\nNew Aegis configuration directory mode: 0700 (an existing parent must not be writable by group or others)\n\nExact configuration to write:\n%s\n", plan.Principal.UID, plan.Principal.User, plan.ConfigPath, plan.StatePath, plan.Document)
	if len(plan.Partials) != 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Recovery: remove %d recognized secure interrupted initialization artifact(s) before the atomic write.\n", len(plan.Partials))
	}
	fmt.Fprintln(cmd.OutOrStdout(), "No Hermes profile, model, credential, agent, Ollama service, or external system will be created or modified.")
	fmt.Fprint(cmd.OutOrStdout(), "Create this configuration? [Y/n]: ")
	confirmation, eof, err := input.ReadLine(cmd.Context(), 16)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(cmd.OutOrStdout(), "\nInitialization declined; no writes were performed.")
			return false, nil
		}
		return false, err
	}
	if eof {
		fmt.Fprintln(cmd.OutOrStdout(), "\nInitialization declined; no writes were performed.")
		return false, nil
	}
	confirmation = strings.ToLower(strings.TrimSpace(confirmation))
	if confirmation != "" && confirmation != "y" && confirmation != "yes" {
		fmt.Fprintln(cmd.OutOrStdout(), "Initialization declined; no writes were performed.")
		return false, nil
	}
	if err = initializer.Apply(cmd.Context(), plan); err != nil {
		return false, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Initialization completed atomically.\nConfiguration: %s\nState: principal-configured\nNo model was downloaded and no credential authority was created.\n", plan.ConfigPath)
	return true, nil
}

func runManager(cmd *cobra.Command, build builder) error {
	return runManagerWithInput(cmd, build, newTerminalInput(cmd.InOrStdin()))
}

func runManagerWithInput(cmd *cobra.Command, build builder, input *terminalInput) error {
	rawOutput := cmd.OutOrStdout()
	capabilities := tui.Detect(cmd.InOrStdin(), rawOutput, os.Getenv)
	terminalOutput := tui.NewSynchronizedWriter(rawOutput)
	cmd.SetOut(terminalOutput)
	presentation := tui.NewController(terminalOutput, capabilities, tui.SecurityContext{Principal: "pending", Stanza: managerdomain.SecurityContext, MandateState: "pending", Runtime: "Hermes Agent", RuntimeState: "pending", Route: "local-only", NoFallback: true})
	_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "principal authentication", Message: "AEGIS / manager — authenticating principal"})
	lifecycle := managerdomain.NewLifecycle()
	_ = lifecycle.Advance(managerdomain.LifecyclePreflighting)
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		_ = presentation.Emit(tui.Event{Kind: tui.PrincipalDenied, Origin: tui.AegisAuthoritative, Reason: "principal authentication denied"})
		return err
	}
	security := tui.SecurityContext{Principal: subject.PrincipalID, Stanza: managerdomain.SecurityContext, MandateID: subject.ID, MandateState: "active", ExpiresAt: subject.ExpiresAt, Runtime: "Hermes Agent", RuntimeState: "preflight", Route: "local-only", PolicyDigest: managerdomain.PolicyDigest(), NoFallback: true}
	_ = presentation.Emit(tui.Event{Kind: tui.PrincipalAuthenticated, Origin: tui.AegisAuthoritative, Message: "principal authenticated", Security: &security})
	_ = presentation.Emit(tui.Event{Kind: tui.TrustSelected, Origin: tui.AegisAuthoritative, Message: "exactly one built-in trust stanza selected: " + managerdomain.SecurityContext, Security: &security})
	guard, err := managerdomain.NewGuard(int(service.Config.Manager.Ingress.MaximumMessageBytes), service.Config.Manager.Ingress.MaximumMessageRunes, service.Config.Manager.Ingress.BoundedDecodeDepth, service.Config.Manager.Ingress.ScanTimeout)
	if err != nil {
		return err
	}
	readiness := inspectManagerReadiness(service)
	commandRegistry, err := slash.NewRegistry()
	if err != nil {
		return err
	}
	commandService := slash.NewService(service, commandRegistry)
	security.Authority = readiness.authority
	model := service.Config.Manager.Inference.Model
	digest := service.Config.Manager.Inference.ModelDigest
	if model == "" {
		model, digest = "not configured", "not certified"
	}
	security.Model, security.ModelDigest, security.Certification = readiness.model, digest, readiness.certification
	if service.Config.Manager.Inference.ModelDigest == "" {
		security.ModelDigest = "absent"
	}
	_ = presentation.RenderHeader()
	fmt.Fprintf(cmd.OutOrStdout(), "[AEGIS] Authenticated as %s. Preparing exact-local manager; /status shows security details.\n", subject.PrincipalID)
	_ = lifecycle.Advance(managerdomain.LifecycleStarting)
	sessionCtx, cancelSession := context.WithCancelCause(cmd.Context())
	defer cancelSession(nil)
	conversation, startupErr, queued, cancelStartup := startManagerWithQueue(sessionCtx, service, subject, guard, cmd, input, presentation, commandRegistry)
	defer cancelStartup(nil)
	startupReason := ""
	if startupErr != nil {
		_ = lifecycle.Advance(managerdomain.LifecycleDegraded)
		startupReason = managerStartupReason(startupErr)
		readiness.applyStartupReason(startupReason)
		security.RuntimeState = "degraded"
		_ = presentation.Emit(tui.Event{Kind: tui.ManagerDegraded, Origin: tui.AegisAuthoritative, Reason: startupReason, Security: &security})
		auditCtx, auditCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
		defer auditCancel()
		if auditErr := service.AuditManagerStartup(auditCtx, subject, "degraded", startupReason, map[string]string{"runtime": "hermes", "model": service.Config.Manager.Inference.Model, "mode": service.Config.Manager.Inference.Mode}); auditErr != nil {
			return auditErr
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Conversational local inference unavailable. Reason: %s\nDeterministic local controls remain available; no cloud fallback or alternate model was attempted.\nNext: %s\n", startupReason, readiness.nextStep(service.Config.StateDir))
		if strings.Contains(startupErr.Error(), "manager cleanup failed") {
			fmt.Fprintln(cmd.ErrOrStderr(), "Manager startup rollback was incomplete; review the authoritative audit and process state before retrying.")
		}
	} else {
		_ = lifecycle.Advance(managerdomain.LifecycleActive)
		readiness.inference = "active"
		readiness.hermes = "supported"
		readiness.artifact = "installed"
		readiness.certification = "valid"
		security.RuntimeState, security.Certification = "active", "valid"
		_ = presentation.Emit(tui.Event{Kind: tui.ManagerReady, Origin: tui.AegisAuthoritative, Message: "ready — authenticated exact-local session; explicit inline credential commands execute directly; terminal scrollback remains outside Aegis cleanup", Security: &security})
		auditCtx, auditCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
		if auditErr := service.AuditManagerStartup(auditCtx, subject, "ok", "manager_ready", map[string]string{"runtime": "hermes", "model": service.Config.Manager.Inference.Model, "mode": service.Config.Manager.Inference.Mode}); auditErr != nil {
			auditCancel()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
			cleanupErr := conversation.Close(cleanupCtx, managerdomain.EndStartupFailed)
			cleanupCancel()
			return errors.Join(auditErr, cleanupErr)
		}
		auditCancel()

	}
	go func() {
		select {
		case <-sessionCtx.Done():
		case <-time.After(max(time.Until(subject.ExpiresAt), 0)):
			cancelSession(managerdomain.ErrSessionExpired)
		}
	}()
	if conversation != nil {
		go func() {
			select {
			case <-sessionCtx.Done():
			case <-conversation.failures:
				cancelSession(managerdomain.ErrRuntimeFailed)
			}
		}()
	}
	endReason := ""
	composer := tui.NewComposer(cmd.InOrStdin(), cmd.OutOrStdout(), int(service.Config.Manager.Ingress.MaximumMessageBytes))
	for {
		refreshed := tui.Detect(cmd.InOrStdin(), rawOutput, os.Getenv)
		if refreshed.Width != capabilities.Width || refreshed.Height != capabilities.Height {
			capabilities = refreshed
			presentation.SetCapabilities(refreshed)
			_ = presentation.Emit(tui.Event{Kind: tui.TerminalResize, Origin: tui.AegisAuthoritative, Message: fmt.Sprintf("terminal resized to %dx%d", refreshed.Width, refreshed.Height)})
			_ = presentation.RenderHeader()
		}
		if sessionCtx.Err() != nil {
			endReason = managerdomain.EndReasonFromContext(sessionCtx)
			break
		}
		var line string
		var eof bool
		var readErr error
		if len(queued) > 0 {
			line, queued = queued[0], queued[1:]
		} else if capabilities.Profile == tui.Machine {
			fmt.Fprint(cmd.OutOrStdout(), "\n[composer] > ")
			line, eof, readErr = input.ReadLine(sessionCtx, int(service.Config.Manager.Ingress.MaximumMessageBytes))
		} else {
			line, eof, readErr = composer.Read(sessionCtx, "\n[composer] > ", capabilities)
		}
		if reason, ended := managerInputEndReason(sessionCtx, eof, readErr); ended {
			endReason = reason
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "?" {
			_, _ = localDirective(sessionCtx, cmd, service, commandRegistry, commandService, subject, "/help", readiness, conversation != nil, presentation)
			continue
		}
		if trimmed == "quit" || trimmed == "exit" {
			endReason = managerdomain.EndUserExit
			break
		}
		detection := slash.Detect(line)
		if detection == slash.Command {
			exit, err := localDirective(sessionCtx, cmd, service, commandRegistry, commandService, subject, line, readiness, conversation != nil, presentation)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "aegis:", err)
				continue
			}
			if exit {
				endReason = managerdomain.EndUserExit
				break
			}
			continue
		}
		if detection == slash.LiteralSlash {
			line = slash.UnescapeLiteral(line)
		}
		createIntent, createRequested := managerdomain.ParseCreateIntent(line)
		valueReference, valueReadRequested := managerdomain.ParseCredentialValueReadIntent(line)
		readIntent := managerdomain.ParseAuthorityReadIntent(line)
		if !createRequested && managerdomain.ContainsInlineCredentialValue(line) {
			_ = presentation.Emit(tui.Event{Kind: tui.InputBlocked, Origin: tui.AegisAuthoritative, Reason: "credential-bearing create syntax was not recognized; input was not sent to Hermes or retained"})
			continue
		}
		guardedLine := line
		if createRequested {
			guardedLine = createIntent.SafeInput
		}
		finding := guard.Inspect(sessionCtx, managerdomain.ContentEnvelope{Source: managerdomain.SourceUser, SubjectID: subject.ID, ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, ContentType: "text/plain", ProvenanceID: "terminal-turn", RouteClass: "local", Content: []byte(guardedLine), PlaintextAuthorized: conversation != nil})
		if sessionCtx.Err() != nil {
			createIntent.Wipe()
			endReason = managerdomain.EndReasonFromContext(sessionCtx)
			break
		}
		if managerCredentialInputBlocked(finding, createRequested) {
			createIntent.Wipe()
			_ = presentation.Emit(tui.Event{Kind: tui.InputBlocked, Origin: tui.AegisAuthoritative, Reason: "possible credential blocked; message was not sent to Hermes and was not retained; restart the create request and paste it only at protected intake"})
			continue
		}
		if finding.Decision != managerdomain.AllowLocal {
			createIntent.Wipe()
			fmt.Fprintln(cmd.ErrOrStderr(), "Aegis blocked the message:", finding.Reason)
			continue
		}
		if !createRequested && valueReadRequested && conversation != nil {
			composer.Remember(line)
			_ = presentation.Emit(tui.Event{Kind: tui.InputAccepted, Origin: tui.UserInput, Message: line})
			message, operationErr := conversation.session.HandleCredentialValueRead(sessionCtx, valueReference)
			if operationErr != nil {
				_ = presentation.Emit(tui.Event{Kind: tui.OperationFailed, Origin: tui.AegisAuthoritative, Reason: operationErr.Error()})
				continue
			}
			_ = presentation.Emit(tui.Event{Kind: tui.OperationCompleted, Origin: tui.AegisAuthoritative, Message: message})
			continue
		}
		if !createRequested && readIntent != managerdomain.AuthorityReadUnknown && conversation != nil {
			composer.Remember(line)
			_ = presentation.Emit(tui.Event{Kind: tui.InputAccepted, Origin: tui.UserInput, Message: line})
			var message string
			var operationErr error
			switch readIntent {
			case managerdomain.AuthorityReadCount:
				message, operationErr = conversation.session.HandleCredentialCount(sessionCtx)
			case managerdomain.AuthorityReadList:
				message, operationErr = conversation.session.HandleCredentialList(sessionCtx)
			}
			if operationErr != nil {
				_ = presentation.Emit(tui.Event{Kind: tui.OperationFailed, Origin: tui.AegisAuthoritative, Reason: operationErr.Error()})
				continue
			}
			_ = presentation.Emit(tui.Event{Kind: tui.OperationCompleted, Origin: tui.AegisAuthoritative, Message: message})
			continue
		}
		if createRequested && conversation != nil {
			if createIntent.ReferenceMissing {
				reference, referenceErr := readManagerCredentialReference(sessionCtx, composer, cmd.OutOrStdout(), capabilities)
				if referenceErr != nil {
					createIntent.Wipe()
					_ = presentation.Emit(tui.Event{Kind: tui.OperationFailed, Origin: tui.AegisAuthoritative, Reason: referenceErr.Error()})
					continue
				}
				createIntent.Arguments.Reference = reference
			}
			composer.Remember(createIntent.SafeInput)
			_ = presentation.Emit(tui.Event{Kind: tui.InputAccepted, Origin: tui.UserInput, Message: createIntent.SafeInput})
			_ = presentation.Emit(tui.Event{Kind: tui.ProposalValidated, Origin: tui.AegisAuthoritative, Message: fmt.Sprintf("natural create request mapped locally: reference=%s kind=%s disclosure=protected", createIntent.Arguments.Reference, createIntent.Arguments.Kind)})
			var message string
			var createErr error
			if createIntent.ValueRemoved {
				message, createErr = conversation.session.HandleCreateIntentWithValue(sessionCtx, line, createIntent.Arguments, createIntent.Value)
			} else {
				message, createErr = conversation.session.HandleCreateIntent(sessionCtx, createIntent.Arguments)
			}
			createIntent.Wipe()
			if createErr != nil {
				_ = presentation.Emit(tui.Event{Kind: tui.OperationFailed, Origin: tui.AegisAuthoritative, Reason: createErr.Error()})
				continue
			}
			_ = presentation.Emit(tui.Event{Kind: tui.OperationCompleted, Origin: tui.AegisAuthoritative, Message: message})
			continue
		}
		if createRequested && createIntent.ValueRemoved {
			createIntent.Wipe()
			_ = presentation.Emit(tui.Event{Kind: tui.InputBlocked, Origin: tui.AegisAuthoritative, Reason: "credential-bearing input requires the active authenticated exact-local-model session; input was not retained"})
			continue
		}
		if finding.DetectorID != "" {
			composer.Remember("[credential-bearing trusted-local turn purged on close]")
		} else {
			composer.Remember(line)
		}
		if conversation != nil {
			presentedLine := line
			if finding.DetectorID != "" {
				presentedLine = "[credential-bearing trusted-local turn; plaintext sent only to exact local model]"
			}
			_ = presentation.Emit(tui.Event{Kind: tui.InputAccepted, Origin: tui.UserInput, Message: presentedLine})
			_ = presentation.Emit(tui.Event{Kind: tui.TurnStarted, Origin: tui.RuntimeHermes, Stage: "Hermes Agent turn", Message: "turn started through authenticated Aegis proxy to exact Ollama model"})
			message, streamed, turnErr := handleManagerTurn(sessionCtx, presentation, conversation.session, line)
			if turnErr != nil {
				if streamed {
					_ = presentation.Emit(tui.Event{Kind: tui.AssistantRejected, Origin: tui.ModelUntrusted, Reason: "stream rejected because the completed manager response was invalid"})
				}
				if sessionCtx.Err() != nil {
					endReason = managerdomain.EndReasonFromContext(sessionCtx)
					break
				}
				_ = presentation.Emit(tui.Event{Kind: tui.TurnFailed, Origin: tui.AegisDiagnostic, Reason: turnErr.Error()})
				continue
			}
			if message != "" {
				_ = presentation.Emit(tui.Event{Kind: tui.AssistantCompleted, Origin: tui.ModelUntrusted, Message: message})
			}
			_ = presentation.Emit(tui.Event{Kind: tui.TurnCompleted, Origin: tui.AegisAuthoritative, Message: "guarded turn complete"})
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "The local Aegis management model is unavailable (%s). No cloud fallback was attempted.\nUse /help or /status. Next: %s\n", startupReason, readiness.nextStep(service.Config.StateDir))
	}
	if endReason == "" {
		endReason = managerdomain.EndRuntimeFailed
	}
	_ = lifecycle.RequestClose(endReason)
	cancelSession(endReasonCause(endReason))
	_ = presentation.Emit(tui.Event{Kind: tui.CleanupRequested, Origin: tui.AegisAuthoritative, Reason: endReason})
	fmt.Fprintf(cmd.OutOrStdout(), "Shutting down Aegis manager (%s).\n", endReason)
	_ = lifecycle.Advance(managerdomain.LifecycleCleaning)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
	defer cleanupCancel()
	var cleanupErr error
	if conversation != nil {
		_ = presentation.Emit(tui.Event{Kind: tui.CleanupStage, Origin: tui.AegisAuthoritative, Stage: "bounded runtime teardown", Message: "invalidating inference capability, tearing down exact model/runtime and Hermes, removing disposable state, finalizing receipt"})
		cleanupErr = conversation.Close(cleanupCtx, endReason)
	} else {
		cleanupErr = service.AuditManagerSession(cleanupCtx, subject, "ok", endReason, map[string]string{"cleanup": "complete", "runtime": "degraded"})
	}
	composer.Clear()
	presentation.PurgeSessionContent()
	if cleanupErr != nil {
		_ = presentation.Emit(tui.Event{Kind: tui.CleanupFailed, Origin: tui.AegisAuthoritative, Reason: managerdomain.ReasonCleanupIncomplete})
		_ = lifecycle.Advance(managerdomain.LifecycleFailed)
		failed := "unknown teardown stage"
		if conversation != nil && len(conversation.cleanupFailures()) > 0 {
			failed = strings.Join(conversation.cleanupFailures(), ", ")
		}
		return fmt.Errorf("%s: failed teardown stage(s): %s", managerdomain.ReasonCleanupIncomplete, failed)
	}
	_ = lifecycle.Advance(managerdomain.LifecycleClosed)
	_ = presentation.Emit(tui.Event{Kind: tui.CleanupCompleted, Origin: tui.AegisAuthoritative, Message: "bounded cleanup and terminal restoration complete"})
	fmt.Fprintln(cmd.OutOrStdout(), "Aegis manager stopped; cleanup complete.")
	return nil
}

func readManagerCredentialReference(ctx context.Context, composer *tui.Composer, output io.Writer, capabilities tui.Capabilities) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		reference, eof, err := composer.Read(ctx, "\n[AEGIS / authoritative] credential name > ", capabilities)
		if err != nil {
			return "", err
		}
		reference = strings.TrimSpace(reference)
		if eof || reference == "" {
			return "", errors.New("credential creation cancelled: reference is required")
		}
		if credentials.ValidateIdentifier(reference) {
			return reference, nil
		}
		normalized := managerdomain.NormalizeCredentialReference(reference)
		if credentials.ValidateIdentifier(normalized) {
			fmt.Fprintf(output, "[AEGIS / authoritative] using credential reference %s\n", normalized)
			return normalized, nil
		}
		fmt.Fprintln(output, "[AEGIS / authoritative] invalid credential name; enter at least one letter or number")
	}
	return "", errors.New("credential creation cancelled: valid reference not provided")
}

func managerCredentialInputBlocked(finding managerdomain.Finding, createRequested bool) bool {
	return finding.Decision == managerdomain.BlockSecret || (!createRequested && finding.DetectorID != "")
}

func managerInputEndReason(ctx context.Context, eof bool, readErr error) (string, bool) {
	if readErr == nil && !eof {
		return "", false
	}
	if eof || errors.Is(readErr, io.EOF) {
		return managerdomain.EndTerminalEOF, true
	}
	if ctx.Err() != nil {
		return managerdomain.EndReasonFromContext(ctx), true
	}
	return managerdomain.EndRuntimeFailed, true
}

func localDirective(ctx context.Context, cmd *cobra.Command, service *app.Service, registry *slash.Registry, commands *slash.Service, subject core.Subject, line string, readiness managerReadiness, conversational bool, presentation *tui.Controller) (bool, error) {
	request, err := registry.Parse(line)
	if err != nil {
		var parseError *slash.ParseError
		if errors.As(err, &parseError) && len(parseError.Suggestions) != 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Suggestions (submit explicitly):", strings.Join(parseError.Suggestions, "  "))
		}
		return false, err
	}
	state := slash.Degraded
	if conversational {
		state = slash.Active
	}
	managerContext := slash.Context{
		Subject: subject, StanzaID: managerdomain.SecurityContext, MandateID: subject.ID,
		MandateIssued: subject.AuthenticatedAt, MandateExpiry: subject.ExpiresAt, MandateState: "active",
		Lifecycle: state, RuntimeState: readiness.inference, Route: "local-only",
		PolicyVersion: managerdomain.PolicyVersion, PolicyDigest: managerdomain.PolicyDigest(),
		Readiness:      map[string]string{"authority": readiness.authority, "model": readiness.model, "artifact": readiness.artifact, "certification": readiness.certification, "hermes": readiness.hermes, "inference": readiness.inference},
		Conversational: conversational,
	}
	if request.Canonical == "complete" {
		matches := registry.Complete(request.Arguments[0], state, map[string]bool{"credential authority": readiness.authority == "ready", "authenticated manager context": true, "finding store": true, "authoritative audit/event records": true, "audit authority": true})
		fmt.Fprintln(cmd.OutOrStdout(), strings.Join(matches, "  "))
		return false, nil
	}
	if request.Canonical == "secret" {
		return executeSecretMetadata(ctx, cmd, service, request, readiness)
	}
	if request.Canonical == "status" && presentation != nil {
		if err := presentation.RenderStatus(); err != nil {
			return false, err
		}
	}
	if request.Canonical == "help" {
		fmt.Fprintln(cmd.OutOrStdout(), "plain quit and exit also work")
	}
	if presentation != nil {
		_ = presentation.Emit(tui.Event{Kind: tui.CommandAccepted, Origin: tui.AegisAuthoritative, Stage: request.Definition.Policy, Message: "local command accepted: /" + request.Canonical})
	}
	result, err := commands.Execute(ctx, managerContext, request)
	if err != nil {
		if presentation != nil {
			_ = presentation.Emit(tui.Event{Kind: tui.CommandDenied, Origin: tui.AegisAuthoritative, Reason: result.Reason, Fields: slashResultFields(result)})
		}
		return false, err
	}
	if request.Canonical == "clear" && presentation != nil {
		_ = presentation.Redraw()
	}
	kind := tui.CommandResult
	if result.State == "unavailable" {
		kind = tui.CommandUnavailable
	} else if strings.Contains(result.State, "cancel") {
		kind = tui.CommandCancelled
	}
	if presentation != nil {
		message := "/" + request.Canonical + " — " + result.Reason
		if request.Canonical == "status" {
			message += "\nOrigin: AEGIS / authoritative"
		}
		if request.Canonical == "help" && readiness.authority != "ready" {
			message += "\nCredential metadata commands unavailable: credential authority prerequisite is " + readiness.authority + ". Plain quit and exit also work."
		}
		if err = presentation.Emit(tui.Event{Kind: kind, Origin: tui.AegisAuthoritative, Message: message, Fields: slashResultFields(result)}); err != nil {
			return false, err
		}
	}
	return request.Canonical == "exit", nil
}

func executeSecretMetadata(ctx context.Context, cmd *cobra.Command, service *app.Service, request slash.Request, readiness managerReadiness) (bool, error) {
	if readiness.authority != "ready" {
		return false, errors.New("credential authority is not ready; configure credentials.authority and run: aegis secret initialize")
	}
	authority, closeAuthority, err := openAuthorityForService(cmd, service)
	if err != nil {
		return false, err
	}
	defer func() {
		if closeErr := closeAuthority(); closeErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: credential authority close failed\n", managerdomain.ReasonCleanupIncomplete)
		}
	}()
	if request.Arguments[0] == "show" {
		record, metadataErr := authority.Metadata(ctx, request.Arguments[1])
		if metadataErr != nil {
			return false, metadataErr
		}
		return false, output(cmd, record)
	}
	query := ""
	if len(request.Arguments) == 2 {
		query = request.Arguments[1]
	}
	records, listErr := authority.List(ctx, query, 50)
	if listErr != nil {
		return false, listErr
	}
	return false, output(cmd, map[string]any{"records": records, "count": len(records)})
}

func slashResultFields(result slash.Result) map[string]any {
	return map[string]any{
		"schema": result.Schema, "result_schema": result.ResultSchema, "operation": result.Operation, "operation_id": result.OperationID,
		"state": result.State, "reason": result.Reason, "actor_id": result.ActorID,
		"context_id": result.ContextID, "stanza_id": result.StanzaID,
		"requested_scope": result.RequestedScope, "effective_scope": result.EffectiveScope,
		"started_at": result.StartedAt, "finished_at": result.FinishedAt, "observed_at": result.ObservedAt,
		"health": result.Health, "coverage": result.Coverage, "warnings": result.Warnings,
		"related_ids": result.RelatedIDs, "audit_reference": result.AuditReference, "data": result.Data,
	}
}

func handleManagerTurn(ctx context.Context, presentation *tui.Controller, session *managerdomain.Session, input string) (string, bool, error) {
	started := time.Now()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(750 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			_ = presentation.Emit(tui.Event{Kind: tui.TurnProgress, Origin: tui.RuntimeHermes, Message: fmt.Sprintf("Hermes turn active for %s; Ctrl-C interrupts", time.Since(started).Round(time.Second))})
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	message, streamed, err := session.HandleStream(ctx, input, func(snapshot string) error {
		return presentation.Emit(tui.Event{Kind: tui.AssistantDelta, Origin: tui.ModelUntrusted, Message: snapshot})
	})
	close(stop)
	<-done
	return message, streamed, err
}

func streamSafeText(output io.Writer, text string) {
	const chunkRunes = 96
	runes := []rune(tui.Sanitize(text, tui.DefaultSanitizeOptions(tui.Prose)))
	for len(runes) > 0 {
		n := min(len(runes), chunkRunes)
		_, _ = fmt.Fprint(output, string(runes[:n]))
		runes = runes[n:]
	}
	_, _ = fmt.Fprintln(output)
}

type managerReadiness struct {
	authority, model, artifact, certification, hermes, inference string
}

func inspectManagerReadiness(service *app.Service) managerReadiness {
	result := managerReadiness{authority: "absent", model: "absent", artifact: "unavailable", certification: "absent", hermes: "check pending", inference: "degraded"}
	authority := service.Config.Credentials.Authority
	if authority.Database != "" || authority.DeploymentID != "" || authority.Custody != "" {
		result.authority = "invalid"
		if info, err := os.Lstat(authority.Database); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 {
			custodyReady := false
			if authority.Custody == "systemd" {
				directory := os.Getenv("CREDENTIALS_DIRECTORY")
				credential, credentialErr := os.Lstat(filepath.Join(directory, authority.KEKCredential))
				custodyReady = directory != "" && credentialErr == nil && credential.Mode().IsRegular()
			}
			if authority.Custody == "host-file" {
				key, keyErr := os.Lstat(authority.KEKFile)
				custodyReady = keyErr == nil && key.Mode().IsRegular() && key.Mode().Perm()&0077 == 0
			}
			if authority.Custody == "passphrase-file" {
				key, keyErr := os.Lstat(authority.KEKFile)
				custodyReady = keyErr == nil && key.Mode().IsRegular() && key.Mode().Perm()&0077 == 0
			}
			if custodyReady {
				result.authority = "ready"
			}
		}
	}
	inference := service.Config.Manager.Inference
	if inference.Model != "" {
		result.model = "configured: " + inference.Model
	}
	if inference.Certification != "" {
		if info, err := os.Lstat(inference.Certification); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0 {
			result.certification = "configured; live validation pending"
		}
	}
	return result
}

func (r *managerReadiness) applyStartupReason(reason string) {
	switch reason {
	case managerdomain.ReasonAuthorityUnavailable:
		r.authority = "absent"
	case managerdomain.ReasonAuthorityInvalid:
		r.authority = "invalid"
	case managerdomain.ReasonModelAbsent:
		r.artifact = "absent"
	case managerdomain.ReasonDigestMismatch:
		r.artifact = "digest mismatch"
	case managerdomain.ReasonModelLoadFailed:
		r.artifact = "installed; load failed"
	case managerdomain.ReasonContextUnsupported:
		r.hermes = "configured context unsupported"
	case managerdomain.ReasonRouteMismatch:
		r.inference = "degraded; route mismatch"
	case managerdomain.ReasonNotCertified:
		r.artifact = "installed"
		r.certification = "absent, stale, or invalid"
	case managerdomain.ReasonRuntimeUnsupported:
		r.hermes = "unsupported"
	case managerdomain.ReasonGatewayProtocol:
		r.hermes = "supported; gateway unavailable"
	case managerdomain.ReasonOllamaUnavailable:
		r.artifact = "unavailable (Ollama unavailable)"
	}
}

func (r managerReadiness) nextStep(stateDir string) string {
	if r.authority != "ready" {
		return "follow docs/CREDENTIAL_AUTHORITY_SETUP.md, then run aegis secret initialize"
	}
	if r.model == "absent" {
		return "aegis manager model candidates; then inspect an explicit loopback route with: aegis manager model discover --endpoint http://127.0.0.1:11434"
	}
	if r.artifact == "absent" || strings.Contains(r.artifact, "unavailable") {
		return "aegis manager model discover --endpoint http://127.0.0.1:11434 (discovery never downloads or copies a model)"
	}
	if strings.Contains(r.certification, "absent") || strings.Contains(r.certification, "invalid") {
		return "aegis manager certify <candidate-id> after explicit model configuration; certifications remain below " + filepath.Join(stateDir, "manager", "certifications")
	}
	return "review /status and retry in a clean manager session"
}

func managerStartupReason(err error) string {
	text := err.Error()
	for _, reason := range []string{managerdomain.ReasonModelAbsent, managerdomain.ReasonAuthorityUnavailable, managerdomain.ReasonAuthorityInvalid, managerdomain.ReasonDigestMismatch, managerdomain.ReasonNotCertified,
		managerdomain.ReasonModelLoadFailed,
		managerdomain.ReasonContextUnsupported,
		managerdomain.ReasonRouteMismatch, managerdomain.ReasonOllamaUnavailable, managerdomain.ReasonRuntimeUnsupported, managerdomain.ReasonGatewayProtocol, managerdomain.ReasonSessionExpired, managerdomain.ReasonSessionRevoked, managerdomain.ReasonScannerFailed} {
		if strings.Contains(text, reason) {
			return reason
		}
	}
	if errors.Is(err, context.Canceled) {
		return managerdomain.ReasonStartupCancelled
	}
	return managerdomain.ReasonGatewayProtocol
}

func endReasonCause(reason string) error {
	switch reason {
	case managerdomain.EndInterrupt:
		return managerdomain.ErrInterrupt
	case managerdomain.EndTermination:
		return managerdomain.ErrTermination
	case managerdomain.EndSessionExpired:
		return managerdomain.ErrSessionExpired
	case managerdomain.EndSessionRevoked:
		return managerdomain.ErrSessionRevoked
	case managerdomain.EndRuntimeFailed:
		return managerdomain.ErrRuntimeFailed
	default:
		return nil
	}
}
