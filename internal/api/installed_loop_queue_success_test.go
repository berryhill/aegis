package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/queue"
)

// Installed binary -> owning Unix peer-authenticated service -> real stores and
// worker -> synthetic gateway patch -> native TestHello. Not live Javi acceptance.
func TestInstalledOnlineLoopQueueSuccess(t *testing.T) {
	exactLoopQueueImplementationSuccessReplay(t, "online")
}

func TestAuthenticatedPreparationHTTPRoutes(t *testing.T) {
	exactLoopQueueImplementationSuccessReplay(t, "console")
}

type installedLoopQueueClient struct{ binary, config, target, root string }

func newInstalledLoopQueueClient(t *testing.T, svc *app.Service) *installedLoopQueueClient {
	t.Helper()
	root := t.TempDir()
	c := &installedLoopQueueClient{binary: filepath.Join(root, "aegis"), config: filepath.Join(root, "owner.json"), target: svc.Config.API.Console.Origin, root: root}
	build := exec.Command("go", "build", "-p=1", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-loop-queue-fixture", "-o", c.binary, "./cmd/aegis")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	raw, err := json.Marshal(svc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *installedLoopQueueClient) run(dest any, args ...string) error {
	time.Sleep(250 * time.Millisecond)
	out, err := exec.Command(c.binary, append([]string{"--config", c.config, "--target", c.target}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %w: %s", args, err, out)
	}
	if err := json.Unmarshal(out, dest); err != nil {
		return fmt.Errorf("decode %v: %w: %s", args, err, out)
	}
	return nil
}

func (c *installedLoopQueueClient) queue(input app.QueueLoopInput) (app.QueueLoopResult, error) {
	var result app.QueueLoopResult
	raw, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	// Separate files allow the concurrent in-flight replay without a writer race.
	file, err := os.CreateTemp(c.root, "request-*.json")
	if err != nil {
		return result, err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return result, err
	}
	if err = file.Close(); err != nil {
		return result, err
	}
	err = c.run(&result, "loops", "queue", file.Name())
	return result, err
}

func (c *installedLoopQueueClient) assertReadback(t *testing.T, input app.QueueLoopInput, result app.QueueLoopResult) {
	t.Helper()
	var got app.QueueExecutionView
	if err := c.run(&got, "queue", "show", result.QueueItemID); err != nil {
		t.Fatal(err)
	}
	if result.Reason != "processed" || got.Item.ItemID != result.QueueItemID || got.Item.Digest != result.Execution.Item.Digest || got.Submission.Digest != result.Execution.Submission.Digest || got.Projection.State != queue.StateSucceeded {
		t.Fatalf("incorrect exact readback: %+v", got)
	}
	if len(got.Claims) != 1 || len(got.Attempts) != 1 || len(got.LoopExecutions) != 1 {
		t.Fatalf("duplicate lineage: %+v", got)
	}
	key := strings.TrimSuffix(result.QueueItemID, "-queue")
	if got.Claims[0].ClaimID != key+"-claim" || got.Attempts[0].AttemptID != key+"-attempt" || got.Attempts[0].ClaimID != got.Claims[0].ClaimID || got.Attempts[0].QueueItem.ID != result.QueueItemID || got.LoopExecutions[0].LoopExecutionID != key+"-loop" || got.LoopExecutions[0].Loop != input.Loop || got.LoopExecutions[0].Participant != input.Agent {
		t.Fatal("exact execution identities lost")
	}
	if got.Artifact == nil || got.Artifact.ID != key+"-artifact" || got.Artifact.AttemptID != got.Attempts[0].AttemptID || got.Artifact.Validate() != nil || got.Disposition == nil || got.Disposition.State != execution.StateSucceeded || len(got.Receipts) == 0 {
		t.Fatal("missing independently verified success evidence")
	}
	for _, receipt := range got.Receipts {
		if receipt.Validate() != nil || receipt.Outcome != evidence.Passed || receipt.AttemptID != got.Attempts[0].AttemptID || receipt.ArtifactID != got.Artifact.ID {
			t.Fatalf("invalid exact receipt: %+v", receipt)
		}
	}
	if !reflect.DeepEqual(got, *result.Execution) {
		t.Fatal("online readback differs from execution result")
	}
	replay, err := c.queue(input)
	if err != nil || replay.QueueItemID != result.QueueItemID || replay.Graph != result.Graph || replay.Reason != "existing_execution" || replay.Execution == nil || !reflect.DeepEqual(*replay.Execution, got) {
		t.Fatalf("repeat changed execution: %+v %v", replay, err)
	}
}

func configurePreparationHTTP(t *testing.T, svc *app.Service) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	if err = probe.Close(); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
}

func assertPreparationHTTP(t *testing.T, client *http.Client, origin, id, state string) {
	t.Helper()
	routes := []string{"/console/preparations"}
	if id != "" {
		routes = append(routes, "/console/preparations?record_key="+url.QueryEscape(id))
	}
	for _, route := range routes {
		response, err := client.Get(origin + route)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("removed preparation route returned %d: %s", response.StatusCode, body)
		}
	}
	response, err := client.Get(origin + "/console/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("queue status=%d", response.StatusCode)
	}
	for _, forbidden := range []string{"/console/preparations", "Action prerequisites", "authority_context_required", "select_exact_authority_context", "Operation-specific authority", ">Claim<", ">Runtime effect<", ">Verify evidence<", ">Disposition<"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("authenticated Queue overview retained %q", forbidden)
		}
	}
	if !strings.Contains(string(body), `href="/console/graphs/run"`) {
		t.Fatal("queue entry point missing")
	}
}
