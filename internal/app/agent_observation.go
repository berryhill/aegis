package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/registry"
	"golang.org/x/sys/unix"
)

// AgentObservation is a read-only precondition observation, never execution
// admission. It issues no mandate, changes no lifecycle and grants no authority.
// Only closed diagnostic codes leave this boundary, never artifact contents.
type AgentObservation struct{ Runtime, Provisioning string }

func (s *Service) ObserveAgentPreconditionsAs(ctx context.Context, subject core.Subject, revision registry.AgentRevision) (AgentObservation, error) {
	out := AgentObservation{Runtime: "unavailable", Provisioning: "unavailable"}
	if err := s.RequirePrincipalIdentity(subject); err != nil {
		return out, err
	}
	c, err := s.GetCharter(revision.Charter.ID, revision.Charter.Revision)
	if err != nil || c.Digest != revision.Charter.Digest || c.Charter.Runtime.Adapter != revision.Runtime.Adapter || c.Charter.Runtime.Runtime != revision.Runtime.Runtime || c.Charter.Runtime.Target != revision.Runtime.Target {
		return out, nil
	}
	if s.Hermes != nil {
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		rt, e := s.Runtime(probe)
		cancel()
		if e == nil {
			out.Runtime = "incompatible"
			if rt.Runtime == c.Charter.Runtime.Runtime && runtimeSatisfies(rt.Version, c.Charter.Runtime.VersionConstraint) == nil {
				out.Runtime = "compatible"
			}
		}
	}
	receipts, err := s.ListReceipts()
	if err != nil {
		return out, nil
	}
	out.Provisioning = "missing"
	for _, r := range receipts {
		if r.CharterDigest != c.Digest {
			continue
		}
		out.Provisioning = "unverified"
		if r.Status != "verified" {
			continue
		}
		p, e := s.GetPlan(r.PlanID)
		if e != nil || p.Digest != r.PlanDigest || p.CharterDigest != c.Digest || p.AgentID != c.Charter.AgentID || p.Revision != c.Charter.Revision || p.Runtime.Runtime != c.Charter.Runtime.Runtime || p.Environment.Name != "local" {
			continue
		}
		a, e := s.GetApproval(r.ApprovalID)
		if e != nil || a.Status != "consumed" || a.PlanID != p.ID || a.PlanDigest != p.Digest || a.CharterDigest != c.Digest || a.Runtime != p.Runtime.Runtime || a.RuntimeVersion != p.Runtime.Version || a.Environment != p.Environment {
			continue
		}
		if len(p.Effects) != 1 || len(r.Artifacts) != len(p.Effects) {
			continue
		}
		type provisionedPayload struct {
			AgentID       string                 `json:"agent_id"`
			CharterDigest string                 `json:"charter_digest"`
			Revision      uint64                 `json:"revision"`
			Runtime       core.RuntimeConstraint `json:"runtime"`
			Stanzas       []core.TrustStanza     `json:"stanzas"`
		}
		payload := provisionedPayload{c.Charter.AgentID, c.Digest, c.Charter.Revision, c.Charter.Runtime, c.Charter.Stanzas}
		expected := core.Digest(payload)
		expectedBytes, _ := json.MarshalIndent(payload, "", "  ")
		expectedBytes = append(expectedBytes, '\n')
		valid := true
		provisioned := filepath.Join(s.Store.Root(), "provisioned")
		expectedTarget := filepath.Join(provisioned, c.Charter.AgentID, strconv.FormatUint(c.Charter.Revision, 10), "hermes-config.json")
		root, e := os.OpenRoot(provisioned)
		if e != nil {
			continue
		}
		for i, effect := range p.Effects {
			artifact := r.Artifacts[i]
			if effect.Kind != core.EffectCreateFile || effect.Target != expectedTarget || effect.Digest != expected || artifact.Path != effect.Target || artifact.Action != effect.Kind || !artifact.Verified || artifact.Digest != expected {
				valid = false
				break
			}
			relative, e := filepath.Rel(provisioned, effect.Target)
			if e != nil {
				valid = false
				break
			}
			f, e := root.OpenFile(relative, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if e != nil {
				valid = false
				break
			}
			info, statErr := f.Stat()
			if statErr != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
				f.Close()
				valid = false
				break
			}
			b, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
			f.Close()
			var applied provisionedPayload
			if e != nil || len(b) > 4<<20 || json.Unmarshal(b, &applied) != nil || core.Digest(applied) != expected || !bytes.Equal(b, expectedBytes) {
				valid = false
				break
			}
		}
		root.Close()
		if valid {
			out.Provisioning = "verified-current"
			break
		}
	}
	return out, nil
}
