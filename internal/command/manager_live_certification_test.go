package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	managerdomain "github.com/berryhill/aegis/internal/manager"
)

// TestLiveManagerCertification is an opt-in, non-authorizing route probe. It
// runs the real corpus but never reads production Aegis state or saves a
// certification. A successful run is not principal authentication.
func TestLiveManagerCertification(t *testing.T) {
	if os.Getenv("AEGIS_LIVE_CERTIFY") != "1" {
		t.Skip("explicit live local-model opt-in required")
	}
	installation := os.Getenv("AEGIS_LIVE_HERMES_INSTALLATION")
	endpoint := os.Getenv("AEGIS_LIVE_OLLAMA")
	candidateID := os.Getenv("AEGIS_LIVE_CANDIDATE")
	digest := os.Getenv("AEGIS_LIVE_DIGEST")
	if !filepath.IsAbs(installation) || installation == "" || filepath.Clean(installation) != installation ||
		endpoint != "http://127.0.0.1:11434" || !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		t.Fatal("exact absolute Hermes installation, loopback Ollama, and pinned digest are required")
	}
	if _, err := hex.DecodeString(digest[7:]); err != nil {
		t.Fatal("pinned digest is not hexadecimal")
	}
	if parsed, err := url.Parse(endpoint); err != nil || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("Ollama endpoint is not exact loopback")
	}
	candidate, err := managerdomain.CandidateByID(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	python := managerPython(installation, filepath.Join(installation, "venv", "bin", "hermes"))
	if python == "" {
		t.Fatal("installed Hermes Python is unavailable")
	}
	cfg := config.Defaults().Manager
	checkpointPath := filepath.Join(t.TempDir(), "campaign", "checkpoint.json")
	var prior []managerdomain.ConformanceResult
	var checkpoint managerdomain.CertificationCheckpoint
	for segment := 0; segment < 4; segment++ {
		cert := runLiveCertificationSegment(t, installation, endpoint, python, candidate, digest, cfg, prior)
		if segment < 3 {
			if segment == 0 {
				checkpoint = managerdomain.CertificationCheckpoint{SchemaVersion: "aegis.manager.certification-checkpoint.v1", PrincipalID: "test-only", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
			}
			checkpoint.Certification = cert
			checkpoint.Certification.CertifiedAt = time.Time{}
			previous := len(prior)
			if previous == 0 {
				previous = -1
			}
			if err := managerdomain.SaveCertificationCheckpoint(checkpointPath, checkpoint, previous); err != nil {
				t.Fatalf("segment %d checkpoint save: %v", segment, err)
			}
			loaded, present, err := managerdomain.LoadCertificationCheckpoint(checkpointPath, checkpoint.Certification, "test-only", time.Now().UTC())
			if err != nil || !present {
				t.Fatalf("segment %d checkpoint readback present=%t err=%v", segment, present, err)
			}
			prior = loaded.Certification.Results
			t.Logf("segment=%d checkpointed_cases=%d certified=false", segment+1, len(prior))
		} else {
			if err := cert.Validate(); err != nil {
				t.Fatal("final complete corpus invalid:", err)
			}
			t.Logf("live segmented corpus passed: cases=%d; certification_not_saved=true", len(cert.Results))
		}
	}
}

func runLiveCertificationSegment(t *testing.T, installation, endpoint, python string, candidate managerdomain.Candidate, digest string, cfg config.Manager, prior []managerdomain.ConformanceResult) managerdomain.Certification {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	ollama, err := managerdomain.NewOllamaClient(endpoint, cfg.Inference.RequestTimeout)
	if err != nil {
		t.Fatal(err)
	}
	version, err := ollama.Version(ctx)
	if err != nil {
		t.Fatal("Ollama version unavailable:", err)
	}
	model, err := ollama.VerifyModel(ctx, candidate.OllamaName, digest)
	if err != nil {
		t.Fatal("exact model verification failed:", err)
	}
	cleanup := &certificationCleanup{}
	defer func() {
		if err := cleanup.close(); err != nil {
			t.Errorf("live probe cleanup failed: %v", err)
		}
	}()
	if err := loadCertificationModel(ctx, cleanup, ollama, candidate.OllamaName, digest, cfg.Hermes.ContextLength, cfg.Inference.KeepAlive, cfg.CleanupTimeout); err != nil {
		t.Fatal("model load failed:", err)
	}
	guard, err := managerdomain.NewGuard(int(cfg.Ingress.MaximumMessageBytes), cfg.Ingress.MaximumMessageRunes, cfg.Ingress.BoundedDecodeDepth, cfg.Ingress.ScanTimeout)
	if err != nil {
		t.Fatal(err)
	}
	attempt := sha256.Sum256([]byte(candidate.OllamaName + "\x00" + digest + "\x00" + managerdomain.CorpusDigest()))
	var active atomic.Bool
	active.Store(true)
	defer active.Store(false)
	var budget atomic.Int32
	format := &conformanceFormat{}
	authorizer := managerdomain.NewProcessAuthorizer()
	proxy, err := managerdomain.StartProxy(ctx, managerdomain.ProxyConfig{
		Target: endpoint, Model: candidate.OllamaName, RouteDigest: "sha256:" + hex.EncodeToString(attempt[:]),
		MaximumRequestBytes: cfg.Inference.MaximumRequestBytes, MaximumResponseBytes: cfg.Inference.MaximumResponseBytes,
		Timeout: cfg.Inference.RequestTimeout, Guard: guard, SessionActive: active.Load,
		ProcessAuthorizer: authorizer, CapabilityExpires: deadline, ConsumeCapability: func() bool { return consumeCertificationBudget(&budget) },
		RequireSystemInstruction: true, AllowPlaintextRequests: true, ResponseFormat: format.get,
	})
	if err != nil {
		t.Fatal("proxy startup failed:", err)
	}
	cleanup.add(func() error { return closeProxyBounded(proxy, cfg.CleanupTimeout) })
	hermes, err := managerdomain.StartHermesProcess(ctx, managerdomain.HermesProcessConfig{
		Python: python, Installation: installation, StateRoot: t.TempDir(), ProxyEndpoint: proxy.Endpoint(),
		Model: candidate.OllamaName, MaximumMessageBytes: int(cfg.Inference.MaximumResponseBytes),
		StartTimeout: cfg.Hermes.GatewayStartTimeout, AuthorizeRelease: authorizer.Bind,
	})
	if err != nil {
		t.Fatal("Hermes startup failed:", err)
	}
	cleanup.add(func() error { return closeHermesBounded(hermes, cfg.CleanupTimeout) })
	var caseStart time.Time
	executor := liveConformanceExecutor{gateway: hermes.Client(), budget: &budget, maximum: int(cfg.Hermes.MaximumResponseBytes), timeout: cfg.Hermes.TurnTimeout, proxy: proxy, format: format,
		progress: func(caseID string) {
			caseStart = time.Now()
			t.Logf("case=%s start_elapsed=%s", caseID, time.Until(deadline).Round(time.Second))
		},
	}
	probe := timedConformanceExecutor{run: executor, t: t, caseStart: &caseStart}
	// This is the same 15-minute aggregate budget as an authenticated run,
	// but test-owned authority is not a production principal or a saved grant.
	cert, complete, runErr := managerdomain.RunCertificationSegment(ctx, probe, candidate, candidate.OllamaName, digest,
		model.Details.QuantizationLevel, "installed-hermes-probe", version, cfg.Hermes.ContextLength, time.Now().UTC(), prior, 4)
	if runErr != nil {
		var failure *managerdomain.ConformanceFailure
		if errors.As(runErr, &failure) {
			t.Fatalf("live corpus failed: case=%s reason=%s elapsed=%s", failure.CaseID, failure.Reason, time.Since(deadline.Add(-15*time.Minute)).Round(time.Second))
		}
		t.Fatalf("live corpus failed: %v", runErr)
	}
	if err := cleanup.close(); err != nil {
		t.Fatal("cleanup failed before pass:", err)
	}
	if complete != (len(cert.Results) == len(managerdomain.ConformanceCorpus())) {
		t.Fatal(fmt.Sprintf("completion mismatch: %t cases=%d", complete, len(cert.Results)))
	}
	if ctx.Err() != nil {
		t.Fatal("segment authority expired before checkpoint")
	}
	t.Logf("segment passed: cases=%d elapsed=%s", len(cert.Results), time.Since(deadline.Add(-15*time.Minute)).Round(time.Second))
	return cert
}

type timedConformanceExecutor struct {
	run       liveConformanceExecutor
	t         *testing.T
	caseStart *time.Time
}

func (e timedConformanceExecutor) Execute(ctx context.Context, test managerdomain.ConformanceCase) ([]byte, error) {
	result, err := e.run.Execute(ctx, test)
	reason := "none"
	if err != nil {
		var failure *managerdomain.ConformanceFailure
		if errors.As(err, &failure) {
			reason = failure.Reason
		} else {
			reason = "unclassified"
		}
	}
	e.t.Logf("case=%s attempt_duration=%s reason=%s", test.ID, time.Since(*e.caseStart).Round(time.Second), reason)
	return result, err
}
