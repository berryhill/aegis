package slash

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

func serviceFixture(t *testing.T) (*Service, *Registry, Context) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "hermes")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; exit 0; fi\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.StateDir = state.Root()
	cfg.HermesExecutable = executable
	cfg.Principal = config.Principal{ID: "principal", Name: "Principal", UID: "4242", User: "operator", AuthTTL: 5 * time.Minute}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application := app.New(cfg, state, hermes.New(executable, logger), logger)
	application.Current = func() (*user.User, error) { return &user.User{Uid: "4242", Username: "operator"}, nil }
	registry := testRegistry(t)
	service := NewService(application, registry)
	now := time.Now().UTC()
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	manager := Context{Subject: subject, StanzaID: managerdomain.SecurityContext, MandateID: subject.ID, MandateIssued: now, MandateExpiry: subject.ExpiresAt, MandateState: "active", Lifecycle: Degraded, RuntimeState: "degraded", Route: "local-only", PolicyVersion: managerdomain.PolicyVersion, PolicyDigest: managerdomain.PolicyDigest(), Readiness: map[string]string{"authority": "absent", "model": "absent", "artifact": "absent", "certification": "absent", "hermes": "healthy", "inference": "degraded"}}
	return service, registry, manager
}

func execute(t *testing.T, service *Service, registry *Registry, manager Context, input string) Result {
	t.Helper()
	request, err := registry.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Execute(context.Background(), manager, request)
	if err != nil {
		t.Fatalf("execute %s: %v (%+v)", input, err, result)
	}
	return result
}

func TestCoreScanCoverageEquivalenceAndTruthfulAdapterBoundary(t *testing.T) {
	service, registry, manager := serviceFixture(t)
	first := execute(t, service, registry, manager, "/scan")
	if first.State != "completed_no_findings" || first.OperationID == "" || first.EffectiveScope == nil || first.Coverage != "Aegis-native manager core checks only" {
		t.Fatalf("scan result = %+v", first)
	}
	if !strings.Contains(strings.ToLower(strings.Join(first.Warnings, " ")), "no findings in covered scope") {
		t.Fatalf("unsafe no-finding wording: %#v", first.Warnings)
	}
	second := execute(t, service, registry, manager, "/scan core")
	if second.Reason != "equivalent_scan_attached" || second.OperationID != first.OperationID {
		t.Fatalf("equivalence failed: first=%+v second=%+v", first, second)
	}
	manager.PolicyDigest = "sha256:changed-policy-generation"
	third := execute(t, service, registry, manager, "/scan")
	if third.OperationID == first.OperationID {
		t.Fatal("different policy/scope generation incorrectly deduplicated")
	}
	unavailable := execute(t, service, registry, manager, "/scan processes")
	if unavailable.State != "unavailable" || unavailable.Reason != "scan_adapter_unavailable" {
		t.Fatalf("host scan was not truthful unavailable: %+v", unavailable)
	}
}

func TestFindingDisclosureInvestigationAndFrozenReportRevisions(t *testing.T) {
	service, registry, manager := serviceFixture(t)
	now := time.Now().UTC()
	visible := Finding{Schema: "aegis.finding.v1", ID: "finding-visible", OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, RuleID: "test.rule", RuleVersion: "v1", Severity: "medium", Confidence: "high", State: "open", History: []FindingHistory{{State: "open", At: now, Actor: manager.Subject.ID, Reason: "observed"}}, Target: "typed fixture", Source: "authoritative-test-source", Scope: Scope{Kind: "manager-session", Digest: "sha256:visible"}, Health: "healthy", Coverage: "fixture scope", EvidenceRefs: []string{"evidence-visible"}, FirstObserved: now, LastObserved: now}
	restricted := visible
	restricted.ID, restricted.OwnerID, restricted.EvidenceRefs = "finding-restricted", "other-owner", []string{"restricted-canary"}
	if err := service.app.Store.Save("manager-findings", visible.ID, visible); err != nil {
		t.Fatal(err)
	}
	if err := service.app.Store.Save("manager-findings", restricted.ID, restricted); err != nil {
		t.Fatal(err)
	}
	list := execute(t, service, registry, manager, "/findings")
	rendered, _ := json.Marshal(list.Data)
	if list.Reason != "visible_open_findings" || strings.Contains(string(rendered), "restricted-canary") || strings.Contains(string(rendered), "finding-restricted") {
		t.Fatalf("finding disclosure failure: %+v", list)
	}
	missing := execute(t, service, registry, manager, "/findings finding-restricted")
	if missing.Reason != "finding_not_found_or_not_visible" || len(missing.RelatedIDs) != 0 {
		t.Fatalf("restricted detail leaked distinction: %+v", missing)
	}
	bare := execute(t, service, registry, manager, "/investigate")
	if bare.Reason != "no_active_investigations_no_target_selected" {
		t.Fatalf("bare investigation selected a target: %+v", bare)
	}
	created := execute(t, service, registry, manager, "/investigate finding-visible")
	if created.Reason != "investigation_created" {
		t.Fatalf("investigation = %+v", created)
	}
	report1 := execute(t, service, registry, manager, "/report")
	report2 := execute(t, service, registry, manager, "/report")
	one := report1.Data["report"].(Report)
	two := report2.Data["report"].(Report)
	if one.Revision != 1 || two.Revision != 2 || one.ID == two.ID || one.Digest == "" || two.Digest == "" || report1.Data["exported"] != false || report2.Data["published"] != false {
		t.Fatalf("report revisions not frozen/local: one=%+v two=%+v", one, two)
	}
}

func TestAuthorityAndCancelFailClosed(t *testing.T) {
	service, registry, manager := serviceFixture(t)
	bad := manager
	bad.StanzaID = "another-stanza"
	request, _ := registry.Parse("/status")
	if _, err := service.Execute(context.Background(), bad, request); err == nil {
		t.Fatal("stanza switch was accepted")
	}
	cancel := execute(t, service, registry, manager, "/cancel")
	if cancel.Reason != "no_foreground_operation" || cancel.Data["rollback_claimed"] != false {
		t.Fatalf("cancel guessed or claimed rollback: %+v", cancel)
	}
	clear := execute(t, service, registry, manager, "/clear")
	if clear.Reason != "presentation_only" {
		t.Fatalf("clear mutated workflow: %+v", clear)
	}
}
