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

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/initialize"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/onboarding"
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

// runBootstrap resumes at the first incomplete artifact-derived stage. Its
// bool result means the operator selected immediate manager launch after a
// freshly reverified ready state.
func runBootstrap(cmd *cobra.Command, build builder, initializer *initialize.Service, configPath, statePath string, logger *slog.Logger) (bool, error) {
	capabilities := tui.Detect(cmd.InOrStdin(), cmd.OutOrStdout(), os.Getenv)
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
	inspection := config.Inspect(configPath)
	if inspection.State == config.StateAbsent || inspection.State == config.StatePartial {
		renderOnboardingProgress(cmd, onboarding.Snapshot{State: onboarding.Uninitialized})
		initialized, err := runFirstInitializationWithInput(cmd, initializer, configPath, statePath, input)
		if err != nil || !initialized {
			return false, err
		}
	}
	continued, err := reconcileOperationalAuthority(cmd, initializer, configPath, input)
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
			fmt.Fprint(cmd.OutOrStdout(), "Start the Aegis manager TUI now? [1] start  [2] exit (safe default): ")
			answer, eof, err := readBootstrapLine(cmd, input, 32)
			if err != nil {
				return false, err
			}
			return !eof && (answer == "1" || answer == "start"), nil
		case onboarding.RepairRequired:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageFailed, Origin: tui.AegisAuthoritative, Stage: "bootstrap", Reason: snapshot.Reason})
			return false, usage(fmt.Errorf("%s: %s; remediation: %s", snapshot.State, snapshot.Reason, snapshot.NextCommand))
		case onboarding.PrincipalConfigured:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "credential authority", Message: "credential authority setup or unlock required"})
			continued, err := bootstrapAuthority(cmd, build, input, snapshot, &authorityPassphrase)
			if err != nil || !continued {
				return false, err
			}
		case onboarding.AuthorityConfigured:
			// Runtime checks are read-only. A missing or unsupported prerequisite
			// is external and must not be hidden by a weaker fallback.
			return false, nil
		case onboarding.RuntimeConfigured:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "exact local model", Message: "model route and exact artifact verification required"})
			continued, err := bootstrapModel(cmd, build, input, snapshot)
			if err != nil || !continued {
				return false, err
			}
		case onboarding.ModelPresent:
			_ = presentation.Emit(tui.Event{Kind: tui.BootstrapStageStarted, Origin: tui.AegisAuthoritative, Stage: "certification", Message: "exact Hermes to proxy to Ollama certification required"})
			continued, err := bootstrapCertification(cmd, build, input, snapshot)
			if err != nil || !continued {
				return false, err
			}
		default:
			return false, usage(fmt.Errorf("bootstrap stopped in unsupported state %s", snapshot.State))
		}
	}
	return false, errors.New("bootstrap did not converge after bounded state transitions")
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

