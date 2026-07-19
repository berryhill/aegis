package slash

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestRegistryHasExactlyCanonicalCore15(t *testing.T) {
	registry := testRegistry(t)
	want := []string{"help", "status", "context", "authority", "limits", "scan", "watch", "findings", "investigate", "timeline", "report", "audit", "cancel", "clear", "exit"}
	if got := registry.BaseNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("base names = %#v", got)
	}
	for _, definition := range registry.Definitions() {
		if definition.Name == "" || definition.Usage == "" || definition.Grammar == "" || definition.Class == "" || len(definition.States) == 0 || definition.Consequence == "" || definition.Policy == "" || definition.Audit == "" || definition.ResultSchema == "" || definition.Help == "" {
			t.Fatalf("incomplete typed definition: %+v", definition)
		}
	}
}

func TestParserCanonicalizesAliasesAndLeadingWhitespace(t *testing.T) {
	registry := testRegistry(t)
	for input, canonical := range map[string]string{" /quit": "exit", "\t/scan-secrets": "scan", "/scan-processes": "scan", "/scan-network": "scan", "/scan-files": "scan"} {
		request, err := registry.Parse(input)
		if err != nil || request.Canonical != canonical || request.Definition.Policy != "manager."+canonical {
			t.Fatalf("parse %q = %+v, %v", input, request, err)
		}
		if request.Alias == "" {
			t.Fatalf("alias not retained for %q", input)
		}
	}
	request, err := registry.Parse("/help 'scan'")
	if err != nil || !reflect.DeepEqual(request.Arguments, []string{"scan"}) {
		t.Fatalf("quoted parse = %+v, %v", request, err)
	}
}

func TestDetectionAndLiteralSlashEscape(t *testing.T) {
	for input, want := range map[string]Detection{"ordinary": Ordinary, " /status": Command, "\t//status": LiteralSlash} {
		if got := Detect(input); got != want {
			t.Fatalf("Detect(%q)=%v want %v", input, got, want)
		}
	}
	if got := UnescapeLiteral(" \t//status"); got != " \t/status" {
		t.Fatalf("literal unescape = %q", got)
	}
}

func TestParserRejectsUnknownMalformedAndShellSyntax(t *testing.T) {
	registry := testRegistry(t)
	inputs := []string{"/Status", "/unknown", "/status extra", "/audit nope", "/help 'unterminated", "/help a b", "/help $HOME", "/help $(id)", "/help a|b", "/help a>b", "/help a;b", "/help a&b", "/help !x", "/help `id`", "/help a\\ b"}
	for _, input := range inputs {
		t.Run(strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			if _, err := registry.Parse(input); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
	_, err := registry.Parse("/stat")
	var parseError *ParseError
	if !errors.As(err, &parseError) || parseError.Reason != "unknown_command" || len(parseError.Suggestions) == 0 {
		t.Fatalf("suggestion error = %#v, %v", parseError, err)
	}
}

func TestCompletionUsesRegistryAvailabilityWithoutExecution(t *testing.T) {
	registry := testRegistry(t)
	prerequisites := map[string]bool{"authenticated manager context": true, "finding store": true, "authoritative audit/event records": true, "audit authority": true}
	got := registry.Complete("/", Degraded, prerequisites)
	if !contains(got, "/help") || !contains(got, "/scan") || contains(got, "/watch") {
		t.Fatalf("degraded completion = %#v", got)
	}
	if !contains(registry.Complete("/qu", Active, prerequisites), "/quit") {
		t.Fatal("exit alias missing from registry completion")
	}
}

func TestRegistryDrivesAvailabilityPolicyAuditAndResultNames(t *testing.T) {
	registry := testRegistry(t)
	for _, name := range registry.BaseNames() {
		definition, ok := registry.Lookup(name)
		if !ok || definition.Policy != "manager."+name || definition.Audit != definition.Policy || definition.ResultSchema == "" {
			t.Fatalf("registry metadata drift for %q: %+v", name, definition)
		}
	}
	status, _ := registry.Lookup("status")
	for _, state := range []LifecycleState{Startup, Degraded, Active, Expiring} {
		if available, _ := registry.Available(status, state, map[string]bool{}); !available {
			t.Fatalf("status unavailable in %s", state)
		}
	}
	scan, _ := registry.Lookup("scan")
	if available, reason := registry.Available(scan, Revoked, map[string]bool{"authenticated manager context": true}); available || reason != "lifecycle_state_unavailable" {
		t.Fatalf("scan available after revocation: %v %s", available, reason)
	}
}

func FuzzParserNeverTreatsUnknownAsKnown(f *testing.F) {
	for _, seed := range []string{"/help", " /status", "//status", "/unknown", "/help 'scan'", "/help a|b", "/quit"} {
		f.Add(seed)
	}
	registry, _ := NewRegistry()
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > MaximumInputBytes+1 {
			return
		}
		request, err := registry.Parse(input)
		if err != nil {
			return
		}
		definition, ok := registry.Lookup(request.Name)
		if !ok || request.Canonical != definition.Name || request.Definition.Policy != definition.Policy {
			t.Fatalf("parser produced an unregistered request: %+v", request)
		}
	})
}
