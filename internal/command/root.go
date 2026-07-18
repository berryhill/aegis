package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/api"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	credentialbroker "github.com/berryhill/aegis/internal/credentials/broker"
	"github.com/berryhill/aegis/internal/initialize"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/spf13/cobra"
)

type Dependencies struct {
	In          io.Reader
	Out, Err    io.Writer
	Logger      *slog.Logger
	Version     string
	IsTerminal  func(io.Reader, io.Writer) bool
	Updater     UpdateService
	Initializer *initialize.Service
}
type rootOptions struct{ configFile, stateDir, hermesExecutable, runtime string }

type UpdateService interface {
	Run(context.Context, bool) (selfupdate.Result, error)
}

type rootActionValue struct {
	name     string
	value    *bool
	selected *string
}

func (v *rootActionValue) Set(raw string) error {
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return err
	}
	if enabled && *v.selected != "" && *v.selected != v.name {
		return fmt.Errorf("conflicting root actions --%s and --%s", *v.selected, v.name)
	}
	*v.value = enabled
	if enabled {
		*v.selected = v.name
	}
	return nil
}
func (v *rootActionValue) String() string {
	if v.value != nil && *v.value {
		return "true"
	}
	return "false"
}
func (*rootActionValue) Type() string     { return "bool" }
func (*rootActionValue) IsBoolFlag() bool { return true }

func NewRoot(deps Dependencies) *cobra.Command {
	if deps.In == nil {
		deps.In = os.Stdin
	}
	if deps.Out == nil {
		deps.Out = os.Stdout
	}
	if deps.Err == nil {
		deps.Err = os.Stderr
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(deps.Err, nil))
	}
	if deps.IsTerminal == nil {
		deps.IsTerminal = terminalPair
	}
	if deps.Updater == nil {
		deps.Updater = selfupdate.New(deps.Version)
	}
	if deps.Initializer == nil {
		deps.Initializer = initialize.New()
	}
	o := &rootOptions{}
	var updateAlias, helpAction, versionAction bool
	var selectedAction string
	root := &cobra.Command{Use: "aegis", Short: "Authenticated trust-stanza sessions over explicit Hermes Agent runtimes", Version: deps.Version, Args: cobra.NoArgs, SilenceErrors: true, SilenceUsage: true}
	root.SetIn(deps.In)
	root.SetOut(deps.Out)
	root.SetErr(deps.Err)
	f := root.PersistentFlags()
	f.StringVar(&o.configFile, "config", "", "configuration file")
	f.StringVar(&o.stateDir, "state-dir", "", "Aegis state directory")
	f.StringVar(&o.hermesExecutable, "hermes-executable", "", "Hermes executable")
	f.StringVar(&o.runtime, "runtime", "", "explicit runtime (hermes)")
	f.Var(&rootActionValue{name: "update", value: &updateAlias, selected: &selectedAction}, "update", "update Aegis from the latest verified GitHub release")
	f.VarP(&rootActionValue{name: "help", value: &helpAction, selected: &selectedAction}, "help", "h", "help for aegis")
	f.Var(&rootActionValue{name: "version", value: &versionAction, selected: &selectedAction}, "version", "version for aegis")
	for _, name := range []string{"update", "help", "version"} {
		f.Lookup(name).NoOptDefVal = "true"
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "conflicting root actions") {
			return usage(err)
		}
		return err
	})
	build := func(cmd *cobra.Command) (*app.Service, error) {
		cfg, err := config.Load(o.configFile, nil)
		if err != nil {
			return nil, usage(err)
		}
		if cmd.Flags().Changed("state-dir") || cmd.InheritedFlags().Changed("state-dir") {
			cfg.StateDir = o.stateDir
		}
		if cmd.Flags().Changed("hermes-executable") || cmd.InheritedFlags().Changed("hermes-executable") {
			cfg.HermesExecutable = o.hermesExecutable
		}
		if o.runtime != "" && o.runtime != "hermes" {
			return nil, usage(fmt.Errorf("unsupported explicit runtime %q", o.runtime))
		}
		if err = cfg.Validate(); err != nil {
			return nil, usage(err)
		}
		st, err := store.OpenWithCheckpoints(cfg.StateDir, cfg.Audit.CheckpointDir)
		if err != nil {
			return nil, err
		}
		h := hermes.New(cfg.HermesExecutable, deps.Logger)
		service := app.New(cfg, st, h, deps.Logger)
		if err = service.RecoverProvisioning(cmd.Context()); err != nil {
			return nil, err
		}
		return service, nil
	}
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if updateAlias && cmd != root {
			return usage(errors.New("--update is valid only as a root action without a positional command"))
		}
		return nil
	}
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if updateAlias {
			return runUpdate(cmd, deps.Updater, false)
		}
		inspection := config.Inspect(o.configFile)
		if inspection.State != config.StateValid && inspection.State != config.StateAbsent && inspection.State != config.StatePartial {
			return usage(inspection.Failure())
		}
		if !deps.IsTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			if inspection.State == config.StateAbsent || inspection.State == config.StatePartial {
				if err := output(cmd, map[string]any{"state": inspection.State, "initialized": false, "reason": inspection.ReasonCode, "next_command": "aegis init", "exit_status": 2}); err != nil {
					return err
				}
				return usage(fmt.Errorf("%s: Aegis is uninitialized; run: aegis init", managerdomain.ReasonNotInitialized))
			}
			return usage(fmt.Errorf("%s: interactive manager mode requires stdin and stdout terminals; use deterministic subcommands such as aegis secret, aegis audit, or aegis config", managerdomain.ReasonRequiresTTY))
		}
		if inspection.State != config.StateValid {
			initialized, err := runFirstInitialization(cmd, deps.Initializer, o.configFile, o.stateDir)
			if err != nil || !initialized {
				return err
			}
		}
		return runManager(cmd, build)
	}
	root.AddCommand(managerCmd(build, deps.IsTerminal, deps.Initializer, o), initCmd(build, deps.IsTerminal, deps.Initializer, o), runtimeCmd(build, o), configCmd(build), charterCmd(build), designCmd(build), planCmd(build), approvalCmd(build), provisionCmd(build), sessionCmd(build), secretCmd(build), auditCmd(build), serveCmd(build), updateCmd(deps.Updater))
	return root
}

