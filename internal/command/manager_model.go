package command

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/config"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/spf13/cobra"
)

func managerModelCmd(build builder, options *rootOptions) *cobra.Command {
	model := &cobra.Command{Use: "model", Short: "Inspect and configure an exact already-installed local manager model"}
	candidates := &cobra.Command{Use: "candidates", Short: "List official traceable manager candidates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, _, err := authenticatedService(cmd, build); err != nil {
			return err
		}
		return output(cmd, map[string]any{"candidates": managerdomain.Candidates(), "default": nil, "downloaded": false, "next_step": "aegis manager model candidate CANDIDATE_ID"})
	}}
	candidate := &cobra.Command{Use: "candidate CANDIDATE_ID", Short: "Inspect candidate source and license provenance", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if _, _, err := authenticatedService(cmd, build); err != nil {
			return err
		}
		item, err := managerdomain.CandidateByID(args[0])
		if err != nil {
			return usage(err)
		}
		return output(cmd, map[string]any{"candidate": item, "certified": false, "note": "registry membership is provenance metadata, not certification", "next_step": "aegis manager model discover --endpoint http://127.0.0.1:11434"})
	}}
	var discoverEndpoint string
	discover := &cobra.Command{Use: "discover", Short: "Discover approved artifacts visible at one exact external loopback Ollama route", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, _, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		if discoverEndpoint == "" {
			return usage(errors.New("--endpoint is required; ambient Ollama state is never selected"))
		}
		report, err := managerdomain.DiscoverInstalledCandidates(cmd.Context(), discoverEndpoint, service.Config.Manager.Inference.RequestTimeout)
		if err != nil {
			return err
		}
		return output(cmd, report)
	}}
	discover.Flags().StringVar(&discoverEndpoint, "endpoint", "", "exact external HTTP loopback Ollama origin")

	var routeMode, routeEndpoint string
	route := &cobra.Command{Use: "route", Short: "Preview managed versus external-local model visibility and ownership", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, _, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		switch routeMode {
		case "managed":
			return output(cmd, map[string]any{"mode": "managed", "daemon_ownership": "Aegis starts and stops the daemon", "model_store": filepath.Join(service.Config.StateDir, "manager", "ollama-models"), "ambient_models_visible": false, "ordinary_startup_imports_or_downloads": false, "next_step": "Use external-local for an artifact already installed in an operator daemon; managed-store installation/import is a separate future explicitly authorized operation."})
		case "external-local":
			if _, err = managerdomain.NewOllamaClient(routeEndpoint, service.Config.Manager.Inference.RequestTimeout); err != nil {
				return usage(err)
			}
			return output(cmd, map[string]any{"mode": "external-local", "endpoint": routeEndpoint, "daemon_ownership": "operator-managed; Aegis does not stop or update it", "model_store": "artifact remains in the external daemon's store", "weaker_boundary": true, "imports_or_downloads": false, "next_step": "aegis manager model discover --endpoint " + routeEndpoint})
		default:
			return usage(errors.New("--mode must be managed or external-local"))
		}
	}}
	route.Flags().StringVar(&routeMode, "mode", "", "managed or external-local")
	route.Flags().StringVar(&routeEndpoint, "endpoint", "", "exact loopback origin required for external-local")
	_ = route.MarkFlagRequired("mode")

	var configureEndpoint, certificationPath string
	configure := &cobra.Command{Use: "configure CANDIDATE_ID", Short: "Preview and atomically configure one installed external-local artifact", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		if configureEndpoint == "" {
			return usage(errors.New("--endpoint is required; ambient Ollama state is never selected"))
		}
		report, err := managerdomain.DiscoverInstalledCandidates(cmd.Context(), configureEndpoint, service.Config.Manager.Inference.RequestTimeout)
		if err != nil {
			return err
		}
		var installed *managerdomain.InstalledCandidate
		for index := range report.Installed {
			if report.Installed[index].Candidate.ID == args[0] {
				copy := report.Installed[index]
				installed = &copy
			}
		}
		if installed == nil {
			return usage(errors.New("selected approved candidate is not installed at the exact configured route; no write performed"))
		}
		configPath, err := configPathForMutation(options.configFile)
		if err != nil {
			return usage(err)
		}
		preview, err := managerdomain.PreviewExternalModelConfiguration(configPath, service.Config.StateDir, certificationPath, *installed)
		if err != nil {
			return err
		}
		if err = output(cmd, map[string]any{"preview": preview, "authenticated_principal": subject.PrincipalID, "effects": []string{"atomically update manager.inference in the existing config", "create no model, certification, authority, daemon, or profile"}, "confirmation_required": "yes"}); err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), "Type yes to apply this exact model configuration, or anything else to decline: ")
		answer, eof, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 16)
		if err != nil {
			return err
		}
		if eof || answer != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), "Model configuration declined; no writes were performed.")
			return nil
		}
		if err = managerdomain.ApplyModelConfiguration(preview); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "configured", "model": preview.Model, "digest": preview.Digest, "mode": preview.Mode, "endpoint": preview.Endpoint, "certification": preview.Certification, "activated": false, "downloaded": false, "copied": false, "next_step": "aegis manager certify " + preview.CandidateID})
	}}
	configure.Flags().StringVar(&configureEndpoint, "endpoint", "", "exact external HTTP loopback Ollama origin")
	configure.Flags().StringVar(&certificationPath, "certification", "", "certification destination below Aegis state (defaults to state/manager/certifications/CANDIDATE_ID.json)")

	status := &cobra.Command{Use: "status", Short: "Inspect configured model, certification path, and drift without loading a model", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, _, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		cfg := service.Config.Manager.Inference
		result := map[string]any{"mode": cfg.Mode, "model": cfg.Model, "digest": cfg.ModelDigest, "certification": cfg.Certification, "loaded": false, "download_attempted": false, "fallback": false}
		if cfg.Model == "" {
			result["reason"] = managerdomain.ReasonModelAbsent
			result["next_step"] = "aegis manager model candidates"
			return output(cmd, result)
		}
		if cfg.Mode != "external-local" {
			result["artifact_status"] = "managed private store is inspected only when its Aegis-owned daemon is explicitly started"
			result["next_step"] = "aegis manager model route --mode managed"
			return output(cmd, result)
		}
		client, err := managerdomain.NewOllamaClient(cfg.Endpoint, service.Config.Manager.Inference.RequestTimeout)
		if err != nil {
			return err
		}
		_, err = client.VerifyModel(cmd.Context(), cfg.Model, cfg.ModelDigest)
		if err != nil {
			result["artifact_status"] = "drifted_or_absent"
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (!strings.Contains(err.Error(), managerdomain.ReasonModelAbsent) && !strings.Contains(err.Error(), managerdomain.ReasonDigestMismatch)) {
				result["reason"] = managerdomain.ReasonOllamaUnavailable
			} else {
				result["reason"] = managerStartupReason(err)
			}
		} else {
			result["artifact_status"] = "installed_exact_digest"
		}
		certification, certificationErr := managerdomain.InspectCertification(cfg.Certification, cfg.Model, cfg.ModelDigest)
		if certificationErr != nil {
			result["certification_status"] = "absent_or_insecure"
			result["reason"] = managerdomain.ReasonNotCertified
			result["next_step"] = "aegis manager certify " + candidateIDForModel(cfg.Model)
		} else {
			result["certification_status"] = "valid for configured model and digest; full runtime tuple is validated at manager startup"
			result["candidate_id"] = certification.CandidateID
			result["certified_at"] = certification.CertifiedAt
			result["next_step"] = "aegis manager"
		}
		return output(cmd, result)
	}}
	model.AddCommand(candidates, candidate, discover, route, configure, status)
	return model
}

func configPathForMutation(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	return configDefaultPath()
}

func configDefaultPath() (string, error) {
	return config.DefaultPath()
}

func candidateIDForModel(model string) string {
	for _, candidate := range managerdomain.Candidates() {
		if candidate.OllamaName == model {
			return candidate.ID
		}
	}
	return "CANDIDATE_ID"
}
