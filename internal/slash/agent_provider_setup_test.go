package slash

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

func TestAgentProviderSetupManagerMigrationAndAuthority(t *testing.T) {
	service, registry, manager := serviceFixture(t)
	configureSlashFleet(t, service)
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("model: {provider: openai-codex, default: gpt-5.4}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service.app.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.app.LocalHermesHome = func(string, string) (string, error) { return home, nil }
	manager.Subject.ID = "local-uid:" + service.app.Config.Principal.UID
	manager.Subject.Claims = map[string]string{"uid": service.app.Config.Principal.UID}
	manager.Subject.Kind = "human"
	p := execute(t, service, registry, manager, "/agents import hermes default").Data["proposal"].(app.LocalHermesAgentImportProposal)
	confirmed := execute(t, service, registry, manager, p.Confirmation)
	next, ok := confirmed.Data["next"].(string)
	if !ok {
		t.Fatal("registration continuation missing")
	}
	preview := execute(t, service, registry, manager, next)
	setup, ok := preview.Data["setup"].(app.AgentProviderSetupResult)
	if !ok || setup.Review == nil {
		t.Fatalf("service review missing: %+v", preview)
	}
	impostor := manager
	impostor.Subject.Kind = "agent"
	if denied := execute(t, service, registry, impostor, setup.Next); denied.State != "denied" {
		t.Fatal("manager subject laundered into human")
	}
	approved := execute(t, service, registry, manager, setup.Next)
	result, ok := approved.Data["setup"].(app.AgentProviderSetupResult)
	if !ok || result.Agent.Revision.Revision != 2 || result.Models.ProviderAuthentication.Mode != core.ConfiguredProviderV1 || result.Executable {
		t.Fatalf("migration not applied: %+v", approved)
	}
	read, err := service.app.GetFleetAgentAs(context.Background(), manager.Subject, p.AgentID, 2)
	if err != nil || read.Revision.Digest != result.Agent.Revision.Digest {
		t.Fatal("exact readback differs")
	}
}
