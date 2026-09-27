package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func doerFormRequest(values url.Values) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/console/loops/doer/preview", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func validDoerForm() url.Values {
	return url.Values{
		"csrf": {"csrf"}, "publisher_id": {"existing-agent"}, "publication_key": {"unique-key"},
		"loop_id": {"doer-task"}, "revision": {"1"}, "previous_digest": {""},
		"task": {"Create result.txt"}, "workspace": {"/home/operator/work"},
		"writable_files": {"result.txt"}, "verify_file": {"result.txt"},
		"assert_text": {"presence"}, "expected_text": {""}, "max_attempts": {"3"},
	}
}

func TestDoerConsoleFormBuildsCanonicalRevisionAndPreservesAssertion(t *testing.T) {
	values := validDoerForm()
	form, err := decodeDoerComposerForm(doerFormRequest(values))
	if err != nil {
		t.Fatal(err)
	}
	if form.Revision.SchemaVersion != loop.DoerRevisionSchemaVersion || form.Revision.Doer == nil || form.Revision.Doer.ExpectedText != nil || form.Revision.Doer.VerifyFile != "result.txt" || form.Revision.Doer.MaxAttempts != 3 {
		t.Fatalf("wrong presence candidate: %+v", form.Revision)
	}
	values.Set("assert_text", "exact")
	form, err = decodeDoerComposerForm(doerFormRequest(values))
	if err != nil || form.Revision.Doer == nil || form.Revision.Doer.ExpectedText == nil || *form.Revision.Doer.ExpectedText != "" {
		t.Fatalf("exact-empty assertion lost: form=%+v err=%v", form, err)
	}
	values.Set("expected_text", "hello")
	form, err = decodeDoerComposerForm(doerFormRequest(values))
	if err != nil || *form.Revision.Doer.ExpectedText != "hello" {
		t.Fatalf("exact text lost: form=%+v err=%v", form, err)
	}
}

func TestDoerConsoleFormRejectsUnsafeAndAmbiguousInputs(t *testing.T) {
	cases := map[string]func(url.Values){
		"escape":                  func(v url.Values) { v.Set("verify_file", "../escape") },
		"relative workspace":      func(v url.Values) { v.Set("workspace", "relative") },
		"verify not writable":     func(v url.Values) { v.Set("writable_files", "other.txt") },
		"unknown field":           func(v url.Values) { v.Set("authority", "forged") },
		"duplicate field":         func(v url.Values) { v.Add("publisher_id", "other") },
		"empty owner":             func(v url.Values) { v.Set("publisher_id", "") },
		"extra text for presence": func(v url.Values) { v.Set("expected_text", "hello") },
		"invalid assertion":       func(v url.Values) { v.Set("assert_text", "guess") },
		"too many attempts":       func(v url.Values) { v.Set("max_attempts", "4") },
		"missing csrf":            func(v url.Values) { v.Del("csrf") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			values := validDoerForm()
			mutate(values)
			if _, err := decodeDoerComposerForm(doerFormRequest(values)); err == nil {
				t.Fatal("accepted invalid Doer form")
			}
		})
	}
}
