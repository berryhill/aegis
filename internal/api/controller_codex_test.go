package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"go.yaml.in/yaml/v3"
)

// Real owning daemon + kernel Unix peer + qualified installed native parser.
// All credential bytes below are synthetic; no installed profile is opened.
func TestControllerCodexOwningTransport(t *testing.T) {
	install := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
	if install == "" {
		t.Skip("explicit native installation required")
	}
	s := apiService(t)
	configureAPIFleet(t, s)
	u, e := user.Current()
	if e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp(u.HomeDir, ".aegis-connection-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	home := filepath.Join(root, ".hermes", "profiles", "synthetic")
	if e = os.MkdirAll(home, 0700); e != nil {
		t.Fatal(e)
	}
	syntheticUser := *u
	syntheticUser.HomeDir = root
	s.Current = func() (*user.User, error) { return &syntheticUser, nil }
	executable := filepath.Join(root, "qualified-hermes")
	if e = os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'Hermes Agent v0.21.3\\nInstall directory: "+install+"\\n'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	s.Config.HermesExecutable = executable
	s.Hermes = hermes.New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Reinstall the real service resolver after replacing only discovery fixture.
	s2 := app.New(s.Config, s.Store, s.Authority, s.AuthorityCommands, s.Hermes, s.Log)
	s2.Current = s.Current
	s2.QueueWorker = s.QueueWorker
	s2.Fleet = s.Fleet
	s2.FleetRepository = s.FleetRepository
	s = s2
	s.ConfigFile = filepath.Join(root, "owner.yaml")
	data, _ := json.Marshal(s.Config)
	var mapping map[string]any
	json.Unmarshal(data, &mapping)
	data, _ = yaml.Marshal(mapping)
	original := append([]byte("# unrelated bytes preserved\n"), data...)
	if e = os.WriteFile(s.ConfigFile, original, 0600); e != nil {
		t.Fatal(e)
	}
	token := ""
	poolSource := "manual:device_code"
	multiple := false
	entryID := "primary"
	refreshOffset := time.Duration(0)
	save := func(account string) {
		claims, _ := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour + refreshOffset).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account}})
		token = "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".synthetic"
		// Pool-only native login: no singleton may be required by API/CLI
		// preview, account-drift admission, persisted pin, or running resolver.
		entries := []any{map[string]any{"id": entryID, "label": "LABEL_CANARY", "auth_type": "oauth", "source": poolSource, "access_token": token, "refresh_token": "REFRESH_CANARY"}}
		if multiple {
			otherClaims, _ := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "other-account"}})
			entries = append(entries, map[string]any{"id": "secondary", "auth_type": "oauth", "source": "device_code", "access_token": "header." + base64.RawURLEncoding.EncodeToString(otherClaims) + ".synthetic"})
		}
		store, _ := json.Marshal(map[string]any{"version": 1, "providers": map[string]any{}, "credential_pool": map[string]any{"openai-codex": entries}})
		if e = os.WriteFile(filepath.Join(home, "auth.json"), store, 0600); e != nil {
			t.Fatal(e)
		}
	}
	save("synthetic-account")
	ctx, cancel := context.WithCancel(context.Background())
	sub, e := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	fixture, _ := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "connection-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "existing", AgentID: "existing", Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/synthetic"}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "existing", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}}}})
	if _, _, e = s.RegisterFleetAgentAs(ctx, sub, app.NewRegisterFleetAgentInput(fixture, "connection-fleet", "existing")); e != nil {
		t.Fatal(e)
	}
	revision, _, e := loop.NewDoerRevision("preserved-loop", 1, "", loop.DoerContract{Task: "Preserve this exact immutable Loop", Workspace: root, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishLoopAs(ctx, sub, app.PublishLoopInput{AgentID: "existing", Revision: revision, IdempotencyKey: "preserved-publication"}); e != nil {
		t.Fatal(e)
	}
	beforeLoop, e := s.GetLoopAs(ctx, sub, "preserved-loop", 1)
	if e != nil {
		t.Fatal(e)
	}
	binary := os.Getenv("AEGIS_TEST_CONTROLLER_CODEX_BINARY")
	if binary == "" {
		binary = filepath.Join(root, "aegis-test")
		build := exec.Command("go", "build", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-connection-test", "-o", binary, "./cmd/aegis")
		build.Dir = "../.."
		if _, e = build.CombinedOutput(); e != nil {
			t.Fatal("CLI build failed", e)
		}
	}
	cli := func(args ...string) app.ControllerCodexConnection {
		t.Helper()
		time.Sleep(500 * time.Millisecond)
		c := exec.Command(binary, append([]string{"--config", s.ConfigFile, "--target", s.Config.API.Console.Origin, "controller-codex"}, args...)...)
		raw, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("typed CLI failed: %v %s", e, raw)
		}
		if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("REFRESH_CANARY")) || bytes.Contains(raw, []byte("LABEL_CANARY")) {
			t.Fatal("CLI source leak")
		}
		var out app.ControllerCodexConnection
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal("invalid typed CLI response")
		}
		return out
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	defer func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	client := unixClient(s.Config.API.UnixSocket)
	call := func(method, path string, in any, want int) app.ControllerCodexConnection {
		t.Helper()
		time.Sleep(240 * time.Millisecond)
		b, _ := json.Marshal(in)
		r, _ := http.NewRequest(method, "http://unix"+path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+s.Config.API.Token)
		r.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if bytes.Contains(out, []byte(token)) || bytes.Contains(out, []byte("REFRESH_CANARY")) {
			t.Fatal("source leaked")
		}
		if resp.StatusCode != want {
			t.Fatalf("%s status %d want %d: %s", path, resp.StatusCode, want, out)
		}
		var result app.ControllerCodexConnection
		json.Unmarshal(out, &result)
		return result
	}
	// Unknown/caller-authored approval metadata is rejected by the real decoder.
	call("POST", "/v1/controller/codex/preview", map[string]any{"profile_home": home, "approved_by": "forged"}, 400)
	call("POST", "/v1/controller/codex/candidates", map[string]any{"profile_home": home, "credential_id": "forged"}, 400)
	call("POST", "/v1/controller/codex/candidates", app.ControllerCodexCandidatesInput{ProfileHome: "/other/.hermes"}, 403)
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: "unknown"}, 403)
	call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: "/other/.hermes/profiles/x"}, 403)
	// Unapproved borrowed source is diagnosed, not read or reported as absent.
	poolSource = "global:device_code"
	save("synthetic-account")
	call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: home}, 409)
	denied := exec.Command(binary, "--config", s.ConfigFile, "--target", s.Config.API.Console.Origin, "controller-codex", "preview", home)
	rawDenied, deniedErr := denied.CombinedOutput()
	if deniedErr == nil || !bytes.Contains(rawDenied, []byte("doer_provider_auth_native_source_approval_required")) || bytes.Contains(rawDenied, []byte(token)) || bytes.Contains(rawDenied, []byte("REFRESH_CANARY")) {
		t.Fatal("typed sanitized source approval gate missing")
	}
	poolSource = "manual:device_code"
	save("synthetic-account")
	review := call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: home}, 200)
	if review.AccountID != "synthetic-account" || review.PrincipalID != s.Config.Principal.ID || review.ReviewID == "" {
		t.Fatal("metadata preview missing")
	}
	// Changed native account cannot apply a reviewed binding.
	save("changed-account")
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: review.ReviewID}, 409)
	save("synthetic-account")
	review = call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: home}, 200)
	// Config drift denies; the owning config is never overwritten.
	os.WriteFile(s.ConfigFile, append(original, []byte("# changed\n")...), 0600)
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: review.ReviewID}, 409)
	os.WriteFile(s.ConfigFile, original, 0600)
	multiple = true
	save("synthetic-account")
	call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: home}, 409)
	sourceBefore, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	inspect := exec.Command(binary, "--config", s.ConfigFile, "--target", s.Config.API.Console.Origin, "controller-codex", "candidates", home)
	time.Sleep(500 * time.Millisecond)
	raw, e := inspect.CombinedOutput()
	if e != nil {
		t.Fatalf("candidate CLI: %v %s", e, raw)
	}
	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte("CANARY")) || bytes.Contains(raw, []byte("access_token")) {
		t.Fatal("candidate CLI leak")
	}
	var candidates hermes.NativeCodexCandidates
	if json.Unmarshal(raw, &candidates) != nil || len(candidates.Candidates) != 2 || candidates.DistinctEligibleGrants != 2 {
		t.Fatal("candidate metadata incomplete")
	}
	chosen := candidates.Candidates[0].CredentialID
	if !hermes.ValidNativeCredentialID(chosen) || !candidates.Candidates[0].Eligible {
		t.Fatal("missing exact candidate")
	}
	sourceAfterInspection, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if !bytes.Equal(sourceBefore, sourceAfterInspection) {
		t.Fatal("inspection changed source bytes")
	}
	configAfter, _ := os.ReadFile(s.ConfigFile)
	if !bytes.Equal(configAfter, original) {
		t.Fatal("inspection changed config")
	}
	review = cli("preview", home, "--credential", chosen)
	if review.CredentialID != chosen || review.AccountID != "synthetic-account" {
		t.Fatal("exact review missing")
	}
	// Entry ID replacement denies this single-use review even with the same account.
	entryID = "replacement"
	save("synthetic-account")
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: review.ReviewID}, 409)
	entryID = "primary"
	save("synthetic-account")
	sourceBefore, _ = os.ReadFile(filepath.Join(home, "auth.json"))
	review = cli("preview", home, "--credential", chosen)
	readerStop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-readerStop:
				return
			default:
			}
			_, _ = s.Hermes.ResolveProviderAuthentication(ctx, core.HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	out := cli("connect", review.ReviewID)
	close(readerStop)
	<-readerDone
	if out.Status != "connected" || out.AccountID != review.AccountID || out.CredentialID != chosen {
		t.Fatal("connection not applied")
	}
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: review.ReviewID}, 403)
	shown := cli("show")
	if shown.CredentialID != chosen || shown.Status != "connected" || shown.AccountID != out.AccountID {
		t.Fatal("CLI exact readback differs")
	}
	read := call("GET", "/v1/controller/codex", nil, 200)
	if read.AccountID != out.AccountID {
		t.Fatal("binding readback differs")
	}
	actual, e := config.ReadControllerCodexBinding(s.ConfigFile)
	if e != nil || actual == nil || actual.AccountID != out.AccountID || actual.CredentialID != chosen {
		t.Fatal("persisted binding unavailable", e)
	}
	after, _ := os.ReadFile(s.ConfigFile)
	if !bytes.Contains(after, []byte("# unrelated bytes preserved")) || bytes.Contains(after, []byte("REFRESH_CANARY")) || bytes.Contains(after, []byte(token)) {
		t.Fatal("configuration leak/preservation")
	}
	// Existing daemon adopts the binding with no Config mutation, authority grant,
	// or replacement Loop. Exercise its registered runtime resolver via readiness.
	h := core.HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e != nil {
		t.Fatal("running resolver did not adopt connection", e)
	}
	sourceAfterConnection, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if !bytes.Equal(sourceBefore, sourceAfterConnection) {
		t.Fatal("connection changed source bytes")
	}
	// A native refresh may advance this same entry/account without reselection.
	refreshOffset = time.Minute
	save("synthetic-account")
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e != nil {
		t.Fatal("same-entry refresh denied", e)
	}
	sourceAfter, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if bytes.Equal(sourceBefore, sourceAfter) {
		t.Fatal("synthetic refresh did not advance token")
	}
	entryID = "replacement"
	save("synthetic-account")
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e == nil {
		t.Fatal("resolver picked another entry")
	}
	entryID = "primary"
	save("changed-account")
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e == nil {
		t.Fatal("resolver ignored selected account drift")
	}
	save("synthetic-account")
	// Rebinding is explicit, reviewed, single-use and history-preserving.
	// The running daemon resolves the new exact native entry without touching
	// operator YAML, native auth bytes or the approved Agent/Loop/model.
	other := candidates.Candidates[1].CredentialID
	originalConnection, e := os.ReadFile(s.ConfigFile + ".controller-codex.json")
	if e != nil {
		t.Fatal(e)
	}
	beforeReplacement, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	call("POST", "/v1/controller/codex/preview", app.ControllerCodexPreviewInput{ProfileHome: home, CredentialID: other}, 409)
	replacement := cli("preview", home, "--credential", other, "--replace")
	staleReplacement := cli("preview", home, "--credential", other, "--replace")
	replaced := cli("connect", replacement.ReviewID)
	if replaced.CredentialID != other || replaced.AccountID != "other-account" || replaced.Status != "connected" {
		t.Fatal("exact replacement missing")
	}
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: replacement.ReviewID}, 403)
	call("POST", "/v1/controller/codex/connect", app.ControllerCodexApplyInput{ReviewID: staleReplacement.ReviewID}, 409)
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e != nil {
		t.Fatal("running resolver did not adopt replacement", e)
	}
	retainedConnection, _ := os.ReadFile(s.ConfigFile + ".controller-codex.json")
	afterReplacement, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if !bytes.Equal(originalConnection, retainedConnection) || !bytes.Equal(beforeReplacement, afterReplacement) {
		t.Fatal("replacement overwrote retained connection or native auth")
	}
	afterLoop, e := s.GetLoopAs(ctx, sub, "preserved-loop", 1)
	if e != nil || !reflect.DeepEqual(beforeLoop, afterLoop) {
		t.Fatal("exact published Loop changed", e)
	}
	os.WriteFile(s.ConfigFile, append(append([]byte{}, original...), []byte("# late operator edit\n")...), 0600)
	call("GET", "/v1/controller/codex", nil, 409)
	if _, e = s.Hermes.ResolveProviderAuthentication(ctx, h); e == nil {
		t.Fatal("runtime ignored config binding drift")
	}
	drifted, _ := os.ReadFile(s.ConfigFile)
	if !bytes.Contains(drifted, []byte("# late operator edit")) {
		t.Fatal("operator data lost")
	}
	os.WriteFile(s.ConfigFile, original, 0600)
	if s.Config.Credentials.ControllerCodexLogin != nil {
		t.Fatal("mutable startup Config")
	}
	for _, name := range []string{"graphs", "queue"} {
		// Collections are real authoritative API reads; connection adds no records.
		time.Sleep(240 * time.Millisecond)
		r, _ := http.NewRequest("GET", "http://unix/v1/"+name, nil)
		r.Header.Set("Authorization", "Bearer "+s.Config.API.Token)
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.TrimSpace(string(raw)) != "[]" {
			t.Fatalf("connection changed %s: %s", name, raw)
		}
	}
}
