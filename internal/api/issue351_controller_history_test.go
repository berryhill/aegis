package api

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

// Completed-result inspection is not permission to execute again. All state
// transitions below affect only this isolated real-store/synthetic-runtime case.
func testIssue351ControllerHistoricalResult(t *testing.T, s *app.Service, input app.QueueLoopInput, contract loop.DoerContract, item string, clock *requeueStageClock, mode string) {
	t.Helper()
	ctx := context.Background()
	if clock == nil {
		t.Fatal("historical fixture requires one shared stage clock")
	}
	reader, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.GetQueueResultAs(ctx, reader, item)
	if err != nil || original.Result == nil || original.Verification != "verified" {
		t.Fatal("initial exact result unavailable", err)
	}
	before, err := s.GetQueueItemAs(ctx, reader, item)
	if err != nil || before.RuntimeAuthorityBinding == nil {
		t.Fatal("exact runtime binding unavailable", err)
	}
	binding := before.RuntimeAuthorityBinding
	authority, err := s.Authority.GetAuthorityContext(ctx, binding.Authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if mode == "authority-expiry" || mode == "host-expiry" {
		// The task is complete; stop this fixture's otherwise-idle owned runtime
		// while its original scope is still live, before testing later read-only
		// expiry. Do not require shutdown to reuse an already expired subject.
		reader, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.TerminateSessionAs(ctx, reader, authority.SessionID, "completed historical-read fixture"); err != nil {
			t.Fatal(err)
		}
	}
	switch mode {
	case "authority-expiry":
		at := authority.ExpiresAt.Add(time.Second)
		if delta := at.Sub(clock.Now()); delta > 0 {
			clock.Advance(delta)
		}
	case "host-expiry":
		grant, err := s.ReadDoerHostWriteApprovalAs(ctx, reader, input.Agent, contract)
		if err != nil {
			t.Fatal(err)
		}
		if delta := grant.ExpiresAt.Add(time.Second).Sub(clock.Now()); delta > 0 {
			clock.Advance(delta)
		}
	case "revoked":
		reader, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RevokeSessionAs(ctx, reader, authority.SessionID, "isolated historical-read regression"); err != nil {
			t.Fatal(err)
		}
	case "disabled", "retired":
		reader, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
		if err != nil {
			t.Fatal(err)
		}
		mutation, err := app.NewSetAgentLifecycleInput(input.Agent.ID, input.Agent.Revision, input.Agent.Digest, mode)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetAgentLifecycleAs(ctx, reader, input.Agent.ID, mutation); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("unknown historical read case")
	}
	// Fresh reader authentication does not renew the original runtime/host grant.
	reader, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	historical, err := s.GetQueueResultAs(ctx, reader, item)
	if err != nil || !reflect.DeepEqual(historical, original) {
		t.Fatalf("completed immutable result lost after %s: %v", mode, err)
	}
	after, err := s.GetQueueItemAs(ctx, reader, item)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("historical read changed Queue/attempt/evidence facts", err)
	}
	if mode == "authority-expiry" || mode == "host-expiry" || mode == "revoked" {
		live, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, clock.Now())
		if err != nil || live.Admitted {
			t.Fatal("historical read renewed execution authority", err)
		}
	}
	if mode == "host-expiry" {
		if _, err := s.ReadDoerHostWriteApprovalAs(ctx, reader, input.Agent, contract); err == nil {
			t.Fatal("historical read renewed expired host scope")
		}
	}
}
