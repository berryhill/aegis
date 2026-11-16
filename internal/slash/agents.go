package slash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"golang.org/x/sys/unix"
)

const maximumAgentInputBytes = 1 << 20

func (s *Service) agents(ctx context.Context, result Result, manager Context, request Request) (Result, error) {
	switch request.Arguments[0] {
	case "providers":
		input, err := app.NewAgentProviderSetupInput(request.Arguments[2:])
		if err != nil {
			return agentFailure(result, err)
		}
		value, err := s.app.AgentProviderSetupAs(ctx, manager.Subject, request.Arguments[1], input)
		if err != nil {
			// Never promote the manager subject to local-os human authority.
			result.State, result.Reason = "denied", "provider_setup_denied"
			result.Warnings = []string{"Provider setup requires fresh human local-os/linux-so-peercred authority and exact review; use the owning-service agents providers CLI if this manager cannot carry it. No authority was synthesized."}
			if code := app.ProviderAuthenticationFailureCode(err); code != "" {
				result.Reason = code
				result.Warnings = []string{"Native source/transport admission failed. Registration and connection are not executable qualification; preserve the exact source and existing gate."}
			} else if errors.Is(err, app.ErrConflict) {
				result.Reason = "provider_setup_review_conflict"
				result.Warnings = []string{"Exact review or source state changed. Read providers status and Agent history before preparing another review; do not create a replacement identity."}
			}
			return result, nil
		}
		return completed(result, "agent_provider_setup", map[string]any{"setup": value, "message": value.Message}), nil
	case "readiness", "list":
		agents, err := s.app.ListFleetAgentsAs(ctx, manager.Subject)
		if err != nil {
			return agentFailure(result, err)
		}
		if request.Arguments[0] == "readiness" {
			return completed(result, "agent_registry_ready", map[string]any{
				"ready": true, "agent_count": len(agents), "empty_registry_valid": true,
				"operations": []string{"readiness", "list", "show", "import hermes default", "import hermes default with-models", "providers", "prepare", "register"},
			}), nil
		}
		return completed(result, "agent_registry_list", map[string]any{"agents": agents, "count": len(agents), "empty_registry_valid": true}), nil
	case "show":
		revision := uint64(0)
		if len(request.Arguments) == 3 {
			parsed, err := strconv.ParseUint(request.Arguments[2], 10, 64)
			if err != nil || parsed == 0 {
				return failed(result, "invalid_agent_revision", "revision must be a positive base-10 integer"), nil
			}
			revision = parsed
		}
		agent, err := s.app.GetFleetAgentAs(ctx, manager.Subject, request.Arguments[1], revision)
		if err != nil {
			return agentFailure(result, err)
		}
		return completed(result, "agent_registry_exact_readback", map[string]any{"agent": agent}), nil
	case "import":
		if len(request.Arguments) >= 4 && request.Arguments[3] == "with-models" {
			if len(request.Arguments) == 4 {
				proposal, err := s.app.PrepareLocalHermesModelImportAs(ctx, manager.Subject)
				if err != nil {
					return agentFailure(result, err)
				}
				return completed(result, "local_hermes_agent_import_prepared", map[string]any{"proposal": proposal, "message": app.LocalHermesImportGuidance(proposal), "authorizing": false, "registered": false, "activation": false}), nil
			}
			agent, created, err := s.app.ConfirmLocalHermesModelImportAs(ctx, manager.Subject, request.Arguments[5])
			if err != nil {
				return agentFailure(result, err)
			}
			next, message := app.LocalHermesImportContinuation(agent, true)
			return completed(result, "local_hermes_agent_import_confirmed", map[string]any{"agent": agent, "created": created, "message": message, "exact_readback_verified": true, "activation": false, "authentication": "not_connected", "qualification": "not_checked", "next": next}), nil
		}
		if len(request.Arguments) == 3 {
			proposal, err := s.app.PrepareLocalHermesAgentImportAs(ctx, manager.Subject)
			if err != nil {
				return agentFailure(result, err)
			}
			return completed(result, "local_hermes_agent_import_prepared", map[string]any{"proposal": proposal, "message": app.LocalHermesImportGuidance(proposal), "authorizing": false, "registered": false, "activation": false}), nil
		}
		agent, created, err := s.app.ConfirmLocalHermesAgentImportAs(ctx, manager.Subject, request.Arguments[4])
		if err != nil {
			return agentFailure(result, err)
		}
		next, message := app.LocalHermesImportContinuation(agent, false)
		return completed(result, "local_hermes_agent_import_confirmed", map[string]any{"agent": agent, "created": created, "message": message, "exact_readback_verified": true, "activation": false, "next": next}), nil
	case "prepare", "register":
		charter, err := readBoundedAgentInput(request.Arguments[1])
		if err != nil {
			return failed(result, "invalid_charter_input", err.Error()), nil
		}
		fixture, err := readBoundedAgentInput(request.Arguments[2])
		if err != nil {
			return failed(result, "invalid_fleet_fixture_input", err.Error()), nil
		}
		proposal, err := s.app.PrepareFleetAgentRegistrationAs(ctx, manager.Subject, charter, fixture, request.Arguments[3], request.Arguments[4])
		if err != nil {
			return agentFailure(result, err)
		}
		if request.Arguments[0] == "prepare" {
			return completed(result, "agent_registration_prepared", map[string]any{
				"proposal": proposal, "authorizing": false,
				"confirmation": "repeat with /agents register and the exact revision_digest",
			}), nil
		}
		expected := request.Arguments[5]
		if !strings.HasPrefix(expected, "sha256:") || expected != proposal.RevisionDigest {
			return failed(result, "agent_registration_confirmation_mismatch", "exact prepared revision digest is required; no registration was attempted"), nil
		}
		registered, created, err := s.app.RegisterFleetAgentAs(ctx, manager.Subject, app.NewRegisterFleetAgentInput(fixture, request.Arguments[3], request.Arguments[4]))
		if err != nil {
			return agentFailure(result, err)
		}
		readback, err := s.app.GetFleetAgentAs(ctx, manager.Subject, proposal.AgentID, proposal.Revision)
		if err != nil {
			return agentFailure(result, err)
		}
		if registered.Revision.Digest != expected || readback.Revision.Digest != expected {
			return Result{}, errors.New("Agent registration exact readback digest mismatch")
		}
		return completed(result, "agent_registration_confirmed", map[string]any{"created": created, "proposal": proposal, "agent": readback, "exact_readback_verified": true}), nil
	default:
		return failed(result, "invalid_agent_registry_operation", "closed Agent Registry operation required"), nil
	}
}

