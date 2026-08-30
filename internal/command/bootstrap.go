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
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/initialize"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
)

// inspectOnboarding constructs only read-only discovery dependencies. It must
// remain usable before the application store can safely be opened.
func inspectOnboarding(ctx context.Context, configPath string, logger *slog.Logger, passphrase ...[]byte) onboarding.Snapshot {
	inspection := config.Inspect(configPath)
	executable := "hermes"
	if inspection.State == config.StateValid {
		executable = inspection.Config.HermesExecutable
	}
	inspector := onboarding.NewInspector(hermes.New(executable, logger))
	if len(passphrase) != 0 {
		inspector.WithAuthorityPassphrase(passphrase[0])
	}
	return inspector.Inspect(ctx, configPath)
}

// runBootstrap resumes the explicit manager onboarding flow at the first
// incomplete artifact-derived stage. Its bool result means the operator
// selected immediate manager launch after a freshly reverified ready state.
func runBootstrap(cmd *cobra.Command, build builder, initializer *initialize.Service, configPath, statePath string, logger *slog.Logger) (bool, error) {
	capabilities := tui.Detect(cmd.InOrStdin(), cmd.OutOrStdout(), os.Getenv)
	bootstrapView := newBootstrapPresentation(capabilities)
	terminalOutput := tui.NewSynchronizedWriter(cmd.OutOrStdout())
	cmd.SetOut(terminalOutput)
	presentation := tui.NewController(terminalOutput, capabilities, tui.SecurityContext{Principal: "pending", Stanza: managerdomain.SecurityContext, MandateState: "bootstrap", Runtime: "Hermes Agent", RuntimeState: "preflight", Route: "local-only", NoFallback: true})
	if err := presentation.Emit(tui.Event{Kind: tui.BootstrapInspectionStarted, Origin: tui.AegisAuthoritative, Message: "bootstrap inspection started; deterministic Aegis operations only"}); err != nil {
		return false, err
	}
	input := newTerminalInput(cmd.InOrStdin())
	var authorityPassphrase []byte
	defer wipeSecret(authorityPassphrase)
	fmt.Fprintln(cmd.OutOrStdout(), "AEGIS / bootstrap")
	fmt.Fprintln(cmd.OutOrStdout(), "Set up one authenticated, exact-local Aegis manager. Aegis verifies each result before continuing; the model never chooses or authorizes a step.")
	fmt.Fprintln(cmd.OutOrStdout(), "You can exit at any prompt and rerun 'aegis init'. Progress is derived from verified artifacts, so completed stages are not repeated.")
	bootstrapView.renderIntroduction(cmd)
	inspection := config.Inspect(configPath)
	if inspection.State == config.StateAbsent || inspection.State == config.StatePartial {
		renderOnboardingProgress(cmd, onboarding.Snapshot{State: onboarding.Uninitialized})
		initialized, err := runFirstInitializationWithInput(cmd, initializer, configPath, statePath, input, bootstrapView)
		if err != nil || !initialized {
			return false, err
		}
	}
	continued, err := reconcileOperationalAuthority(cmd, initializer, configPath, input, bootstrapView)
	if err != nil || !continued {
		return false, err
	}
	for attempts := 0; attempts < 12; attempts++ {
		snapshot := inspectOnboarding(cmd.Context(), configPath, logger, authorityPassphrase)
		if err := presentation.Emit(tui.Event{Kind: tui.BootstrapInspectionComplete, Origin: tui.AegisAuthoritative, Message: fmt.Sprintf("artifact-derived bootstrap state: %s (%s)", snapshot.State, snapshot.Reason)}); err != nil {
			return false, err
		}
		renderOnboardingProgress(cmd, snapshot)
		switch snapshot.State {
		case onboarding.Ready:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageComplete, Origin: tui.AegisAuthoritative, Stage: "bootstrap", Message: "all manager prerequisites verified"})
			renderReadiness(cmd, snapshot)
			registered, err := bootstrapBuiltInAegisAgent(cmd, build, input, bootstrapView)
			if err != nil || !registered {
				return false, err
			}
			approved, err := bootstrapView.approve(cmd, input, bootstrapDecision{
				Title:          "Start authenticated manager gateway",
				Recommendation: "Start only when interactive manager access is intended now; otherwise exit and resume later.",
				Consequence:    "Starts the local authenticated manager surface. The safe default exits without activation.",
				Details:        fmt.Sprintf("principal=%s; runtime=Hermes Agent %s; route=local-only; no fallback=true; exact model=%s @ %s", snapshot.Principal, snapshot.HermesVersion, snapshot.Model, snapshot.ModelDigest),
				DefaultDecline: true,
			})
			if err != nil {
				return false, err
			}
			return approved, nil
		case onboarding.RepairRequired:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageFailed, Origin: tui.AegisAuthoritative, Stage: "bootstrap", Reason: snapshot.Reason})
			return false, usage(fmt.Errorf("%s: %s; remediation: %s", snapshot.State, snapshot.Reason, snapshot.NextCommand))
		case onboarding.PrincipalConfigured:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "credential authority", Message: "credential authority setup or unlock required"})
			continued, err := bootstrapAuthority(cmd, build, input, snapshot, &authorityPassphrase, bootstrapView)
			if err != nil || !continued {
				return false, err
			}
		case onboarding.AuthorityConfigured:
			// Runtime checks are read-only. A missing or unsupported prerequisite
			// is external and must not be hidden by a weaker fallback.
			return false, nil
		case onboarding.RuntimeConfigured:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "exact local model", Message: "model route and exact artifact verification required"})
			continued, err := bootstrapModel(cmd, build, input, snapshot, bootstrapView)
			if err != nil || !continued {
				return false, err
			}
		case onboarding.ModelPresent:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "certification", Message: "exact Hermes to proxy to Ollama certification required"})
			continued, err := bootstrapCertification(cmd, build, input, snapshot, bootstrapView)
			if err != nil || !continued {
				return false, err
			}
		default:
			return false, usage(fmt.Errorf("bootstrap stopped in unsupported state %s", snapshot.State))
		}
	}
	return false, errors.New("bootstrap did not converge after bounded state transitions")
}

