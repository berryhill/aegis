package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func assertNativeOutcomeRead(t *testing.T, svc *app.Service, subject core.Subject, view app.QueueExecutionView, accepted bool) {
	t.Helper()
	if view.Debugger == nil {
		t.Fatal("missing native debugger")
	}
	initial := consoleQueueRecord(view).Queue
	if initial.NativeOutcome == nil || initial.NativeOutcome.Accepted != accepted {
		t.Fatal("initial native outcome absent or wrong")
	}
	var b bytes.Buffer
	if err := consoleweb.QueueOutcome(initial).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	polled, err := consoleQueueCurrentOutcome(context.Background(), svc, subject, *view.Debugger)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{view.Disposition.ReasonCode, view.Attempts[0].AttemptID, initial.NativeOutcome.PassID, initial.NativeOutcome.HistoryDigest, "trusted offline go-test.v1"} {
		if !strings.Contains(b.String(), text) || !strings.Contains(polled, text) {
			t.Fatalf("initial/poll missing %q", text)
		}
	}
	if accepted {
		for _, text := range []string{"records trusted-check success", view.Disposition.ArtifactIDs[0], view.Disposition.ReceiptIDs[0]} {
			if !strings.Contains(polled, text) {
				t.Fatalf("native evidence missing %q", text)
			}
		}
		original := svc.FleetRepository
		svc.FleetRepository = resultReadRepository{Repository: original, artifact: func(a *evidence.RuntimeArtifact) { a.ContentRef = "sha256:bad" }}
		invalid, invalidErr := svc.ExecutionDebuggerAs(context.Background(), subject, view.Item.ItemID, "")
		svc.FleetRepository = original
		if invalidErr == nil || invalid.NativeEvidenceVerified {
			t.Fatal("corrupt native artifact established acceptance")
		}
		for _, mutate := range []func(*evidence.VerificationReceipt){
			func(r *evidence.VerificationReceipt) { r.EvidenceRef = "sha256:bad" },
			func(r *evidence.VerificationReceipt) { r.AttemptID = "foreign-attempt" },
		} {
			svc.FleetRepository = resultReadRepository{Repository: original, receipt: mutate}
			reloaded, readErr := svc.ExecutionDebuggerAs(context.Background(), subject, view.Item.ItemID, "")
			svc.FleetRepository = original
			if readErr == nil || reloaded.NativeEvidenceVerified {
				t.Fatal("corrupt/foreign native receipt established acceptance")
			}
		}
	} else if initial.NativeOutcome.FailureCode != "" && !strings.Contains(polled, initial.NativeOutcome.FailureCode) {
		t.Fatal("native failure classification missing")
	}
	if strings.Contains(polled, "return 42") || strings.Contains(polled, "private diagnosis failure") || strings.Contains(polled, "verified selected-file evidence") {
		t.Fatal("native outcome content/classification leak")
	}
}