func readBoundedAgentInput(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	return readBoundedAgentFile(file, path)
}

func readBoundedAgentFile(file *os.File, path string) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q must be a regular file no larger than %d bytes", path, maximumAgentInputBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumAgentInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	if len(data) > maximumAgentInputBytes {
		return nil, fmt.Errorf("%q must be a regular file no larger than %d bytes", path, maximumAgentInputBytes)
	}
	return data, nil
}

func agentFailure(result Result, err error) (Result, error) {
	switch {
	case app.IsFleetUnavailable(err):
		result.State, result.Reason = "unavailable", "agent_registry_unavailable"
	case app.IsFleetNotFound(err):
		result.State, result.Reason = "failed", "agent_not_found"
	case app.IsFleetAmbiguous(err):
		result.State, result.Reason = "denied", "ambiguous_fleet_source"
	case app.IsFleetConflict(err), errors.Is(err, app.ErrConflict):
		result.State, result.Reason = "denied", "agent_registration_conflict"
	case errors.Is(err, app.ErrDenied):
		result.State, result.Reason = "denied", "agent_registry_authority_denied"
	default:
		result.State, result.Reason = "failed", "invalid_agent_registration"
	}
	result.Warnings = []string{err.Error()}
	return result, nil
}

func failed(result Result, reason, warning string) Result {
	result.State, result.Reason = "failed", reason
	result.Warnings = []string{warning}
	return result
}
