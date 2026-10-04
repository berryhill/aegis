package api

import (
	"context"
	"strings"
	"testing"

	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestDoerPublicationReceiptOffersExplicitInlineRun(t *testing.T) {
	receipt := consoleweb.OperationReceiptModel{Title: "Doer publication", Outcome: "ok", PublishedDoerRun: &consoleweb.PublishedDoerRunModel{URL: "/console/loops/run?loop_id=exact-doer", CSRF: "csrf-value", Key: "retained-key", Digest: "exact-digest", Revision: 2}}
	html, err := renderConsole(context.Background(), consoleweb.CommandReceiptPage(receipt))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`action="/console/loops/run?loop_id=exact-doer"`, `name="csrf" value="csrf-value"`, `name="idempotency_key" value="retained-key"`, `name="digest" value="exact-digest"`, `name="revision" value="2"`, `Run published Loop`, `Publication is not execution`} {
		if !strings.Contains(string(html), want) {
			t.Errorf("missing %q", want)
		}
	}
	receipt.PublishedDoerRun = nil
	html, err = renderConsole(context.Background(), consoleweb.CommandReceiptPage(receipt))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), `Run published Loop`) {
		t.Fatal("unbound receipt offered Run")
	}
}