func runUpdate(cmd *cobra.Command, updater UpdateService, checkOnly bool) error {
	result, err := updater.Run(cmd.Context(), checkOnly)
	if err != nil {
		return err
	}
	return output(cmd, result)
}

func updateCmd(updater UpdateService) *cobra.Command {
	var checkOnly bool
	command := &cobra.Command{
		Use:   "update",
		Short: "Update Aegis from the latest verified GitHub release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd, updater, checkOnly)
		},
	}
	command.Flags().BoolVar(&checkOnly, "check", false, "check for an update without installing it")
	return command
}

type usageError struct{ error }

func usage(e error) error { return usageError{e} }
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var u usageError
	if errors.As(err, &u) {
		return 2
	}
	return 1
}
func RenderError(w io.Writer, err error) {
	if err != nil {
		fmt.Fprintln(w, "aegis:", err)
	}
}
func output(cmd *cobra.Command, v any) error {
	e := json.NewEncoder(cmd.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func env(name string) coreEnv { return coreEnv{Name: name} }

type coreEnv = struct {
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"`
	Tenant string `json:"tenant,omitempty"`
}

// envValue avoids importing policy into Cobra; conversion occurs via JSON-compatible fields.
func environment(name string) (out struct{ Name, Host, Tenant string }) { out.Name = name; return }

type builder func(*cobra.Command) (*app.Service, error)

func runtimeCmd(build builder, o *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "runtime", Short: "Discover explicit runtime adapters", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		r, e := s.Runtime(cmd.Context())
		if e != nil {
			return e
		}
		source := "configured_default"
		if o.runtime != "" {
			source = "explicit_cli"
		}
		return output(cmd, map[string]any{"resolved_runtime": r, "selection_source": source, "visible": true})
	}}
}
func configCmd(build builder) *cobra.Command {
	return &cobra.Command{Use: "config", Short: "Show effective redacted configuration", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		return output(cmd, config.Redacted(s.Config))
	}}
}

