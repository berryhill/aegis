package slash

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
)

func TestLocalHermesModelImportExplicitReviewAndReplay(t *testing.T) {
	service, registry, manager := serviceFixture(t)
	configureSlashFleet(t, service)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "config.yaml")
	config := "model: {provider: openai-codex, default: gpt-5.4, api_key: secret-canary}\nfallback_providers: [{provider: antigravity-oauth, model: gemini-3-pro}]\n"
	if err := os.WriteFile(marker, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	service.app.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.app.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	manager.Subject.ID = "local-uid:" + service.app.Config.Principal.UID
	manager.Subject.Claims = map[string]string{"uid": service.app.Config.Principal.UID}
	prepared := execute(t, service, registry, manager, "/agents import hermes default with-models")
	p, ok := prepared.Data["proposal"].(app.LocalHermesAgentImportProposal)
	if prepared.State != "completed" || !ok || p.ModelSnapshot == nil || p.ModelSnapshot.DefaultModel != "gpt-5.4" || len(p.ModelSnapshot.Lanes) != 2 {
		t.Fatalf("snapshot preview: %+v", prepared)
	}
	for _, command := range []string{strings.Replace(p.Confirmation, " with-models", "", 1), "/agents import hermes default with-models confirm sha256:" + strings.Repeat("a", 64)} {
		denied := execute(t, service, registry, manager, command)
		if denied.State != "denied" {
			t.Fatalf("wrong approval accepted: %+v", denied)
		}
	}
	if err := os.WriteFile(marker, []byte(strings.Replace(config, "gpt-5.4", "gpt-5.5", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if denied := execute(t, service, registry, manager, p.Confirmation); denied.State != "denied" {
		t.Fatalf("drift: %+v", denied)
	}
	prepared = execute(t, service, registry, manager, "/agents import hermes default with-models")
	p = prepared.Data["proposal"].(app.LocalHermesAgentImportProposal)
	confirmed := execute(t, service, registry, manager, p.Confirmation)
	if confirmed.State != "completed" || confirmed.Data["created"] != true || confirmed.Data["authentication"] != "not_connected" || confirmed.Data["qualification"] != "not_checked" {
		t.Fatalf("confirmation: %+v", confirmed)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	replay := execute(t, service, registry, manager, p.Confirmation)
	if replay.State != "completed" || replay.Data["created"] != false {
		t.Fatalf("consumed replay: %+v", replay)
	}
	readback := execute(t, service, registry, manager, "/agents show "+p.AgentID+" 1")
	agent, ok := readback.Data["agent"].(app.FleetAgent)
	if !ok || agent.Revision.Charter.Digest != p.CharterDigest || agent.Revision.Digest != p.RevisionDigest {
		t.Fatal("exact registration readback mismatch")
	}
}