func ensureBuiltInAegisAgentRegistration(cmd *cobra.Command, build builder) (bool, error) {
	capabilities := tui.Detect(cmd.InOrStdin(), cmd.OutOrStdout(), os.Getenv)
	return bootstrapBuiltInAegisAgent(cmd, build, newTerminalInput(cmd.InOrStdin()), newBootstrapPresentation(capabilities))
}

type builtInAegisAgentService interface {
	GetFleetAgentAs(context.Context, core.Subject, string, uint64) (app.FleetAgent, error)
	RegisterBuiltInAegisAgentAs(context.Context, core.Subject) (app.FleetAgent, bool, error)
}

func bootstrapBuiltInAegisAgent(cmd *cobra.Command, build builder, input *terminalInput, view *bootstrapPresentation) (bool, error) {
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		return false, err
	}
	return bootstrapBuiltInAegisAgentWithService(cmd, service, subject, input, view)
}

func bootstrapBuiltInAegisAgentWithService(cmd *cobra.Command, service builtInAegisAgentService, subject core.Subject, input *terminalInput, view *bootstrapPresentation) (bool, error) {
	wantRegistration, wantRevision, err := registry.CanonicalBuiltInAegisAgent(subject.PrincipalID)
	if err != nil {
		return false, err
	}
	existing, loadErr := service.GetFleetAgentAs(cmd.Context(), subject, registry.BuiltInAegisAgentID, 1)
	if loadErr == nil {
		if !registry.CanonicalBuiltInAegisAgentMatches(existing.Registration, existing.Revision, subject.PrincipalID) {
			return false, fleet.ErrConflict
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Canonical built-in Aegis Agent already registered and exactly verified at %s.\n", existing.Revision.Digest)
		return true, nil
	}
	if !errors.Is(loadErr, fleet.ErrNotFound) {
		return false, loadErr
	}
	approved, err := view.approve(cmd, input, bootstrapDecision{
		Title:          "Register canonical built-in Aegis Agent",
		Recommendation: "Register the sealed Aegis-owned identity required for fleet control.",
		Consequence:    "Creates one immutable built-in Agent record after this authenticated approval. It does not create or retain a Hermes profile.",
		Details:        fmt.Sprintf("agent=%s; source=%s/%s/%s; owner=%s; accountable principal=%s; runtime=Hermes Agent; target=%s; registration revision digest=%s; charter field is a sealed system representation, not user-authored provenance", wantRegistration.AgentID, wantRegistration.Source.Kind, wantRegistration.Source.FleetID, wantRegistration.Source.SourceID, wantRevision.Ownership.OwnerID, wantRevision.Ownership.AccountabilityID, registry.BuiltInAegisRuntimeTarget, wantRevision.Digest),
		DefaultDecline: true,
	})
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Built-in Aegis Agent registration declined; no Agent record was created.")
		return false, err
	}
	stored, created, err := service.RegisterBuiltInAegisAgentAs(cmd.Context(), subject)
	if err != nil {
		return false, err
	}
	if !registry.CanonicalBuiltInAegisAgentMatches(stored.Registration, stored.Revision, subject.PrincipalID) {
		return false, fleet.ErrConflict
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Canonical built-in Aegis Agent registered=%t and exactly verified at %s.\n", created, stored.Revision.Digest)
	return true, nil
}

var onboardingStages = []string{
	"local identity and configuration",
	"credential authority",
	"Hermes and local Ollama runtime",
	"exact model binding",
	"end-to-end certification",
}

// renderOnboardingProgress shows only the current artifact-derived obligation.
// Verified stages are summarized rather than replayed on every transition.
func renderOnboardingProgress(cmd *cobra.Command, snapshot onboarding.Snapshot) {
	completed := onboardingCompletedStages(snapshot)
	if completed >= len(onboardingStages) {
		fmt.Fprintf(cmd.OutOrStdout(), "\nSetup progress  %d/%d verified — complete\n", completed, len(onboardingStages))
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nSetup progress  %d/%d verified\n  now            %s\n", completed, len(onboardingStages), onboardingStages[completed])
	if remaining := len(onboardingStages) - completed - 1; remaining > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  after this     %d stage(s) remain\n", remaining)
	}
	if snapshot.Reason != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  status         %s\n", snapshot.Reason)
	}
	for _, check := range snapshot.Checks {
		if check.Status == "verified" {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  evidence       %s: %s", check.Name, check.Status)
		if check.Reason != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", check.Reason)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		if check.Remedy != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "  next           "+check.Remedy)
		}
		break
	}
}

