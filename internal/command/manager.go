package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/initialize"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func terminalPair(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
}

func managerCmd(build builder, isTerminal func(io.Reader, io.Writer) bool, initializer *initialize.Service, options *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "manager", Short: "Start the built-in local Aegis secrets manager", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New(managerdomain.ReasonRequiresTTY + ": interactive manager mode requires stdin and stdout terminals"))
		}
		inspection := config.Inspect(options.configFile)
		if inspection.State != config.StateValid {
			if inspection.State != config.StateAbsent && inspection.State != config.StatePartial {
				return usage(inspection.Failure())
			}
			initialized, err := runFirstInitialization(cmd, initializer, options.configFile, options.stateDir)
			if err != nil || !initialized {
				return err
			}
		}
		return runManager(cmd, build)
	}}
	command.AddCommand(managerCertifyCmd(build), managerModelCmd(build, options))
	return command
}

func initCmd(build builder, isTerminal func(io.Reader, io.Writer) bool, initializer *initialize.Service, options *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Inspect or resume deterministic manager initialization", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New(managerdomain.ReasonRequiresTTY + ": initialization requires an interactive terminal"))
		}
		inspection := config.Inspect(options.configFile)
		if inspection.State != config.StateValid {
			if inspection.State != config.StateAbsent && inspection.State != config.StatePartial {
				return usage(inspection.Failure())
			}
			_, err := runFirstInitialization(cmd, initializer, options.configFile, options.stateDir)
			return err
		}
		service, subject, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		state := "principal-configured"
		authority := service.Config.Credentials.Authority
		if authority.Database != "" && authority.Custody != "" {
			state = "authority-configured"
		}
		if service.Config.Manager.Inference.Model != "" {
			state = "runtime-configured"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Aegis manager initialization\nAuthenticated principal: %s\nState: %s\nEffects: none (inspection only)\n", subject.PrincipalID, state)
		if state != "runtime-configured" {
			fmt.Fprintln(cmd.OutOrStdout(), "Next: configure credential authority and a locally present, certified Ollama model. No model was downloaded.")
		}
		return nil
	}}
}

