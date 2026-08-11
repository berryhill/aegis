package consoleweb

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecordLabelEscapesAdversarialFleetText(t *testing.T) {
	var output bytes.Buffer
	value := `</span><script>globalThis.pwned=1</script>`
	if err := RecordLabel(value).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "<script>") || !strings.Contains(output.String(), "&lt;script") {
		t.Fatalf("untrusted record was not escaped: %s", output.String())
	}
}

func TestDocumentHasAccessibleServerRenderedStatesAndNoInlineScript(t *testing.T) {
	var output bytes.Buffer
	if err := Document(PageModel{}).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{"<nav", "<main", "aria-live", `id="authentication-status"`, `data-state="loading"`, `data-state="empty"`, `data-state="unavailable"`, `data-state="error"`, `datastar-v1.0.2.js`} {
		if !strings.Contains(html, required) {
			t.Fatalf("document missing %q", required)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "localStorage") || strings.Contains(html, "sessionStorage") {
		t.Fatal("document contains inline executable or browser storage")
	}
}

func TestConsoleSourceForbidsUnsafeActiveContent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	directory := filepath.Dir(file)
	for _, name := range []string{"components.templ", "model.go"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{"templ.Raw", "SafeURL", "SafeCSS", "ExecuteScript", "innerHTML", "outerHTML", "document.write", "eval(", "new Function", "localStorage", "sessionStorage", "<script>"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s contains prohibited active-content primitive %q", name, forbidden)
			}
		}
	}
}