func onboardingCompletedStages(snapshot onboarding.Snapshot) int {
	switch snapshot.State {
	case onboarding.PrincipalConfigured:
		return 1
	case onboarding.AuthorityConfigured:
		return 2
	case onboarding.RuntimeConfigured:
		return 3
	case onboarding.ModelPresent, onboarding.ModelCertified:
		return 4
	case onboarding.Ready:
		return 5
	case onboarding.RepairRequired:
		return completedStagesFromChecks(snapshot.Checks)
	default:
		return 0
	}
}

func completedStagesFromChecks(checks []onboarding.Check) int {
	verified := make(map[string]bool, len(checks))
	for _, check := range checks {
		if check.Status == "verified" {
			verified[check.Name] = true
		}
	}
	completed := 0
	if !verified["principal"] {
		return completed
	}
	completed++
	if !verified["credential-authority"] {
		return completed
	}
	completed++
	if !verified["Hermes Agent"] || !verified["Ollama"] {
		return completed
	}
	completed++
	if !verified["exact-model"] {
		return completed
	}
	completed++
	if verified["certification"] {
		completed++
	}
	return completed
}

func bootstrapAuthority(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot, unlocked *[]byte, views ...*bootstrapPresentation) (bool, error) {
	view := bootstrapView(views)
	inspection := config.Inspect(snapshot.ConfigPath)
	if inspection.State == config.StateValid && inspection.Config.Credentials.Authority.Custody == "passphrase-file" {
		configured := inspection.Config.Credentials.Authority
		for attempt := 0; attempt < passphraseRetryLimit; attempt++ {
			passphrase, err := readAuthorityPassphrase(cmd, false)
			if err != nil {
				return false, err
			}
			custodian, loadErr := credentials.LoadPassphraseCustodian(configured.KEKFile, passphrase)
			if loadErr != nil {
				wipeSecret(passphrase)
				if !credentials.IsPassphraseAuthentication(loadErr) || attempt+1 == passphraseRetryLimit {
					return false, loadErr
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "Aegis: authority passphrase was not accepted; retrying protected input")
				continue
			}
			inspectErr := credentialbolt.Inspect(cmd.Context(), configured.Database, configured.DeploymentID, custodian)
			custodian.Close()
			if inspectErr != nil {
				wipeSecret(passphrase)
				return false, inspectErr
			}
			wipeSecret(*unlocked)
			*unlocked = passphrase
			fmt.Fprintln(cmd.OutOrStdout(), "Encrypted credential authority unlocked and verified for this process.")
			return true, nil
		}
		return false, credentials.ErrPassphraseAuthentication
	}
	if inspection.State == config.StateValid && inspection.Config.Credentials.Authority.Custody == "systemd" {
		authority := inspection.Config.Credentials.Authority
		fmt.Fprintln(cmd.OutOrStdout(), "\nCredential authority / systemd prerequisite")
		fmt.Fprintf(cmd.OutOrStdout(), "  deployment ID  %s\n  database       %s\n  credential     %s (from CREDENTIALS_DIRECTORY)\n", authority.DeploymentID, authority.Database, authority.KEKCredential)
		directory := strings.TrimSpace(os.Getenv("CREDENTIALS_DIRECTORY"))
		if directory == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "This foreground CLI was not launched with a systemd service credential, so that custody mode cannot complete here.")
			approved, err := view.approve(cmd, input, bootstrapDecision{
				Title:          "Replace unavailable systemd custody with encrypted local custody",
				Recommendation: "Use passphrase-encrypted local custody when this foreground process has no delivered systemd credential.",
				Consequence:    "Creates a new encrypted local authority only after a separate exact-plan approval. Declining preserves the existing systemd prerequisite.",
				Details:        fmt.Sprintf("configured custody=systemd; deployment ID=%s; database=%s; delivered credential=absent", authority.DeploymentID, authority.Database),
			})
			if err != nil || !approved {
				return false, err
			}
			return bootstrapPassphraseAuthority(cmd, build, snapshot, unlocked, view)
		}
		credential := filepath.Join(directory, authority.KEKCredential)
		if _, statErr := os.Lstat(credential); statErr != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "The delivered credential is not available at %s. Correct systemd credential delivery, then rerun aegis init. No database was created.\n", credential)
			return false, nil
		}
		approved, err := view.approve(cmd, input, bootstrapDecision{
			Title:          "Initialize systemd-backed credential authority",
			Recommendation: "Initialize only with the externally delivered credential already verified as available.",
			Consequence:    "Creates the deployment-bound mode-0600 authority database; the delivered credential is neither copied nor modified.",
			Details:        fmt.Sprintf("deployment ID=%s; database=%s; credential=%s from CREDENTIALS_DIRECTORY", authority.DeploymentID, authority.Database, authority.KEKCredential),
		})
		if err != nil || !approved {
			fmt.Fprintln(cmd.OutOrStdout(), "Systemd authority initialization declined; no database was created.")
			return false, err
		}
		revalidated := onboarding.NewInspector(nil).Inspect(cmd.Context(), snapshot.ConfigPath)
		if revalidated.State != onboarding.PrincipalConfigured || revalidated.Reason != "systemd_authority_prerequisite_incomplete" {
			return false, fmt.Errorf("principal/configuration changed after systemd authority preview: state=%s reason=%s", revalidated.State, revalidated.Reason)
		}
		if err = onboarding.InitializeConfiguredSystemdAuthority(cmd.Context(), snapshot.ConfigPath); err != nil {
			return false, err
		}
		service, subject, err := authenticatedService(cmd, build)
		if err != nil {
			return false, err
		}
		if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_authority_initialized", "ok", "systemd_authority_verified", ""); err != nil {
			return false, err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Systemd-custody authority initialized and verified.")
		return true, nil
	}
	custody, err := view.chooseCustody(cmd, input, bootstrapDecision{
		Title:          "Choose credential authority custody",
		Recommendation: "Use a passphrase-encrypted local key. It works in this terminal and the passphrase is never stored.",
		Consequence:    "The recommended route creates owner-only encrypted authority artifacts after exact approval. Declining or exiting performs no mutation.",
		Details:        "passphrase-file keeps the KEK encrypted at rest; systemd requires an externally delivered service credential; host-file is weaker development-only custody",
	})
	if err != nil {
		return false, err
	}
	if custody == "passphrase-file" {
		return bootstrapPassphraseAuthority(cmd, build, snapshot, unlocked, view)
	}
	if custody == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "Custody setup declined; no mutation was performed. Rerun 'aegis init' to resume.")
		return false, nil
	}
	plan, err := onboarding.PreviewAuthority(snapshot.ConfigPath, custody)
	if err != nil {
		return false, err
	}
	keyReference := plan.KEKFile
	if keyReference == "" {
		keyReference = plan.KEKCredential + " from CREDENTIALS_DIRECTORY"
	}
	approved, err := view.approve(cmd, input, bootstrapDecision{
		Title:          "Apply exact advanced custody plan",
		Recommendation: "Apply only the explicitly selected custody after reviewing its exact paths and digest transition.",
		Consequence:    "Creates authority artifacts with the selected custody. Declining performs no writes; host-file custody is explicitly weaker.",
		Details:        fmt.Sprintf("deployment ID=%s; database=%s; custody=%s; key reference=%s; ownership=authenticated OS principal; files=0600; directories=0700; config digest=%s -> %s; never back up a host-file KEK with authority.db; local root or a compromised account can defeat this boundary", plan.DeploymentID, plan.Database, plan.Custody, keyReference, plan.OriginalDigest, plan.ResultDigest),
	})
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Authority configuration declined; no writes were performed.")
		return false, err
	}
	// Reauthenticate from host-native account APIs immediately before mutation;
	// this read-only path must not construct stores or create audit artifacts.
	revalidated := onboarding.NewInspector(nil).Inspect(cmd.Context(), snapshot.ConfigPath)
	if revalidated.State != onboarding.PrincipalConfigured {
		return false, fmt.Errorf("principal/configuration changed after preview: state=%s reason=%s", revalidated.State, revalidated.Reason)
	}
	if custody == "systemd" {
		if err = onboarding.ApplyAuthority(plan); err != nil {
			return false, err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Systemd prerequisite recorded. Create and deliver encrypted credential %q to the Aegis service, set CREDENTIALS_DIRECTORY, then rerun aegis init. No KEK or database was created.\n", plan.KEKCredential)
		return false, nil
	}
	if err = onboarding.InitializeHostAuthority(cmd.Context(), plan); err != nil {
		return false, err
	}
	if err = onboarding.ApplyAuthority(plan); err != nil {
		onboarding.CleanupHostAuthority(plan)
		return false, err
	}
	service, subject, auditErr := authenticatedService(cmd, build)
	if auditErr == nil {
		auditErr = service.AuditCredentialOperation(cmd.Context(), subject, "credential_authority_initialized", "ok", "host_file_authority_verified", "")
	}
	if auditErr != nil {
		return false, auditErr
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Host-file authority initialized and verified (explicitly weaker development custody).")
	return true, nil
}

func bootstrapPassphraseAuthority(cmd *cobra.Command, build builder, snapshot onboarding.Snapshot, unlocked *[]byte, views ...*bootstrapPresentation) (bool, error) {
	view := bootstrapView(views)
	plan, err := onboarding.PreviewAuthority(snapshot.ConfigPath, "passphrase-file")
	if err != nil {
		return false, err
	}
	approved, err := view.approve(cmd, newTerminalInput(cmd.InOrStdin()), bootstrapDecision{
		Title:          "Create encrypted credential authority",
		Recommendation: "Use the passphrase-encrypted local authority for this terminal-managed installation.",
		Consequence:    "Creates owner-only authority artifacts. The passphrase is never stored; losing it makes the authority unrecoverable without a separate recovery mechanism.",
		Details:        fmt.Sprintf("deployment ID=%s; database=%s; encrypted KEK=%s; encryption=Argon2id + XChaCha20-Poly1305; files=0600; directories=0700; config digest=%s -> %s", plan.DeploymentID, plan.Database, plan.KEKFile, plan.OriginalDigest, plan.ResultDigest),
	})
	if err != nil || !approved {
		return false, err
	}
	passphrase, err := readAuthorityPassphrase(cmd, true)
	if err != nil {
		return false, err
	}
	if err = onboarding.InitializePassphraseAuthority(cmd.Context(), plan, passphrase); err != nil {
		wipeSecret(passphrase)
		return false, err
	}
	if err = onboarding.ApplyAuthority(plan); err != nil {
		onboarding.CleanupAuthority(plan)
		wipeSecret(passphrase)
		return false, err
	}
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		wipeSecret(passphrase)
		return false, err
	}
	if err = service.AuditCredentialOperation(cmd.Context(), subject, "credential_authority_initialized", "ok", "passphrase_encrypted_authority_verified", ""); err != nil {
		wipeSecret(passphrase)
		return false, err
	}
	wipeSecret(*unlocked)
	*unlocked = passphrase
	fmt.Fprintln(cmd.OutOrStdout(), "Passphrase-encrypted authority initialized, unlocked, and verified.")
	return true, nil
}

