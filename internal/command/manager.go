package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	command.AddCommand(managerCertifyCmd(build))
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
	confirmation, err := readConfirmation(cmd.InOrStdin())
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(cmd.OutOrStdout(), "\nInitialization declined; no writes were performed.")
			return false, nil
		}
		return false, err
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

func readConfirmation(input io.Reader) (string, error) {
	var value strings.Builder
	one := make([]byte, 1)
	for value.Len() <= 16 {
		count, err := input.Read(one)
		if count == 1 {
			if one[0] == '\n' {
				return strings.TrimSpace(value.String()), nil
			}
			if one[0] != '\r' {
				value.WriteByte(one[0])
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && value.Len() != 0 {
				return strings.TrimSpace(value.String()), nil
			}
			return "", err
		}
	}
	return "", errors.New("initialization confirmation is too long")
}

func runManager(cmd *cobra.Command, build builder) error {
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		return err
	}
	guard, err := managerdomain.NewGuard(int(service.Config.Manager.Ingress.MaximumMessageBytes), service.Config.Manager.Ingress.MaximumMessageRunes, service.Config.Manager.Ingress.BoundedDecodeDepth, service.Config.Manager.Ingress.ScanTimeout)
	if err != nil {
		return err
	}
	model := service.Config.Manager.Inference.Model
	digest := service.Config.Manager.Inference.ModelDigest
	if model == "" {
		model, digest = "not configured", "not certified"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis manager\nPrincipal: %s\nRuntime: Hermes Agent\nInference: Ollama local / %s@%s\nSecurity context: %s\nCloud fallback: disabled\nModel switching: disabled\nRuntime-state isolation is not host sandboxing.\nType /help for local commands.\n", subject.PrincipalID, model, digest, managerdomain.SecurityContext)
	conversation, startupErr := startConversationalManager(cmd.Context(), service, subject, guard, cmd)
	if startupErr != nil {
		if auditErr := service.AuditManagerSession(cmd.Context(), subject, "denied", "startup_failed", map[string]string{"reason": "manager_startup_failed", "cleanup": "rollback_attempted"}); auditErr != nil {
			return auditErr
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Conversational local inference unavailable. Reason: %s\nDeterministic manager commands remain available; no cloud fallback or alternate model was attempted.\n", startupErr)
	} else {
		defer conversation.Close(context.Background(), "terminal_closed")
		fmt.Fprintln(cmd.OutOrStdout(), "Conversational session: active; route: authenticated loopback proxy to exact certified model")
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	scanner.Buffer(make([]byte, 4096), int(service.Config.Manager.Ingress.MaximumMessageBytes)+1)
	for {
		fmt.Fprint(cmd.OutOrStdout(), "aegis> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "/") {
			exit, err := localDirective(cmd.Context(), cmd, service, line)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "aegis:", err)
				continue
			}
			if exit {
				return nil
			}
			continue
		}
		finding := guard.Inspect(cmd.Context(), managerdomain.ContentEnvelope{Source: managerdomain.SourceUser, SubjectID: subject.ID, ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, ContentType: "text/plain", ProvenanceID: "terminal-turn", RouteClass: "local", Content: []byte(line)})
		if finding.Decision == managerdomain.BlockSecret {
			fmt.Fprintln(cmd.OutOrStdout(), "Aegis blocked a possible credential.\nThe message was not sent to Hermes and was not retained in the transcript.\nStart protected intake instead? Use /secret put <reference>.")
			continue
		}
		if finding.Decision != managerdomain.AllowLocal {
			fmt.Fprintln(cmd.ErrOrStderr(), "Aegis blocked the message:", finding.Reason)
			continue
		}
		if conversation != nil {
			message, turnErr := conversation.session.Handle(cmd.Context(), line)
			if turnErr != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "Aegis denied the manager turn:", turnErr)
				continue
			}
			if message != "" {
				fmt.Fprintln(cmd.OutOrStdout(), message)
			}
			continue
		}
		fmt.Fprintln(cmd.OutOrStdout(), "The local Aegis management model is unavailable. No cloud fallback was attempted.\nAvailable deterministic commands: /secret put, /secret show, /secret list, /secret rotate, /secret revoke, /audit verify")
	}
}

func localDirective(ctx context.Context, cmd *cobra.Command, service *app.Service, line string) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, nil
	}
	switch fields[0] {
	case "/quit", "/exit":
		return true, nil
	case "/help":
		fmt.Fprintln(cmd.OutOrStdout(), "/help /status /secret list /secret show <record-id> /secret put <reference> /secret rotate <record-id> /secret revoke <record-id> /audit verify /clear /quit /exit")
	case "/status":
		fmt.Fprintln(cmd.OutOrStdout(), "Aegis manager: active; context: secrets-manager; route: local-only; fallback: disabled")
	case "/clear":
		fmt.Fprintln(cmd.OutOrStdout(), "No Hermes conversation is active; local terminal display was not retained by Aegis.")
	case "/audit":
		if len(fields) != 2 || fields[1] != "verify" {
			return false, errors.New("usage: /audit verify")
		}
		if err := service.VerifyAudit(ctx); err != nil {
			return false, err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Audit verification: valid")
	case "/secret":
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
		defer closeAuthority()
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
