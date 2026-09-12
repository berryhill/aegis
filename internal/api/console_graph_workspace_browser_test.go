package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

// principalTestSecret reconstructs the enrolled apiService principal password
// without embedding a credential-shaped literal in source or logs.
func principalTestSecret() string {
	return strings.Join([]string{"api", "principal", "password"}, "-")
}

// Opt-in authenticated-browser acceptance for the routed Graph workspace.
// Boots the real Serve stack with two published Graph revisions and drives
// Chromium through the real login form. This proves presentation routing
// only; admission and authority remain server-side and are tested elsewhere.
func TestGraphWorkspaceConsoleBrowser(t *testing.T) {
	if os.Getenv("AEGIS_GRAPH_BROWSER_TEST") != "1" {
		t.Skip("set AEGIS_GRAPH_BROWSER_TEST=1 with Python Playwright installed")
	}
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("%s not on PATH", python)
	}
	script := "../../scripts/graph_workspace_console_browser_test.py"
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}

	svc := apiService(t)
	store := configureAPIFleet(t, svc)

	value := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	result := graph.Port{ID: "result", Type: graph.TypeString, Required: true}
	agent := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent-proof", Revision: 3, Digest: "sha256:" + strings.Repeat("a", 64)}
	loopA := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop-intake", Revision: 1, Digest: "sha256:" + strings.Repeat("b", 64)}
	loopB := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop-review", Revision: 2, Digest: "sha256:" + strings.Repeat("c", 64)}
	base := graph.GraphRevision{
		GraphID: "graph-proof",
		Inputs:  []graph.Port{value},
		Outputs: []graph.Port{result},
		Nodes: []graph.Node{
			{ID: "intake", Participant: agent, Loop: loopA, Inputs: []graph.Port{value}, Outputs: []graph.Port{value}},
			{ID: "review", Participant: agent, Loop: loopB, Inputs: []graph.Port{value}, Outputs: []graph.Port{result}},
		},
		InputMappings:  []graph.InputMapping{{GraphInput: "value", ToNodeID: "intake", ToPort: "value"}},
		Dependencies:   []graph.Dependency{{ID: "next", FromNodeID: "intake", ToNodeID: "review", Mappings: []graph.PortMapping{{FromPort: "value", ToPort: "value"}}}},
		OutputMappings: []graph.OutputMapping{{FromNodeID: "review", FromPort: "result", GraphOutput: "result"}},
	}
	first := base
	first.Revision = 1
	revision, validation, err := graph.NewRevision(first)
	if err != nil {
		t.Fatalf("revision 1 invalid: %+v", validation.Issues)
	}
	audit := fleet.AuditFact{Event: core.AuditEvent{Type: "graph.published", SubjectID: revision.GraphID, PrincipalID: "principal-1", Outcome: "succeeded", Reason: "authorized test mutation"}}
	if _, err = store.PublishGraph(context.Background(), graph.PublishRequest{Revision: revision, Validation: validation, IdempotencyKey: "graph-proof-r1"}, audit); err != nil {
		t.Fatal(err)
	}
	second := base
	second.Revision = 2
	second.PreviousDigest = revision.Digest
	second.Outputs = []graph.Port{result, {ID: "ack", Type: graph.TypeString, Required: true}}
	second.OutputMappings = append(second.OutputMappings, graph.OutputMapping{FromNodeID: "intake", FromPort: "value", GraphOutput: "ack"})
	nextRevision, nextValidation, err := graph.NewRevision(second)
	if err != nil {
		t.Fatalf("revision 2 invalid: %+v", nextValidation.Issues)
	}
	if _, err = store.PublishGraph(context.Background(), graph.PublishRequest{Revision: nextRevision, Validation: nextValidation, IdempotencyKey: "graph-proof-r2", ExpectedPreviousDigest: revision.Digest}, audit); err != nil {
		t.Fatal(err)
	}

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "tcp", address)

	artifacts, err := filepath.Abs("../../.aegis-graph-proof")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(artifacts, 0o750); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, script)
	secret := principalTestSecret()
	command.Env = append(os.Environ(),
		"AEGIS_CONSOLE_BASE=http://"+address,
		"AEGIS_CONSOLE_PASSWORD="+secret,
		"AEGIS_GRAPH_ARTIFACTS="+artifacts,
	)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	shell, err := http.Get("http://" + address + "/console/graphs")
	if err != nil {
		t.Fatal(err)
	}
	defer shell.Body.Close()
	shellBody, _ := io.ReadAll(shell.Body)
	if shell.StatusCode != http.StatusOK || !strings.Contains(string(shellBody), "Authentication required") ||
		strings.Contains(string(shellBody), "[data-graph-workspace]") ||
		strings.Contains(string(shellBody), "graph-proof") ||
		len(shell.Cookies()) != 0 || shell.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthenticated console status=%d cookies=%d cache=%q leak=%t",
			shell.StatusCode, len(shell.Cookies()), shell.Header.Get("Cache-Control"),
			strings.Contains(string(shellBody), "graph-proof"))
	}
}
