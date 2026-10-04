package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
)

// decodeDoerComposerForm accepts only operator-authored definition inputs.
// Authority and canonical revision geometry remain server-owned.
func decodeDoerComposerForm(request *http.Request) (loopComposerForm, error) {
	var form loopComposerForm
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > loopComposerBytesMax {
		return form, errors.New("invalid Doer form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, loopComposerBytesMax+1))
	if err != nil || len(body) > loopComposerBytesMax {
		return form, errors.New("invalid Doer form")
	}
	values, err := url.ParseQuery(string(body))
	keys := []string{"csrf", "publisher_id", "publication_key", "loop_id", "revision", "previous_digest", "task", "workspace", "writable_files", "verify_file", "assert_text", "expected_text", "max_attempts"}
	if len(values) == len(keys)+2 {
		keys = append(keys, "draft_id", "draft_version")
	}
	if err != nil || len(values) != len(keys) {
		return form, errors.New("invalid Doer fields")
	}
	for _, key := range keys {
		if len(values[key]) != 1 {
			return form, errors.New("invalid Doer fields")
		}
	}
	revision, err := strconv.ParseUint(values.Get("revision"), 10, 64)
	if err != nil || revision == 0 {
		return form, errors.New("invalid Doer revision")
	}
	attempts, err := strconv.ParseUint(values.Get("max_attempts"), 10, 16)
	if err != nil || attempts == 0 || attempts > 3 {
		return form, errors.New("invalid Doer attempts")
	}
	files := strings.Split(values.Get("writable_files"), "\n")
	for i := range files {
		files[i] = strings.TrimSuffix(files[i], "\r")
	}
	contract := app.DoerContract{Task: values.Get("task"), Workspace: values.Get("workspace"), WritableFiles: files, VerifyFile: values.Get("verify_file"), MaxAttempts: uint16(attempts)}
	switch values.Get("assert_text") {
	case "presence":
		if values.Get("expected_text") != "" {
			return form, errors.New("presence assertion cannot have expected text")
		}
	case "exact", "bytes":
		text := values.Get("expected_text")
		contract.ExpectedText = &text
		contract.ExactBytes = values.Get("assert_text") == "bytes"
	default:
		return form, errors.New("invalid Doer assertion")
	}
	candidate, validation, err := app.NewDoerLoopRevision(values.Get("loop_id"), revision, values.Get("previous_digest"), contract)
	if err != nil || validation.Outcome != app.LoopValidationValid {
		return form, errors.New("invalid Doer operator inputs")
	}
	form = loopComposerForm{CSRF: values.Get("csrf"), PublisherID: values.Get("publisher_id"), PublicationKey: values.Get("publication_key"), Revision: candidate}
	if len(keys) == 15 {
		form.RetainDraft = true
		form.DraftID = values.Get("draft_id")
		form.DraftVersion, err = strconv.ParseUint(values.Get("draft_version"), 10, 64)
		if err != nil || (form.DraftID == "") != (form.DraftVersion == 0) {
			return loopComposerForm{}, errors.New("invalid Doer draft version")
		}
	}
	if form.CSRF == "" || form.PublisherID == "" || form.PublicationKey == "" {
		return loopComposerForm{}, errors.New("CSRF, publisher and publication key required")
	}
	return form, nil
}
