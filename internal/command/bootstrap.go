package command

import (
	"bytes"
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
	fmt.Fprintln(cmd.OutOrStdout(), "Deterministic local setup. The model does not choose or authorize any step.")
	inspection := config.Inspect(configPath)
	if inspection.State == config.StateAbsent || inspection.State == config.StatePartial {
		initialized, err := runFirstInitializationWithInput(cmd, initializer, configPath, statePath, input)
		if err != nil || !initialized {
			return false, err
		}
	}

	for attempts := 0; attempts < 12; attempts++ {
		snapshot := inspectOnboarding(cmd.Context(), configPath, logger, authorityPassphrase)
		if err := presentation.Emit(tui.Event{Kind: tui.BootstrapInspectionComplete, Origin: tui.AegisAuthoritative, Message: fmt.Sprintf("artifact-derived bootstrap state: %s (%s)", snapshot.State, snapshot.Reason)}); err != nil {
			return false, err
		}
		renderBootstrapInspection(cmd, snapshot)
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

func renderBootstrapInspection(cmd *cobra.Command, snapshot onboarding.Snapshot) {
	fmt.Fprintf(cmd.OutOrStdout(), "\nInstallation inspection\n  configuration  %s\n  state          %s\n  derived state  %s\n  reason         %s\n", snapshot.ConfigPath, valueOr(snapshot.StatePath, "not created"), snapshot.State, snapshot.Reason)
	for _, check := range snapshot.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "  [%-15s] %s", check.Status, check.Name)
		if check.Reason != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", check.Reason)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		if check.Remedy != "" && check.Status != "verified" {
			fmt.Fprintln(cmd.OutOrStdout(), "    next:", check.Remedy)
		}
	}
}

func bootstrapAuthority(cmd *cobra.Command, build builder, input *terminalInput, snapshot onboarding.Snapshot, unlocked *[]byte) (bool, error) {
	inspection := config.Inspect(snapshot.ConfigPath)
	if inspection.State == config.StateValid && inspection.Config.Credentials.Authority.Custody == "passphrase-file" {
		passphrase, err := readAuthorityPassphrase(cmd, false)
		if err != nil {
			return false, err
		}
		custodian, err := credentials.LoadPassphraseCustodian(inspection.Config.Credentials.Authority.KEKFile, passphrase)
		if err != nil {
			wipeSecret(passphrase)
			return false, err
		}
		custodian.Close()
		wipeSecret(*unlocked)
		*unlocked = passphrase
		fmt.Fprintln(cmd.OutOrStdout(), "Encrypted credential authority unlocked and verified for this process.")
		return true, nil
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
	fmt.Fprintln(cmd.OutOrStdout(), "\nCredential authority custody")
	fmt.Fprintln(cmd.OutOrStdout(), "  [1] passphrase-encrypted local KEK (default; works in this terminal)")
	fmt.Fprintln(cmd.OutOrStdout(), "  [2] systemd service credential (advanced; must already be delivered by a service unit)")
	fmt.Fprintln(cmd.OutOrStdout(), "  [3] plaintext host file (development only; weaker)")
	fmt.Fprintln(cmd.OutOrStdout(), "  [4] exit without mutation")
	fmt.Fprint(cmd.OutOrStdout(), "Select [1]: ")
	answer, eof, err := readBootstrapLine(cmd, input, 32)
	if err != nil || eof || answer == "4" || answer == "exit" {
		return false, err
	}
	custody := ""
	switch answer {
	case "", "1", "passphrase-file":
		return bootstrapPassphraseAuthority(cmd, build, snapshot, unlocked)
	case "2", "systemd":
		custody = "systemd"
	case "3", "host-file":
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
	file, ok := cmd.InOrStdin().(*os.File)
	if !ok || !terminalPair(file, cmd.OutOrStdout()) {
		return nil, errors.New("a real terminal is required for no-echo authority passphrase intake")
	}
	first, err := readTerminalSecretBounded(cmd.Context(), file, cmd.ErrOrStderr(), "Authority passphrase (minimum 12 bytes): ", 1024)
	if err != nil {
		return nil, err
	}
	if len(first) < 12 {
		wipeSecret(first)
		return nil, errors.New("authority passphrase must contain at least 12 bytes")
	}
	if !confirm {
		return first, nil
	}
	second, err := readTerminalSecretBounded(cmd.Context(), file, cmd.ErrOrStderr(), "Confirm authority passphrase: ", 1024)
	if err != nil {
		wipeSecret(first)
		return nil, err
	}
	defer wipeSecret(second)
	if !bytes.Equal(first, second) {
		wipeSecret(first)
		return nil, errors.New("authority passphrase confirmation does not match")
	}
	return first, nil
}

func readDefaultYes(cmd *cobra.Command, input *terminalInput) (bool, error) {
	answer, eof, err := readBootstrapLine(cmd, input, 16)
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
		if _, _, err = authenticatedService(cmd, build); err != nil {
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
	if _, _, err = authenticatedService(cmd, build); err != nil {
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
	})
	if err != nil {
		return false, fmt.Errorf("%w; certification was not saved; retry with: aegis manager certify %s", err, candidate)
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

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