func runFirstInitialization(cmd *cobra.Command, initializer *initialize.Service, configPath, statePath string) (bool, error) {
	plan, err := initializer.Plan(configPath, statePath)
	if err != nil {
		return false, usage(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis first-run initialization\nAuthenticated local operator: UID %s / user %s\nConfiguration path: %s\nState path: %s\nConfiguration mode: 0600\nNew Aegis configuration directory mode: 0700 (an existing parent must not be writable by group or others)\n\nExact configuration to write:\n%s\n", plan.Principal.UID, plan.Principal.User, plan.ConfigPath, plan.StatePath, plan.Document)
	if len(plan.Partials) != 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Recovery: remove %d recognized secure interrupted initialization artifact(s) before the atomic write.\n", len(plan.Partials))
	}
	fmt.Fprintln(cmd.OutOrStdout(), "No Hermes profile, model, credential, agent, Ollama service, or external system will be created or modified.")
	fmt.Fprint(cmd.OutOrStdout(), "Type yes to create this configuration, or anything else to decline: ")
	confirmation, eof, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 16)
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
	if confirmation != "yes" {
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
	lifecycle := managerdomain.NewLifecycle()
	_ = lifecycle.Advance(managerdomain.LifecyclePreflighting)
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		return err
	}
	guard, err := managerdomain.NewGuard(int(service.Config.Manager.Ingress.MaximumMessageBytes), service.Config.Manager.Ingress.MaximumMessageRunes, service.Config.Manager.Ingress.BoundedDecodeDepth, service.Config.Manager.Ingress.ScanTimeout)
	if err != nil {
		return err
	}
	readiness := inspectManagerReadiness(service)
	model := service.Config.Manager.Inference.Model
	digest := service.Config.Manager.Inference.ModelDigest
	if model == "" {
		model, digest = "not configured", "not certified"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis manager\nPrincipal: %s (authenticated)\nCredential authority: %s\nRuntime: Hermes Agent\nInference: Ollama local / %s@%s\nSecurity context: %s\nManager route: local-only\nCloud fallback: disabled\nModel switching: disabled\nRuntime-state isolation is not host sandboxing.\nType /help for local commands.\n", subject.PrincipalID, readiness.authority, model, digest, managerdomain.SecurityContext)
	_ = lifecycle.Advance(managerdomain.LifecycleStarting)
	sessionCtx, cancelSession := context.WithCancelCause(cmd.Context())
	defer cancelSession(nil)
	conversation, startupErr := startConversationalManager(sessionCtx, service, subject, guard, cmd)
	startupReason := ""
	if startupErr != nil {
		_ = lifecycle.Advance(managerdomain.LifecycleDegraded)
		startupReason = managerStartupReason(startupErr)
		readiness.applyStartupReason(startupReason)
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
		auditCtx, auditCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
		if auditErr := service.AuditManagerStartup(auditCtx, subject, "ok", "manager_ready", map[string]string{"runtime": "hermes", "model": service.Config.Manager.Inference.Model, "mode": service.Config.Manager.Inference.Mode}); auditErr != nil {
			auditCancel()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
			cleanupErr := conversation.Close(cleanupCtx, managerdomain.EndStartupFailed)
			cleanupCancel()
			return errors.Join(auditErr, cleanupErr)
		}
		auditCancel()
		fmt.Fprintln(cmd.OutOrStdout(), "Conversational session: active; route: authenticated loopback proxy to exact certified model")
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
	input := newTerminalInput(cmd.InOrStdin())
	endReason := ""
	for {
		if sessionCtx.Err() != nil {
			endReason = managerdomain.EndReasonFromContext(sessionCtx)
			break
		}
		fmt.Fprint(cmd.OutOrStdout(), "aegis> ")
		line, eof, readErr := input.ReadLine(sessionCtx, int(service.Config.Manager.Ingress.MaximumMessageBytes))
		if readErr != nil {
			if sessionCtx.Err() != nil {
				endReason = managerdomain.EndReasonFromContext(sessionCtx)
			} else {
				endReason = managerdomain.EndRuntimeFailed
			}
			break
		}
		if eof {
			endReason = managerdomain.EndTerminalEOF
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "/quit" || trimmed == "/exit" || trimmed == "quit" || trimmed == "exit" {
			endReason = managerdomain.EndUserExit
			break
		}
		if strings.HasPrefix(line, "/") {
			exit, err := localDirective(sessionCtx, cmd, service, line, readiness, conversation != nil)
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
		finding := guard.Inspect(sessionCtx, managerdomain.ContentEnvelope{Source: managerdomain.SourceUser, SubjectID: subject.ID, ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, ContentType: "text/plain", ProvenanceID: "terminal-turn", RouteClass: "local", Content: []byte(line)})
		if sessionCtx.Err() != nil {
			endReason = managerdomain.EndReasonFromContext(sessionCtx)
			break
		}
		if finding.Decision == managerdomain.BlockSecret {
			fmt.Fprintln(cmd.OutOrStdout(), "Aegis blocked a possible credential.\nThe message was not sent to Hermes and was not retained in the transcript.\nStart protected intake instead? Use /secret put <reference>.")
			continue
		}
		if finding.Decision != managerdomain.AllowLocal {
			fmt.Fprintln(cmd.ErrOrStderr(), "Aegis blocked the message:", finding.Reason)
			continue
		}
		if conversation != nil {
			message, turnErr := conversation.session.Handle(sessionCtx, line)
			if turnErr != nil {
				if sessionCtx.Err() != nil {
					endReason = managerdomain.EndReasonFromContext(sessionCtx)
					break
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "Aegis denied the manager turn:", turnErr)
				continue
			}
			if message != "" {
				fmt.Fprintln(cmd.OutOrStdout(), message)
			}
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "The local Aegis management model is unavailable (%s). No cloud fallback was attempted.\nUse /help or /status. Next: %s\n", startupReason, readiness.nextStep(service.Config.StateDir))
	}
	if endReason == "" {
		endReason = managerdomain.EndRuntimeFailed
	}
	_ = lifecycle.RequestClose(endReason)
	cancelSession(endReasonCause(endReason))
	fmt.Fprintf(cmd.OutOrStdout(), "Shutting down Aegis manager (%s).\n", endReason)
	_ = lifecycle.Advance(managerdomain.LifecycleCleaning)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), service.Config.Manager.CleanupTimeout)
	defer cleanupCancel()
	var cleanupErr error
	if conversation != nil {
		cleanupErr = conversation.Close(cleanupCtx, endReason)
	} else {
		cleanupErr = service.AuditManagerSession(cleanupCtx, subject, "ok", endReason, map[string]string{"cleanup": "complete", "runtime": "degraded"})
	}
	if cleanupErr != nil {
		_ = lifecycle.Advance(managerdomain.LifecycleFailed)
		return fmt.Errorf("%s: bounded manager cleanup incomplete", managerdomain.ReasonCleanupIncomplete)
	}
	_ = lifecycle.Advance(managerdomain.LifecycleClosed)
	fmt.Fprintln(cmd.OutOrStdout(), "Aegis manager stopped; cleanup complete.")
	return nil
}

func localDirective(ctx context.Context, cmd *cobra.Command, service *app.Service, line string, readiness managerReadiness, conversational bool) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, nil
	}
	switch fields[0] {
	case "/quit", "/exit":
		return true, nil
	case "/help":
		commands := "/help /status /audit verify /clear /quit /exit (plain quit and exit also work)"
		if readiness.authority == "ready" {
			commands += " /secret list [query] /secret show <record-id>"
		} else {
			commands += "\nCredential metadata commands unavailable: credential authority prerequisite is " + readiness.authority
		}
		fmt.Fprintln(cmd.OutOrStdout(), commands)
	case "/status":
		fmt.Fprintf(cmd.OutOrStdout(), "Principal: authenticated\nCredential authority: %s\nModel: %s\nArtifact: %s\nCertification: %s\nHermes: %s\nInference: %s\nRoute: local-only\nCloud fallback: disabled\nModel switching: disabled\n", readiness.authority, readiness.model, readiness.artifact, readiness.certification, readiness.hermes, readiness.inference)
	case "/clear":
		if conversational {
			fmt.Fprintln(cmd.OutOrStdout(), "A clean Hermes conversation cannot be started in place; exit and start a clean manager session.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No Hermes conversation is active; local terminal display was not retained by Aegis.")
		}
	case "/audit":
		if len(fields) != 2 || fields[1] != "verify" {
			return false, errors.New("usage: /audit verify")
		}
		if err := service.VerifyAudit(ctx); err != nil {
			return false, err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Audit verification: valid")
	case "/secret":
		if readiness.authority != "ready" {
			return false, errors.New("credential authority is not ready; configure credentials.authority and run: aegis secret initialize")
		}
		if len(fields) < 2 {
			return false, errors.New("usage: /secret list [query] or /secret show <record-id>")
		}
		if fields[1] != "list" && fields[1] != "show" {
			return false, errors.New("protected mutations use deterministic aegis secret put|rotate|revoke subcommands in this build")
		}
		authority, closeAuthority, err := openAuthorityForService(ctx, service)
		if err != nil {
			return false, err
		}
		defer func() {
			if closeErr := closeAuthority(); closeErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: credential authority close failed\n", managerdomain.ReasonCleanupIncomplete)
			}
		}()
		if fields[1] == "show" {
			if len(fields) != 3 {
				return false, errors.New("usage: /secret show <record-id>")
			}
			record, err := authority.Metadata(ctx, fields[2])
			if err != nil {
				return false, err
			}
			return false, output(cmd, record)
		}
		if len(fields) > 3 {
			return false, errors.New("usage: /secret list [query]")
		}
		query := ""
		if len(fields) == 3 {
			query = fields[2]
		}
		records, err := authority.List(ctx, query, 50)
		if err != nil {
			return false, err
		}
		return false, output(cmd, map[string]any{"records": records, "count": len(records)})
	default:
		return false, errors.New("unrecognized local directive")
	}
	return false, nil
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
		return managerdomain.ReasonGatewayProtocol
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
