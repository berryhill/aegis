package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"

	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
)

func TestConsoleDoerTemplatePublishesWithoutRuntimeSession(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	subject, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	registerDoerWorkspaceAgent(t, svc, subject, "existing-agent", "owner-one")
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
	client, csrf := loginConsole(t, address)
	composer, err := client.Get("http://" + address + "/console/loops/doer")
	if err != nil {
		t.Fatal(err)
	}
	composerBody, err := io.ReadAll(composer.Body)
	_ = composer.Body.Close()
	if err != nil || composer.StatusCode != http.StatusOK || !bytes.Contains(composerBody, []byte("existing-agent")) {
		t.Fatalf("eligible workspace not rendered: status=%d err=%v", composer.StatusCode, err)
	}
	values := validDoerForm()
	values.Set("csrf", csrf)
	values.Set("workspace", t.TempDir())
	values.Set("publication_key", "http-doer-publish")
	request, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/doer/preview", strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://"+address)
	preview, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(preview.Body)
	_ = preview.Body.Close()
	if err != nil || preview.StatusCode != http.StatusOK {
		t.Fatalf("template preview status=%d err=%v body=%s", preview.StatusCode, err, body)
	}
	match := regexp.MustCompile(`name="intent_id" value="([A-Za-z0-9-]+)"`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatal("preview lacked exact confirmation intent")
	}
	before, err := svc.FleetRepository.ListLoopRevisions(context.Background())
	if err != nil || len(before) != 0 {
		t.Fatalf("preview mutated Loop: count=%d err=%v", len(before), err)
	}
	payload, err := json.Marshal(console.CommandExecuteRequest{SchemaVersion: console.CommandCatalogVersion, IntentID: string(match[1])})
	if err != nil {
		t.Fatal(err)
	}
	execute, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/api/commands/execute", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	execute.Header.Set("Content-Type", "application/json")
	execute.Header.Set("X-CSRF-Token", csrf)
	execute.Header.Set("Origin", "http://"+address)
	response, err := client.Do(execute)
	if err != nil {
		t.Fatal(err)
	}
	var receipt console.CommandReceipt
	err = json.NewDecoder(response.Body).Decode(&receipt)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || receipt.ReasonCode != "loop_revision_published" {
		t.Fatalf("template confirmation status=%d reason=%q err=%v", response.StatusCode, receipt.ReasonCode, err)
	}
	var readback struct {
		Published app.PublishedLoop `json:"published"`
		View      app.LoopView      `json:"view"`
	}
	if err := json.Unmarshal(receipt.Readback, &readback); err != nil {
		t.Fatal(err)
	}
	if readback.Published.Revision.Doer == nil || readback.View.Revision.Digest != readback.Published.Revision.Digest || readback.View.Lifecycle.State != "draft" {
		t.Fatalf("publication readback not exact inactive Doer: %+v", readback.View)
	}
	if _, err := svc.FleetCommandAuthorityAs(context.Background(), subject); err == nil {
		t.Fatal("template publication silently granted runtime authority")
	}
}