func charterCmd(build builder) *cobra.Command {
	c := &cobra.Command{Use: "charter", Short: "Validate and inspect canonical charters"}
	validate := &cobra.Command{Use: "validate FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		b, e := os.ReadFile(a[0])
		if e != nil {
			return e
		}
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.ValidateCharter(cmd.Context(), b)
		if e != nil {
			return usage(e)
		}
		return output(cmd, x)
	}}
	imp := &cobra.Command{Use: "import FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		b, e := os.ReadFile(a[0])
		if e != nil {
			return e
		}
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.ImportCharter(cmd.Context(), b)
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	list := &cobra.Command{Use: "list [AGENT]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		if len(a) == 0 {
			x, e := s.ListAgents()
			if e != nil {
				return e
			}
			return output(cmd, x)
		}
		x, e := s.ListCharters(a[0])
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	show := &cobra.Command{Use: "show AGENT [REVISION]", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		var rev uint64
		if len(a) == 2 {
			rev, e = strconv.ParseUint(a[1], 10, 64)
			if e != nil {
				return usage(e)
			}
		}
		x, e := s.GetCharter(a[0], rev)
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	var requested, environmentName string
	explain := &cobra.Command{Use: "explain AGENT [REVISION]", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		var rev uint64
		if len(a) == 2 {
			rev, e = strconv.ParseUint(a[1], 10, 64)
			if e != nil {
				return usage(e)
			}
		}
		d, e := s.Explain(cmd.Context(), a[0], rev, requested, coreEnvironment(environmentName))
		if outErr := output(cmd, d); outErr != nil {
			return outErr
		}
		return e
	}}
	explain.Flags().StringVar(&requested, "stanza", "", "requested stanza (request only; never authentication)")
	explain.Flags().StringVar(&environmentName, "environment", "local", "trusted environment name")
	var stanza string
	effective := &cobra.Command{Use: "effective AGENT [REVISION]", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		var rev uint64
		if len(a) == 2 {
			rev, e = strconv.ParseUint(a[1], 10, 64)
			if e != nil {
				return usage(e)
			}
		}
		digest, authority, decision, e := s.EffectiveAuthority(cmd.Context(), a[0], rev, stanza, coreEnvironment(environmentName))
		if e != nil {
			if outErr := output(cmd, map[string]any{"charter_digest": digest, "authority": authority, "decision": decision, "authority_not_unioned": true}); outErr != nil {
				return outErr
			}
			return e
		}
		return output(cmd, map[string]any{"charter_digest": digest, "authority": authority, "decision": decision, "authority_not_unioned": true})
	}}
	effective.Flags().StringVar(&stanza, "stanza", "", "stanza ID")
	effective.Flags().StringVar(&environmentName, "environment", "local", "trusted environment name")
	_ = effective.MarkFlagRequired("stanza")
	c.AddCommand(validate, imp, list, show, explain, effective)
	return c
}

func coreEnvironment(name string) core.Environment { return core.Environment{Name: name} }

func designCmd(build builder) *cobra.Command {
	var draft string
	var smoke bool
	c := &cobra.Command{Use: "design", Short: "Run a principal-only disposable Hermes design worker", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Runtime: Hermes Agent\nDesign mode: no provisioning capability\nIsolation: disposable HERMES_HOME (not a host filesystem sandbox)")
		var b []byte
		if draft != "" {
			b, e = os.ReadFile(draft)
			if e != nil {
				return e
			}
		}
		if len(b) == 0 {
			if !smoke {
				return usage(errors.New("design requires --draft requirements or --smoke"))
			}
			b = []byte("Propose a minimal valid Aegis charter for a principal-only demonstration agent.")
		}
		// Both modes use Hermes's documented TUI-gateway stdio protocol and
		// never invoke one-shot mode or import an unrelated charter file.
		x, e := s.DesignSmoke(cmd.Context(), b)
		if e != nil {
			return e
		}
		if draft == "" {
			return output(cmd, map[string]any{"status": "closed", "draft_saved": false, "mode": map[bool]string{true: "smoke", false: "interactive"}[smoke]})
		}
		return output(cmd, x)
	}}
	c.Flags().StringVar(&draft, "draft", "", "principal requirements text supplied to the isolated Hermes design protocol")
	c.Flags().BoolVar(&smoke, "smoke", false, "run the disposable Hermes gateway protocol non-interactively")
	return c
}

