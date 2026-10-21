package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
	"testing"
)

func TestDoerPrerequisiteClosedProjection(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("secret=/private/token"), "doer_prerequisite_unavailable"},
		{ErrDoerUnsafeWorkspace, "doer_unsafe_workspace"},
		{context.Canceled, "doer_check_cancelled"},
		{ErrDoerHostApprovalRequired, "doer_host_approval_required"},
		{errors.Join(ErrDoerHostApprovalRequired, hostapproval.ErrAbsent), "doer_host_approval_absent"},
		{hostapproval.ErrExpired, "doer_host_approval_expired"},
		{hostapproval.ErrDenied, "doer_host_approval_invalid"},
		{ErrDoerGoIntegrity, "doer_go_executable_untrusted"},
		{ErrDoerLayaIntegrity, "doer_laya_executable_untrusted"},
		{ErrDoerHomeIntegrity, "doer_laya_home_untrusted"},
	} {
		if got := DoerPrerequisiteReason(tc.err); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
func TestDoerReadinessMissingControllerIsNotApproval(t *testing.T) {
	var w *QueueWorker
	if got := DoerPrerequisiteReason(w.ValidateDoerContractReadiness(context.Background(), loop.DoerContract{}, registry.AgentRevision{}, false)); got != "doer_controller_unavailable" {
		t.Fatal(got)
	}
}
func TestDoerReadinessCancellationIsNotApproval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &QueueWorker{implementation: &ImplementationController{}}
	if got := DoerPrerequisiteReason(w.ValidateDoerContractReadiness(ctx, loop.DoerContract{}, registry.AgentRevision{}, false)); got != "doer_check_cancelled" {
		t.Fatal(got)
	}
}
