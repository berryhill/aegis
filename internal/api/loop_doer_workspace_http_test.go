package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"

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
	agent := registerDoerWorkspaceAgent(t, svc, subject, "existing-agent", "owner-one")
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
	values.Set("draft_id", "")
	values.Set("draft_version", "0")
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
	if !bytes.Contains(body, []byte("Agent revision")) || !bytes.Contains(body, []byte("Agent digest")) || !bytes.Contains(body, []byte(agent.Revision.Digest)) {
		t.Fatal("confirmation omitted exact publisher Agent revision or digest")
	}
	// This Registry-only fixture intentionally has no imported exact charter.
	for _, want := range []string{"Before publication", "This candidate is not advertised as runnable", "exact_charter_unavailable", "Return to this retained draft"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("registry-only preview omitted readiness or retention: %s", want)
		}
	}
	draftLink := regexp.MustCompile(`href="(/console/loops/doer\?draft_id=doerdraft-[a-f0-9]{32})"`).FindSubmatch(body)
	if len(draftLink) != 2 {
		t.Fatal("review did not retain a principal-scoped draft")
	}
	resumed, err := client.Get("http://" + address + string(draftLink[1]))
	if err != nil {
		t.Fatal(err)
	}
	resumedBody, err := io.ReadAll(resumed.Body)
	_ = resumed.Body.Close()
	if err != nil || resumed.StatusCode != http.StatusOK {
		t.Fatalf("draft resume status=%d err=%v", resumed.StatusCode, err)
	}
	for _, want := range []string{values.Get("task"), values.Get("workspace"), values.Get("verify_file"), `name="draft_version" value="1"`, `name="max_attempts"`, `name="publication_key" value="doerpub-`} {
		if !bytes.Contains(resumedBody, []byte(want)) {
			t.Fatalf("resumed draft lost operator input or stable publication identity: %s", want)
		}
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
	confirmation, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/execute", strings.NewReader("csrf="+csrf+"&intent_id="+string(match[1])))
	if err != nil {
		t.Fatal(err)
	}
	confirmation.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmation.Header.Set("Origin", "http://"+address)
	confirmed, err := client.Do(confirmation)
	if err != nil {
		t.Fatal(err)
	}
	confirmationBody, err := io.ReadAll(confirmed.Body)
	_ = confirmed.Body.Close()
	if err != nil || confirmed.StatusCode != http.StatusOK || !bytes.Contains(confirmationBody, []byte("Inspect exact published Loop")) || !bytes.Contains(confirmationBody, []byte("record_key=doer-task%3A1")) {
		t.Fatalf("exact published Loop link missing: status=%d err=%v", confirmed.StatusCode, err)
	}
	for _, want := range []string{`action="/console/loops/run?loop_id=doer-task"`, `Run published Loop`, `name="digest" value="` + readback.Published.Revision.Digest + `"`, `name="revision" value="1"`, `name="csrf" value="` + csrf + `"`} {
		if !bytes.Contains(confirmationBody, []byte(want)) {
			t.Fatalf("publication did not retain exact inline Run binding: %s", want)
		}
	}
	runKey := regexp.MustCompile(`name="idempotency_key" value="(loop-run-[a-f0-9]{32})"`).FindSubmatch(confirmationBody)
	if len(runKey) != 2 {
		t.Fatal("inline Run has no recovery identity")
	}
	inspector, err := client.Get("http://" + address + consoleRecordURL(consoleLoops, "doer-task:1"))
	if err != nil {
		t.Fatal(err)
	}
	inspectorBody, err := io.ReadAll(inspector.Body)
	_ = inspector.Body.Close()
	if err != nil || inspector.StatusCode != http.StatusOK || !bytes.Contains(inspectorBody, runKey[1]) {
		t.Fatal("publication-to-inspector navigation substituted the retained Run identity")
	}
	inlineValues := url.Values{"csrf": {csrf}, "revision": {"1"}, "digest": {readback.Published.Revision.Digest}, "idempotency_key": {string(runKey[1])}}
	var blockedOperation []byte
	for n := 0; n < 2; n++ {
		run, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/run?loop_id=doer-task", strings.NewReader(inlineValues.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		run.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		run.Header.Set("Origin", "http://"+address)
		result, err := client.Do(run)
		if err != nil {
			t.Fatal(err)
		}
		resultBody, err := io.ReadAll(result.Body)
		_ = result.Body.Close()
		if err != nil || result.StatusCode != http.StatusOK || !bytes.Contains(resultBody, []byte("exact_charter_unavailable")) || !bytes.Contains(resultBody, runKey[1]) {
			t.Fatalf("inline Run failed to return its authoritative blocker/key: status=%d err=%v", result.StatusCode, err)
		}
		operation := regexp.MustCompile(`request_id: (loopq-[a-f0-9]{32})`).FindSubmatch(resultBody)
		if len(operation) != 2 {
			t.Fatalf("blocked Run lacks authoritative request identity: %s", resultBody)
		}
		if n == 0 {
			blockedOperation = append([]byte(nil), operation[1]...)
		} else if !bytes.Equal(blockedOperation, operation[1]) {
			t.Fatal("same-key Run replay created another request")
		}
	}
	view, err := svc.GetLoopViewAs(context.Background(), subject, "doer-task", 1)
	if err != nil || view.Lifecycle.State != "draft" || len(view.History) != 0 {
		t.Fatal("registry-only Run activated an immutable draft")
	}
	confirmationAgain, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/execute", strings.NewReader("csrf="+csrf+"&intent_id="+string(match[1])))
	if err != nil {
		t.Fatal(err)
	}
	confirmationAgain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmationAgain.Header.Set("Origin", "http://"+address)
	again, err := client.Do(confirmationAgain)
	if err != nil {
		t.Fatal(err)
	}
	againBody, err := io.ReadAll(again.Body)
	_ = again.Body.Close()
	if err != nil || again.StatusCode != http.StatusOK || !bytes.Contains(againBody, runKey[1]) {
		t.Fatal("confirmation replay rotated the execution identity")
	}
	if _, err := svc.FleetCommandAuthorityAs(context.Background(), subject); err == nil {
		t.Fatal("template publication silently granted runtime authority")
	}
}
