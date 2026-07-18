package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"sync/atomic"
	"time"

	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/spf13/cobra"
)

type liveConformanceExecutor struct {
	gateway *managerdomain.GatewayClient
	session string
	budget  *atomic.Int32
	maximum int
}

func (e liveConformanceExecutor) Execute(ctx context.Context, test managerdomain.ConformanceCase) ([]byte, error) {
	e.budget.Store(1)
	defer e.budget.Store(0)
	prompt := fmt.Sprintf("Conformance case %s. Requirement: %s\nInput: %s", test.ID, test.Requirement, test.Input)
	return e.gateway.Turn(ctx, e.session, prompt, e.maximum)
}

func managerCertifyCmd(build builder) *cobra.Command {
	return &cobra.Command{Use: "certify CANDIDATE_ID", Short: "Explicitly run live conformance for one already-installed exact local artifact", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, subject, err := authenticatedService(cmd, build)
		if err != nil {
			return err
		}
		cfg := service.Config.Manager
		if cfg.Inference.Model == "" || cfg.Inference.ModelDigest == "" || cfg.Inference.Certification == "" {
			return usage(errors.New("configure exact manager model, digest, and certification path before live certification"))
		}
		var candidate *managerdomain.Candidate
		for _, item := range managerdomain.Candidates() {
			if item.ID == args[0] {
				copy := item
				candidate = &copy
			}
		}
		if candidate == nil || candidate.OllamaName != cfg.Inference.Model {
			return usage(errors.New("configured model does not exactly match the selected official candidate"))
		}
		descriptor, err := service.Hermes.Discover(cmd.Context())
		if err != nil {
			return err
		}
		guard, err := managerdomain.NewGuard(int(cfg.Ingress.MaximumMessageBytes), cfg.Ingress.MaximumMessageRunes, cfg.Ingress.BoundedDecodeDepth, cfg.Ingress.ScanTimeout)
		if err != nil {
			return err
		}
		endpoint := cfg.Inference.Endpoint
		var managed *managerdomain.ManagedOllama
		if cfg.Inference.Mode == "managed" {
			managed, err = managerdomain.StartManagedOllama(cmd.Context(), cfg.Inference.Executable, service.Config.StateDir, cfg.Inference.StartTimeout)
			if err != nil {
				return err
			}
			endpoint = managed.Endpoint()
			defer managed.Close(context.Background())
		}
		ollama, err := managerdomain.NewOllamaClient(endpoint, cfg.Inference.RequestTimeout)
		if err != nil {
			return err
		}
		version, err := ollama.Version(cmd.Context())
		if err != nil {
			return err
		}
		model, err := ollama.VerifyModel(cmd.Context(), cfg.Inference.Model, cfg.Inference.ModelDigest)
		if err != nil {
			return err
		}
		if err = ollama.Load(cmd.Context(), cfg.Inference.Model, cfg.Hermes.ContextLength, cfg.Inference.KeepAlive); err != nil {
			return err
		}
		defer ollama.Unload(context.Background(), cfg.Inference.Model)
		var active atomic.Bool
		active.Store(true)
		defer active.Store(false)
		var budget atomic.Int32
		attemptSum := sha256.Sum256([]byte(cfg.Inference.Model + "\x00" + cfg.Inference.ModelDigest + "\x00" + descriptor.Version + "\x00" + version + "\x00" + managerdomain.CorpusDigest()))
		attemptDigest := "sha256:" + hex.EncodeToString(attemptSum[:])
		proxy, err := managerdomain.StartProxy(cmd.Context(), managerdomain.ProxyConfig{Target: endpoint, Model: cfg.Inference.Model, RouteDigest: attemptDigest, MaximumRequestBytes: cfg.Inference.MaximumRequestBytes, MaximumResponseBytes: cfg.Inference.MaximumResponseBytes, Timeout: cfg.Inference.RequestTimeout, Guard: guard, SessionActive: active.Load, CapabilityExpires: subject.ExpiresAt, ConsumeCapability: func() bool { return budget.CompareAndSwap(1, 0) }})
		if err != nil {
			return err
		}
		defer proxy.Close(context.Background())
		python := managerPython(descriptor.Installation, descriptor.Executable)
		if python == "" {
			return errors.New("Hermes gateway Python executable not found")
		}
		hermes, err := managerdomain.StartHermesProcess(cmd.Context(), managerdomain.HermesProcessConfig{Python: python, Installation: descriptor.Installation, StateRoot: service.Config.StateDir, ProxyEndpoint: proxy.Endpoint(), ProxyToken: proxy.Token(), Model: cfg.Inference.Model, MaximumMessageBytes: int(cfg.Inference.MaximumResponseBytes), StartTimeout: cfg.Hermes.GatewayStartTimeout})
		if err != nil {
			return err
		}
		defer hermes.Close(context.Background())
		session, err := hermes.Client().CreateSession(cmd.Context(), "aegis-manager-certification")
		if err != nil {
			return err
		}
		certification, err := managerdomain.RunCertification(cmd.Context(), liveConformanceExecutor{gateway: hermes.Client(), session: session, budget: &budget, maximum: int(cfg.Hermes.MaximumResponseBytes)}, *candidate, cfg.Inference.Model, cfg.Inference.ModelDigest, model.Details.QuantizationLevel, descriptor.Version, version, cfg.Hermes.ContextLength, time.Now().UTC())
		if err != nil {
			return err
		}
		if err = managerdomain.SaveCertification(cfg.Inference.Certification, certification); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "certified", "candidate_id": certification.CandidateID, "artifact": certification.ArtifactName, "artifact_digest": certification.ArtifactDigest, "hermes_version": certification.HermesVersion, "ollama_version": certification.OllamaVersion, "context_length": certification.ContextLength, "corpus_digest": certification.CorpusDigest, "certified_at": certification.CertifiedAt, "certification": cfg.Inference.Certification})
	}}
}
