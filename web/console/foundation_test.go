package consoleweb

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func renderFoundation(t *testing.T, component interface {
	Render(context.Context, io.Writer) error
}) string {
	t.Helper()
	var output bytes.Buffer
	if err := component.Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestFormFieldAssociatesHelpErrorAndDropsSecretValue(t *testing.T) {
	html := renderFoundation(t, FormField(FormFieldModel{ID: "mandate", Name: "mandate", Label: "Mandate", Help: "Exact reference", Error: "Reference expired", Required: true, Secret: true, Value: "must-not-render", Autocomplete: "off"}))
	for _, required := range []string{`for="mandate"`, `id="mandate-help"`, `id="mandate-error"`, `aria-describedby="mandate-help mandate-error"`, `aria-invalid="true"`, `type="password"`, `value=""`} {
		if !strings.Contains(html, required) {
			t.Fatalf("field missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, "must-not-render") {
		t.Fatal("secret-bearing value was retained")
	}
}

func TestReferenceAuthorityReceiptAndStateRemainTypedDisplayOnly(t *testing.T) {
	parts := []string{
		renderFoundation(t, ExactReference(ExactReferenceModel{Label: "Exact Loop", ID: "loop-1", Revision: "r7", Digest: "sha256:abc", Lifecycle: "active", Provenance: "fleet-a"})),
		renderFoundation(t, AuthorityContext(AuthorityContextModel{Identity: "agent-1", Stanza: "review", Mandate: "mandate-1", State: "denied", ReasonCode: "mandate_expired"})),
		renderFoundation(t, OperationReceipt(OperationReceiptModel{Title: "Submission", Outcome: "partial", OperationID: "operation-1", RecordedAt: "2026-08-21T00:00:00Z", ReasonCode: "child_failed", Message: "Authoritative readback"})),
		renderFoundation(t, CollectionState(StateUnavailable, "Unavailable", "No count is asserted.", "store_unavailable")),
	}
	html := strings.Join(parts, "")
	for _, required := range []string{"Reference", "Revision", "Digest", "Lifecycle", "Provenance", "Display only · not an authority selector", `data-authority-state="denied"`, `data-outcome="partial"`, `data-state="unavailable"`, "No count is asserted."} {
		if !strings.Contains(html, required) {
			t.Fatalf("typed visual contract missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, "<select") || strings.Contains(html, "<input") {
		t.Fatal("display-only authority/reference contract became an input")
	}
}

func TestDialogDrawerAndErrorSummaryExposeAccessibilityContracts(t *testing.T) {
	dialog := renderFoundation(t, Dialog(OverlayModel{ID: "review", Title: "Review exact reference", Description: "Unresolved operation"}))
	drawer := renderFoundation(t, Drawer(OverlayModel{ID: "record", Title: "Record detail"}))
	summary := renderFoundation(t, ErrorSummary("Correct these errors", []string{"Mandate expired"}))
	html := dialog + drawer + summary
	for _, required := range []string{`<dialog id="review"`, `aria-labelledby="review-title"`, `aria-describedby="review-description"`, `data-overlay-kind="dialog"`, `data-overlay-kind="drawer"`, `commandfor="review"`, `command="close"`, `role="alert"`, `tabindex="-1"`, `data-error-summary`} {
		if !strings.Contains(html, required) {
			t.Fatalf("interaction contract missing %q: %s", required, html)
		}
	}
	for _, titleID := range []string{"review-title", "record-title"} {
		contract := `id="` + titleID + `" tabindex="0" autofocus`
		if !strings.Contains(html, contract) {
			t.Fatalf("overlay title must be a deterministic native Tab stop for forward and reverse containment %q: %s", contract, html)
		}
	}
	for _, forbidden := range []string{`aria-modal="true"`, ` open`, ` hidden`} {
		if strings.Contains(dialog, forbidden) {
			t.Fatalf("closed native dialog asserted unearned modal state %q: %s", forbidden, dialog)
		}
	}
}

func TestHostileTextIsEscapedAcrossFoundation(t *testing.T) {
	hostile := `</section><script>globalThis.pwned=1</script>`
	html := renderFoundation(t, Notice(NoticeModel{Kind: "error", Title: hostile, Message: hostile, ReasonCode: hostile}))
	if strings.Contains(html, "<script>") || strings.Count(html, "&lt;script") != 3 {
		t.Fatalf("hostile text was not escaped: %s", html)
	}
}

func TestPaginationSanitizesUntrustedURLs(t *testing.T) {
	html := renderFoundation(t, Pagination(PaginationModel{
		Label: "Records", PreviousURL: "javascript:alert(1)", NextURL: "/console/agents?page=2",
		Summary: "Page 1", HasPrevious: true, HasNext: true,
	}))
	if strings.Contains(html, "javascript:") || !strings.Contains(html, `href="about:invalid#TemplFailedSanitizationURL"`) {
		t.Fatalf("pagination did not deny an unsafe URL: %s", html)
	}
	if !strings.Contains(html, `href="/console/agents?page=2"`) {
		t.Fatalf("pagination dropped a safe relative URL: %s", html)
	}
}

func TestFixtureCoversEveryVisualStateWithoutClaimingSuccess(t *testing.T) {
	html := renderFoundation(t, FoundationFixture("390x844"))
	for _, state := range []VisualState{StateLoading, StateEmpty, StateFilteredEmpty, StateDenied, StateUnavailable, StateDegraded, StateError} {
		contract := `data-state="` + string(state) + `"`
		if !strings.Contains(html, contract) {
			t.Fatalf("fixture missing visual-state contract %q", contract)
		}
	}
	for _, required := range []string{`data-viewport="390x844"`, `aria-busy="true"`, `data-authority-state="not-evaluated"`, `data-outcome="pending"`, `read_denied`, `store_unavailable`, `repair_required`, `invalid_projection`} {
		if !strings.Contains(html, required) {
			t.Fatalf("fixture missing bounded-state evidence %q", required)
		}
	}
	for _, forbidden := range []string{`data-outcome="success"`, `data-authority-state="authorized"`, `<script`, `data-on:`, `data-bind:`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("fixture asserted forbidden client or authority state %q", forbidden)
		}
	}
}

func TestFilterNoticeConfirmationAndSkeletonRemainDeclarative(t *testing.T) {
	parts := []string{
		renderFoundation(t, FilterBar("/console/queue", []FilterModel{{ID: "state", Label: "State", Name: "state", Value: "denied", Options: []FilterOptionModel{{Value: "", Label: "All"}, {Value: "denied", Label: "Denied"}}}}, FormFieldModel{ID: "query", Name: "query", Label: "Query"})),
		renderFoundation(t, Notice(NoticeModel{Kind: "denied", Title: "Denied", Message: "No count asserted", ReasonCode: "read_denied"})),
		renderFoundation(t, Confirmation(ConfirmationModel{Title: "Revoke", Message: "Exact operation", ConfirmLabel: "Revoke", CancelLabel: "Cancel", DialogID: "review", Dangerous: true})),
		renderFoundation(t, SafeSkeleton("Loading records", 99)),
	}
	html := strings.Join(parts, "")
	for _, required := range []string{`method="get"`, `action="/console/queue"`, `selected`, `role="alert"`, `commandfor="review"`, `command="close"`, `type="submit"`, `danger-button`, `aria-busy="true"`} {
		if !strings.Contains(html, required) {
			t.Fatalf("declarative component contract missing %q: %s", required, html)
		}
	}
	if strings.Count(html, `<span aria-hidden="true"></span>`) != 8 {
		t.Fatalf("safe skeleton did not enforce its eight-row bound: %s", html)
	}
}
