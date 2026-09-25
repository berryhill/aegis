package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"path/filepath"
	"time"
)

// Exact contract digests in operator configuration authorize workspace writes AND native code execution.
// Agent-authored definitions alone never grant host authority.
type ImplementationController struct {
	config   config.Implementation
	root     string
	adapter  *hermesruntime.Adapter
	decision *LayaDecisionAdapter
}

func (w *QueueWorker) ConfigureImplementation(c config.Implementation, stateRoot string, adapter *hermesruntime.Adapter) error {
	if len(c.AuthorizedContracts) == 0 {
		return nil
	}
	if !filepath.IsAbs(c.GoBinary) || !filepath.IsAbs(stateRoot) || adapter == nil {
		return errors.New("explicit native checker and controller custody required")
	}
	if (c.LayaPython == "") != (c.LayaHome == "") {
		return errors.New("local Laya executable and private home must be configured together")
	}
	var decision *LayaDecisionAdapter
	if c.LayaPython != "" {
		if !filepath.IsAbs(c.LayaPython) || !filepath.IsAbs(c.LayaHome) {
			return errors.New("local Laya paths must be absolute")
		}
		decision = NewLayaDecisionAdapter(LocalLayaProcess{PythonExecutable: c.LayaPython, Home: c.LayaHome})
	}
	c.AuthorizedContracts = append([]string(nil), c.AuthorizedContracts...)
	w.implementation = &ImplementationController{config: c, root: filepath.Join(stateRoot, "persistence", "fleet-v1"), adapter: adapter, decision: decision}
	return nil
}
func (c *ImplementationController) authorize(steps []loop.Step, agent registry.AgentRevision) error {
	if c == nil || agent.Runtime.Adapter != "hermes" || agent.Runtime.Runtime != "hermes-agent" {
		return errors.New("operator implementation authorization required")
	}
	for _, s := range steps {
		if s.Implementation != nil {
			d, err := s.Implementation.Digest()
			if err != nil {
				return err
			}
			for _, allowed := range c.config.AuthorizedContracts {
				if d == allowed {
					return nil
				}
			}
		}
	}
	return errors.New("implementation contract not authorized by operator")
}
func (w *QueueWorker) processImplementation(ctx context.Context, request WorkRequest, base WorkResult, runtime RuntimeRequest, action string) (WorkResult, error) {
	c := w.implementation
	var contract loop.VerifiedImplementation
	for _, s := range runtime.LoopRevision.Steps {
		if s.Implementation != nil {
			contract = *s.Implementation
		}
	}
	if len(runtime.Launch.AuthorityContext.Authority.Tools) != 0 || len(runtime.Launch.AuthorityContext.Authority.Credentials) != 0 {
		return w.terminal(ctx, request, base, execution.StateDenied, "implementation_tools_denied", nil, nil)
	}
	custody, ok := w.repository.(interface{ ImplementationStore() implementation.Store })
	if !ok {
		return w.terminal(ctx, request, base, execution.StateFailed, "implementation_custody_unavailable", nil, nil)
	}
	turn := hermesruntime.AttemptTurnRequest{Launch: runtime.Launch, Admission: runtime.Admission, ParentAttemptID: base.Attempt.AttemptID, StateRoot: filepath.Join(filepath.Dir(filepath.Dir(c.root)), "runtime", "fleet"), Model: runtime.Launch.AuthorityContext.Authority.Hermes.Model, Provider: runtime.Launch.AuthorityContext.Authority.Hermes.Provider, Bounds: hermesruntime.AttemptBounds{InputBytes: hermesruntime.MaxAttemptInputBytes, OutputBytes: hermesruntime.MaxAttemptOutputBytes, Duration: hermesruntime.MaxAttemptDuration}}
	kernel := &implementation.Executor{DB: custody.ImplementationStore(), GoBinary: c.config.GoBinary, Proposer: HermesPatchProposer{Adapter: c.adapter, Request: turn}}
	if contract.DecisionMode == "doer.v1" {
		if c.decision == nil {
			return w.terminal(ctx, request, base, execution.StateDenied, "implementation_laya_unconfigured", nil, nil)
		}
		kernel.Decision = implementationLayaDecision{adapter: c.decision}
		luna := implementationLuna{adapter: c.adapter, request: turn}
		kernel.Diagnosis = luna
		kernel.Reporter = luna
	}
	kernel.Admit = func(ctx context.Context, _ string) error {
		p, e := w.repository.GetQueueProjection(ctx, request.QueueItemID)
		if e != nil {
			return e
		}
		if p.State != queue.StateClaimed {
			return &implementation.Halt{State: string(p.State)}
		}
		if e = c.authorize(runtime.LoopRevision.Steps, runtime.Participant); e != nil {
			return e
		}
		decision, e := runtime.Admission.CheckRuntimeAdmission(ctx, runtime.Launch, time.Now().UTC())
		if e != nil {
			return e
		}
		if execution.ValidateAdmission(runtime.Launch, decision, time.Now().UTC()) != nil {
			return &implementation.Halt{State: "denied"}
		}
		return nil
	}
	record, err := kernel.Run(ctx, base.Attempt.AttemptID, contract)
	if err == nil && record.State == "needs_input" {
		return w.terminal(ctx, request, base, execution.StateFailed, "implementation_needs_input", nil, nil)
	}
	if err != nil {
		state := execution.State(record.State)
		if executionQueueState(state) == "" {
			state = execution.StateFailed
		}
		terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return w.terminal(terminalCtx, request, base, state, "implementation_"+record.State, nil, nil)
	}
	output, err := kernel.Output(base.Attempt.AttemptID, contract)
	if err != nil {
		return w.terminal(ctx, request, base, execution.StateFailed, "implementation_revalidation_failed", nil, nil)
	}
	ref, err := w.blobs.PutBlob(output)
	if err != nil {
		return base, err
	}
	artifact := evidence.RuntimeArtifact{ID: request.ArtifactID, AttemptID: base.Attempt.AttemptID, OwnerID: runtime.Participant.AgentID, ActionID: action, RunID: runtime.LoopExecutionID, AuthorityContextID: request.Authority.ID, AuthorityContextDigest: request.Authority.Digest, Digest: ref, ContentRef: ref, MediaType: "application/json", CreatedAt: w.now()}
	verifier, ok := w.verifier.(*evidence.BlobVerifier)
	if !ok {
		return base, errors.New("native evidence verifier required")
	}
	receipt, proof, err := verifyImplementation(ctx, verifier, artifact, kernel, contract)
	if err != nil {
		return w.terminal(ctx, request, base, execution.StateFailed, "implementation_evidence_failed", nil, nil)
	}
	return w.terminal(ctx, request, base, execution.StateSucceeded, "implementation_verified", &artifact, []evidence.VerificationReceipt{receipt}, proof)
}

// verifyImplementation composes the native kernel with the evidence-owned port.
// Every invocation (including final persistence admission) reloads custody.
func verifyImplementation(ctx context.Context, verifier *evidence.BlobVerifier, artifact evidence.RuntimeArtifact, kernel *implementation.Executor, contract loop.VerifiedImplementation) (evidence.VerificationReceipt, evidence.CompletionProvenance, error) {
	digest, err := contract.Digest()
	if err != nil {
		return evidence.VerificationReceipt{}, evidence.CompletionProvenance{}, err
	}
	return verifier.VerifyImplementation(ctx, artifact, evidence.ImplementationCheck{ContractDigest: digest, PolicyVersion: loop.VerifiedImplementationSchema, Reload: func() ([]byte, error) {
		record, err := kernel.Read(artifact.AttemptID)
		if err != nil {
			return nil, err
		}
		if record.State != "succeeded" {
			return nil, errors.New("implementation has no terminal checker success")
		}
		return kernel.Output(artifact.AttemptID, contract)
	}})
}