func readAuthorityPassphrase(cmd *cobra.Command, confirm bool) ([]byte, error) {
	provider, err := passphraseProvider(cmd)
	if err != nil {
		return nil, err
	}
	intent := AuthorityPassphraseUnlock
	if confirm {
		intent = AuthorityPassphraseCreate
	}
	return provider.Acquire(cmd.Context(), AuthorityPassphraseRequest{Intent: intent, Input: cmd.InOrStdin(), Diagnostic: cmd.ErrOrStderr()})
}

func readDefaultYes(cmd *cobra.Command, input *terminalInput) (bool, error) {
	answer, eof, err := readBootstrapLine(cmd, input, 128)
	if err != nil || eof {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		fmt.Fprintln(cmd.OutOrStdout(), "Unrecognized answer; cancelled without mutation.")
		return false, nil
	}
}

func bootstrapModel(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot, views ...*bootstrapPresentation) (bool, error) {
	view := bootstrapView(views)
	service, subject, err := authenticatedService(cmd, build)
	if err != nil {
		return false, err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nOllama deployment")
	fmt.Fprintln(cmd.OutOrStdout(), "Aegis will inspect an explicit operator-managed loopback daemon. It will not start, stop, replace, or take ownership of that daemon.")
	fmt.Fprint(cmd.OutOrStdout(), "Loopback endpoint [http://127.0.0.1:11434]: ")
	endpoint, eof, err := readBootstrapLine(cmd, input, 512)
	if err != nil || eof {
		return false, err
	}
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	report, err := managerdomain.DiscoverInstalledCandidates(cmd.Context(), endpoint, service.Config.Manager.Inference.RequestTimeout)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Ollama %s at %s\nBoundary: %s\n", report.Version, report.Endpoint, report.Boundary)
	renderInstalledCandidates(cmd, report.Installed)
	if len(report.Installed) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No approved installed artifact is visible. No candidate is selected by default.")
		for index, candidate := range managerdomain.Candidates() {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%d] %s (%s, %s) %s\n", index+1, candidate.ID, candidate.Publisher, candidate.License, candidate.OllamaName)
		}
		fmt.Fprint(cmd.OutOrStdout(), "Download one exact registry candidate now? Enter its number, or press Enter to exit: ")
		choice, ended, readErr := readBootstrapLine(cmd, input, 32)
		if readErr != nil || ended || choice == "" {
			return false, readErr
		}
		index := parseMenuIndex(choice, len(managerdomain.Candidates()))
		if index < 0 {
			return false, usage(errors.New("candidate selection is outside the closed registry"))
		}
		candidate := managerdomain.Candidates()[index]
		approved, readErr := view.approve(cmd, input, bootstrapDecision{
			Title:          "Download exact registry model candidate",
			Recommendation: "Download only the selected closed-registry candidate to the explicit operator-managed Ollama daemon.",
			Consequence:    "Requests a network download into operator-managed Ollama. Declining requests no network mutation; success is rediscovered and bound by exact sha256 digest.",
			Details:        fmt.Sprintf("action=POST %s/api/pull; artifact=%s; store owner=operator-managed Ollama at %s; publisher/source=%s / %s; license/terms=%s / %s; size=reported during transfer; mutable name is never identity", endpoint, candidate.OllamaName, endpoint, candidate.Publisher, candidate.Source, candidate.License, candidate.LicenseURL),
		})
		if readErr != nil || !approved {
			fmt.Fprintln(cmd.OutOrStdout(), "Download declined; no network mutation was requested.")
			return false, readErr
		}
		service, subject, err = authenticatedService(cmd, build)
		if err != nil {
			return false, err
		}
		client, clientErr := managerdomain.NewOllamaClient(endpoint, service.Config.Manager.Inference.RequestTimeout)
		if clientErr != nil {
			return false, clientErr
		}
		last, lastPercent := "", -1
		started := time.Now()
		if err = client.Pull(cmd.Context(), candidate.OllamaName, func(progress managerdomain.PullProgress) {
			if progress.Total > 0 {
				percent := int(float64(progress.Completed) / float64(progress.Total) * 100)
				if percent != lastPercent {
					elapsed := time.Since(started).Seconds()
					rate, eta := float64(progress.Completed)/max(elapsed, 0.001), "calculating"
					if rate > 0 {
						eta = (time.Duration(float64(progress.Total-progress.Completed)/rate) * time.Second).Round(time.Second).String()
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  download: %s %d%% (%d/%d bytes, %.0f bytes/s, ETA %s)\n", progress.Status, percent, progress.Completed, progress.Total, rate, eta)
					lastPercent = percent
				}
			} else if progress.Status != "" && progress.Status != last {
				fmt.Fprintln(cmd.OutOrStdout(), "  download:", progress.Status)
			}
			last = progress.Status
		}); err != nil {
			auditErr := service.AuditManagerOnboarding(cmd.Context(), subject, "model_pull", "denied", "pull_failed_or_cancelled", map[string]string{"candidate_id": candidate.ID, "endpoint": endpoint})
			return false, errors.Join(err, auditErr)
		}
		if err = service.AuditManagerOnboarding(cmd.Context(), subject, "model_pull", "ok", "pull_completed", map[string]string{"candidate_id": candidate.ID, "endpoint": endpoint}); err != nil {
			return false, err
		}
		for attempt := 0; attempt < 8; attempt++ {
			report, err = managerdomain.DiscoverInstalledCandidates(cmd.Context(), endpoint, service.Config.Manager.Inference.RequestTimeout)
			if err == nil && len(report.Installed) > 0 {
				break
			}
			select {
			case <-cmd.Context().Done():
				return false, cmd.Context().Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		if err != nil {
			return false, err
		}
		if len(report.Installed) == 0 {
			return false, errors.New("download completed but the approved artifact was not visible during bounded rediscovery; rerun aegis init")
		}
	}
	selected, selectedOK, err := selectInstalledCandidate(cmd, input, report.Installed)
	if err != nil || !selectedOK {
		return false, err
	}
	preview, err := managerdomain.PreviewExternalModelConfiguration(snapshot.ConfigPath, service.Config.StateDir, "", selected)
	if err != nil {
		return false, err
	}
	approved, err := view.approve(cmd, input, bootstrapDecision{
		Title:          "Bind exact local model",
		Recommendation: "Bind the selected verified installed artifact to the explicit loopback route.",
		Consequence:    "Writes the exact digest-bound model configuration. No cloud fallback, model switching, or artifact copy is enabled.",
		Details:        fmt.Sprintf("model=%s; digest=%s; endpoint=%s; certification=%s", preview.Model, preview.Digest, preview.Endpoint, preview.Certification),
	})
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Model binding declined; no configuration write was performed.")
		return false, err
	}
	service, subject, err = authenticatedService(cmd, build)
	if err != nil {
		return false, err
	}
	if err = managerdomain.ApplyModelConfiguration(preview); err != nil {
		return false, err
	}
	if err = service.AuditManagerOnboarding(cmd.Context(), subject, "model_bound", "ok", "exact_artifact_bound", map[string]string{"candidate_id": selected.Candidate.ID, "model_digest": selected.Digest, "endpoint": endpoint}); err != nil {
		return false, err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Exact local artifact configured; certification is still required before readiness.")
	return true, nil
}

func selectInstalledCandidate(cmd *cobra.Command, input *terminalInput, installed []managerdomain.InstalledCandidate) (managerdomain.InstalledCandidate, bool, error) {
	if len(installed) == 0 {
		return managerdomain.InstalledCandidate{}, false, errors.New("no installed candidate is available for selection")
	}
	if len(installed) == 1 {
		fmt.Fprintf(cmd.OutOrStdout(), "Only one approved installed candidate found; selected automatically: %s\n", installed[0].Candidate.ID)
		return installed[0], true, nil
	}
	fmt.Fprint(cmd.OutOrStdout(), "Select one installed candidate number (no default): ")
	choice, eof, err := readBootstrapLine(cmd, input, 32)
	if err != nil || eof || choice == "" {
		return managerdomain.InstalledCandidate{}, false, err
	}
	index := parseMenuIndex(choice, len(installed))
	if index < 0 {
		return managerdomain.InstalledCandidate{}, false, usage(errors.New("installed candidate selection is invalid"))
	}
	return installed[index], true, nil
}

func renderInstalledCandidates(cmd *cobra.Command, installed []managerdomain.InstalledCandidate) {
	for index, candidate := range installed {
		label := ""
		if len(installed) > 1 {
			label = fmt.Sprintf("[%d] ", index+1)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s%s\n      Ollama name: %s\n      publisher/source: %s / %s\n      license/terms: %s / %s\n      digest: %s\n      artifact size: %d bytes\n      quantization: %s\n      context: %d\n      capabilities: %s\n", label, candidate.Candidate.ID, candidate.Candidate.OllamaName, candidate.Candidate.Publisher, candidate.Candidate.Source, candidate.Candidate.License, candidate.Candidate.LicenseURL, candidate.Digest, candidate.Artifact.Size, candidate.Artifact.Details.QuantizationLevel, candidate.Artifact.Details.ContextLength, strings.Join(candidate.Artifact.Capabilities, ", "))
	}
}

func bootstrapCertification(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot, views ...*bootstrapPresentation) (bool, error) {
	view := bootstrapView(views)
	candidate := "CANDIDATE_ID"
	for _, item := range managerdomain.Candidates() {
		if item.OllamaName == snapshot.Model {
			candidate = item.ID
		}
	}
	approved, err := view.approve(cmd, input, bootstrapDecision{
		Title:          "Run end-to-end certification",
		Recommendation: "Run now only when this workstation can sustain the exact local model workload.",
		Consequence:    "May use substantial CPU, GPU, RAM, and time. Declining saves no certification; rerunning 'aegis init' resumes from verified artifacts.",
		Details:        fmt.Sprintf("candidate=%s; path=Hermes Agent -> authenticated Aegis proxy -> Ollama; every named corpus case must pass; all runtime resources unload afterward", candidate),
	})
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Certification declined; readiness was not reported.")
		return false, err
	}
	err = runManagerCertification(cmd, build, candidate, func(stage string) {
		fmt.Fprintln(cmd.OutOrStdout(), "  conformance:", stage)
	}, false)
	if err != nil {
		return false, fmt.Errorf("%w; certification was not saved; retry all cases with: aegis manager certify %s --continue-on-error", err, candidate)
	}
	return true, nil
}

func renderReadiness(cmd *cobra.Command, snapshot onboarding.Snapshot) {
	digest := snapshot.ModelDigest
	if len(digest) > 19 {
		digest = digest[:19] + "..."
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nREADY / verified from artifacts\n  authenticated principal  %s\n  credential authority     verified\n  Hermes Agent             %s (%s)\n  Ollama route             %s\n  exact model              %s @ %s\n  certification            valid\n  cloud fallback           disabled\n  model switching          disabled\n  isolation limitation     disposable runtime state is not host sandboxing\n  full digest              aegis manager model status\n", snapshot.Principal, snapshot.HermesPath, snapshot.HermesVersion, snapshot.OllamaRoute, snapshot.Model, digest)
}

func readBootstrapLine(cmd *cobra.Command, input *terminalInput, maximum int) (string, bool, error) {
	line, eof, err := input.ReadLine(cmd.Context(), maximum)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return "", true, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(line), eof, nil
}

func parseMenuIndex(value string, maximum int) int {
	index := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return -1
		}
		index = index*10 + int(r-'0')
	}
	if index < 1 || index > maximum {
		return -1
	}
	return index - 1
}