func planCmd(build builder) *cobra.Command {
	var rev uint64
	var envName string
	c := &cobra.Command{Use: "plan", Short: "Provisioning plans"}
	preview := &cobra.Command{Use: "preview AGENT", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.PreviewPlan(cmd.Context(), a[0], rev, coreEnvironment(envName))
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	preview.Flags().Uint64Var(&rev, "revision", 0, "charter revision (0 latest)")
	preview.Flags().StringVar(&envName, "environment", "local", "target environment")
	show := &cobra.Command{Use: "show PLAN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.GetPlan(a[0])
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	c.AddCommand(preview, show)
	return c
}
func approvalCmd(build builder) *cobra.Command {
	c := &cobra.Command{Use: "approval", Short: "Exact single-use approvals"}
	var ttl time.Duration
	req := &cobra.Command{Use: "request PLAN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.RequestApproval(cmd.Context(), a[0], ttl)
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	req.Flags().DurationVar(&ttl, "ttl", 5*time.Minute, "approval lifetime")
	decide := func(ok bool) *cobra.Command {
		verb := "approve"
		if !ok {
			verb = "reject"
		}
		return &cobra.Command{Use: verb + " APPROVAL_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			s, e := build(cmd)
			if e != nil {
				return e
			}
			x, e := s.DecideApproval(cmd.Context(), a[0], ok)
			if e != nil {
				return e
			}
			return output(cmd, x)
		}}
	}
	show := &cobra.Command{Use: "show APPROVAL_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.GetApproval(a[0])
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	c.AddCommand(req, decide(true), decide(false), show)
	return c
}
func provisionCmd(build builder) *cobra.Command {
	return &cobra.Command{Use: "provision PLAN_ID APPROVAL_ID", Short: "Apply an exact approved deterministic plan", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.Apply(cmd.Context(), a[0], a[1])
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
}

func sessionCmd(build builder) *cobra.Command {
	c := &cobra.Command{Use: "session", Short: "Preview, start, inspect, list, revoke, and terminate sessions"}
	var rev uint64
	var stanza, envName string
	preview := &cobra.Command{Use: "preview AGENT", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		m, d, e := s.PreviewSession(cmd.Context(), a[0], rev, stanza, coreEnvironment(envName))
		response := map[string]any{"authenticated_identity": m.Subject, "logical_agent": m.AgentID, "selected_stanza": m.StanzaID, "charter_revision": m.CharterRevision, "charter_digest": m.CharterDigest, "runtime": m.Runtime, "selection_source": "charter", "target": m.Target, "capabilities": m.Capabilities, "tools": m.Tools, "memory_scope": m.Scopes.Memory, "credential_scope": m.Scopes.Credentials, "expires_at": m.ExpiresAt, "warnings": []string{"Hermes state isolation is not host filesystem sandboxing"}, "mandate": m, "decision": d}
		if outErr := output(cmd, response); outErr != nil {
			return outErr
		}
		return e
	}}
	preview.Flags().Uint64Var(&rev, "revision", 0, "revision (0 latest)")
	preview.Flags().StringVar(&stanza, "stanza", "", "requested stanza; authorization still required")
	preview.Flags().StringVar(&envName, "environment", "local", "trusted environment")
	start := &cobra.Command{Use: "start MANDATE_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.StartSession(cmd.Context(), a[0])
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.ListSessions()
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	show := &cobra.Command{Use: "show SESSION_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, alive, e := s.InspectSession(a[0])
		if e != nil {
			return e
		}
		return output(cmd, map[string]any{"session": x, "runtime_process_alive": alive})
	}}
	end := func(revoke bool) *cobra.Command {
		verb := "terminate"
		if revoke {
			verb = "revoke"
		}
		var reason string
		x := &cobra.Command{Use: verb + " SESSION_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			s, e := build(cmd)
			if e != nil {
				return e
			}
			if revoke {
				e = s.RevokeSession(cmd.Context(), a[0], reason)
			} else {
				e = s.TerminateSession(cmd.Context(), a[0], reason)
			}
			if e != nil {
				return e
			}
			return output(cmd, map[string]string{"status": verb + "d"})
		}}
		x.Flags().StringVar(&reason, "reason", "operator_request", "machine-readable reason")
		return x
	}
	c.AddCommand(preview, start, list, show, end(true), end(false))
	return c
}
func auditCmd(build builder) *cobra.Command {
	c := &cobra.Command{Use: "audit", Short: "Inspect and verify tamper-evident audit"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		x, e := s.AuditEvents(cmd.Context())
		if e != nil {
			return e
		}
		return output(cmd, x)
	}}
	verify := &cobra.Command{Use: "verify", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		if e = s.VerifyAudit(cmd.Context()); e != nil {
			return e
		}
		return output(cmd, map[string]any{"valid": true})
	}}
	c.AddCommand(list, verify)
	return c
}
func serveCmd(build builder) *cobra.Command {
	return &cobra.Command{Use: "serve", Short: "Run the control plane and configured local credential broker in the foreground", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := build(cmd)
		if e != nil {
			return e
		}
		brokerConfig := s.Config.Credentials.Authority.Broker
		if brokerConfig.Socket == "" {
			return api.Serve(cmd.Context(), s)
		}
		authority, closeAuthority, e := openAuthorityForService(cmd.Context(), s)
		if e != nil {
			return e
		}
		defer closeAuthority()
		s.CredentialAuthority = authority
		destinations := make(map[string]string, len(brokerConfig.Destinations))
		var repositories []string
		for id, destination := range brokerConfig.Destinations {
			destinations[id] = destination.URL
			if id == credentialbroker.GitHubDestination {
				repositories = append([]string(nil), destination.Repositories...)
			}
		}
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		errorsOut := make(chan error, 2)
		go func() { errorsOut <- api.Serve(ctx, s) }()
		go func() {
			errorsOut <- credentialbroker.Serve(ctx, s, credentialbroker.ServerConfig{Socket: brokerConfig.Socket, AllowedUID: brokerConfig.AllowedUID, AllowedGID: brokerConfig.AllowedGID, MaxBodyBytes: brokerConfig.MaxBodyBytes, Timeout: brokerConfig.Timeout, Destinations: destinations, Repositories: repositories})
		}()
		e = <-errorsOut
		cancel()
		second := <-errorsOut
		if e != nil {
			return e
		}
		return second
	}}
}

// imported here to keep transport argument conversion explicit.
var _ = context.Canceled