func bootstrapAuthority(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot, unlocked *[]byte) (bool, error) {
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
			fmt.Fprint(cmd.OutOrStdout(), "Create a passphrase-encrypted local authority and continue? [Y/n]: ")
			approved, err := readDefaultYes(cmd, input)
			if err != nil || !approved {
				return false, err
			}
			return bootstrapPassphraseAuthority(cmd, build, snapshot, unlocked)
		}
		credential := filepath.Join(directory, authority.KEKCredential)
		if _, statErr := os.Lstat(credential); statErr != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "The delivered credential is not available at %s. Correct systemd credential delivery, then rerun aegis init. No database was created.\n", credential)
			return false, nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "The externally delivered KEK is available. Aegis will create the deployment-bound mode-0600 authority database; it will not copy or modify the credential.")
		fmt.Fprint(cmd.OutOrStdout(), "Initialize and verify the systemd-backed authority? [Y/n]: ")
		approved, err := readDefaultYes(cmd, input)
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
	fmt.Fprintln(cmd.OutOrStdout(), "\nCredential authority")
	fmt.Fprintln(cmd.OutOrStdout(), "Recommended: a passphrase-encrypted local key. It works in this terminal and the passphrase is never stored.")
	fmt.Fprint(cmd.OutOrStdout(), "Use the recommended custody? [Y/n/advanced]: ")
	answer, eof, err := readBootstrapLine(cmd, input, 32)
	if err != nil || eof || answer == "exit" {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" || answer == "y" || answer == "yes" || answer == "1" || answer == "passphrase-file" {
		return bootstrapPassphraseAuthority(cmd, build, snapshot, unlocked)
	}
	if answer == "n" || answer == "no" {
		fmt.Fprintln(cmd.OutOrStdout(), "Custody setup declined; no mutation was performed. Rerun 'aegis init' to resume.")
		return false, nil
	}
	if answer != "advanced" && answer != "a" {
		fmt.Fprintln(cmd.OutOrStdout(), "No valid custody choice selected; no mutation was performed. Rerun 'aegis init' to resume.")
		return false, nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nAdvanced custody")
	fmt.Fprintln(cmd.OutOrStdout(), "  [1] systemd service credential (must already be delivered by a service unit)")
	fmt.Fprintln(cmd.OutOrStdout(), "  [2] plaintext host file (development only; weaker)")
	fmt.Fprintln(cmd.OutOrStdout(), "  [3] exit without mutation")
	fmt.Fprint(cmd.OutOrStdout(), "Select: ")
	answer, eof, err = readBootstrapLine(cmd, input, 32)
	if err != nil || eof || answer == "3" || answer == "exit" {
		return false, err
	}
	custody := ""
	switch answer {
	case "1", "systemd":
		custody = "systemd"
	case "2", "host-file":
		custody = "host-file"
	default:
		fmt.Fprintln(cmd.OutOrStdout(), "No valid custody choice selected; no mutation performed.")
		return false, nil
	}
	plan, err := onboarding.PreviewAuthority(snapshot.ConfigPath, custody)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nExact authority plan\n  deployment ID  %s\n  database       %s\n  custody        %s\n", plan.DeploymentID, plan.Database, plan.Custody)
	if plan.KEKFile != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  KEK file       %s\n", plan.KEKFile)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "  credential     %s (from CREDENTIALS_DIRECTORY)\n", plan.KEKCredential)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "  ownership      authenticated OS principal; files 0600; directories 0700")
	fmt.Fprintln(cmd.OutOrStdout(), "  backup warning never back up a host-file KEK with authority.db")
	fmt.Fprintln(cmd.OutOrStdout(), "  limitation     local root or a compromised account can defeat this boundary")
	fmt.Fprintln(cmd.OutOrStdout(), "  config digest  ", plan.OriginalDigest, "->", plan.ResultDigest)
	fmt.Fprint(cmd.OutOrStdout(), "Apply this authority plan? [Y/n]: ")
	approved, err := readDefaultYes(cmd, input)
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

func bootstrapPassphraseAuthority(cmd *cobra.Command, build builder, snapshot onboarding.Snapshot, unlocked *[]byte) (bool, error) {
	plan, err := onboarding.PreviewAuthority(snapshot.ConfigPath, "passphrase-file")
	if err != nil {
		return false, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nEncrypted authority plan\n  deployment ID  %s\n  database       %s\n  encrypted KEK  %s\n  encryption     Argon2id + XChaCha20-Poly1305\n  files          0600\n  directories    0700\n  config digest  %s -> %s\n", plan.DeploymentID, plan.Database, plan.KEKFile, plan.OriginalDigest, plan.ResultDigest)
	fmt.Fprintln(cmd.OutOrStdout(), "The passphrase is never written to disk. Losing it makes the encrypted authority unrecoverable without a separate recovery mechanism.")
	fmt.Fprint(cmd.OutOrStdout(), "Create and verify this encrypted authority? [Y/n]: ")
	approved, err := readDefaultYes(cmd, newTerminalInput(cmd.InOrStdin()))
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

func bootstrapModel(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot) (bool, error) {
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
		fmt.Fprintf(cmd.OutOrStdout(), "Network action: POST %s/api/pull\nExpected artifact: %s\nStore/owner: operator-managed Ollama at %s\nPublisher/source: %s / %s\nLicense/terms: %s / %s\nSize: reported by Ollama during transfer\nDigest policy: rediscover and bind the exact resulting sha256 digest; the mutable name is never identity.\nDownload this model? [Y/n]: ", endpoint, candidate.OllamaName, endpoint, candidate.Publisher, candidate.Source, candidate.License, candidate.LicenseURL)
		approved, readErr := readDefaultYes(cmd, input)
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
	fmt.Fprintf(cmd.OutOrStdout(), "Exact configuration: model=%s digest=%s endpoint=%s certification=%s\nNo cloud fallback. No model switching. No copy.\nApply this exact digest-bound model configuration? [Y/n]: ", preview.Model, preview.Digest, preview.Endpoint, preview.Certification)
	approved, err := readDefaultYes(cmd, input)
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

func bootstrapCertification(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot) (bool, error) {
	candidate := "CANDIDATE_ID"
	for _, item := range managerdomain.Candidates() {
		if item.OllamaName == snapshot.Model {
			candidate = item.ID
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nCertification runs the complete Hermes Agent -> authenticated Aegis proxy -> Ollama conformance path.")
	fmt.Fprintln(cmd.OutOrStdout(), "It loads the exact model, may use substantial CPU/GPU/RAM, runs every named corpus case, and unloads all runtime resources afterward.")
	fmt.Fprint(cmd.OutOrStdout(), "Run certification now? [Y/n]: ")
	approved, err := readDefaultYes(cmd, input)
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
