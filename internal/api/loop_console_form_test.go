package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
)

func validLoopComposerValues() url.Values {
	return url.Values{
		"csrf":                {"csrf-canary"},
		"publisher_id":        {"agent-builder"},
		"publication_key":     {"publish-loop-review-r1"},
		"loop_id":             {"loop.review"},
		"revision":            {"1"},
		"previous_digest":     {""},
		"entry_step_id":       {"start"},
		"inputs":              {"value,string,true"},
		"outputs":             {"result,string,true"},
		"steps":               {"start,action,1,,\ndone,terminal,1,,succeeded"},
		"step_ports":          {"start,input,value,string,true\nstart,output,result,string,true\ndone,input,result,string,true\ndone,output,final,string,true"},
		"terminal_mappings":   {"done,final,result"},
		"evidence_claims":     {""},
		"transitions":         {"complete,start,done,,0"},
		"transition_mappings": {"complete,result,result"},
		"required_evidence":   {""},
	}
}

func composerRequest(values url.Values) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/console/loops/preview", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func TestDecodeLoopComposerBuildsTypedValidRevision(t *testing.T) {
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	if err != nil {
		t.Fatal(err)
	}
	if form.PublisherID != "agent-builder" || form.PublicationKey != "publish-loop-review-r1" || len(form.Revision.Steps) != 2 || len(form.Revision.Transitions) != 1 {
		t.Fatalf("structured form lost typed fields: %+v", form)
	}
	if _, validation, err := app.NewLoopRevision(form.Revision); err != nil || validation.Outcome != app.LoopValidationValid {
		t.Fatalf("structured form did not produce a valid Loop revision: validation=%+v err=%v", validation, err)
	}
}

func TestDecodeLoopComposerRejectsUnknownFieldsAndBrokenReferences(t *testing.T) {
	unknown := validLoopComposerValues()
	unknown.Set("authority_id", "browser-forged")
	if _, err := decodeLoopComposerForm(composerRequest(unknown)); err == nil {
		t.Fatal("unknown browser authority field was accepted")
	}

	broken := validLoopComposerValues()
	broken.Set("terminal_mappings", "missing,final,result")
	if _, err := decodeLoopComposerForm(composerRequest(broken)); err == nil || !strings.Contains(err.Error(), "unknown or non-terminal") {
		t.Fatalf("broken terminal reference was not rejected with field context: %v", err)
	}
}

func TestDecodeLoopLifecycleFormIsClosedAndTyped(t *testing.T) {
	values := url.Values{
		"csrf": {"csrf-canary"}, "target_id": {"loop.review:1"}, "expected_digest": {"sha256:" + strings.Repeat("a", 64)},
		"publisher_id": {"agent-builder"}, "state": {"retired"}, "expected_previous_digest": {""}, "idempotency_key": {"retire-loop-review-r1"},
	}
	form, err := decodeLoopLifecycleForm(composerRequest(values))
	if err != nil || form.State != app.LoopLifecycleRetired {
		t.Fatalf("valid lifecycle form rejected: form=%+v err=%v", form, err)
	}
	values.Set("state", "draft")
	if _, err = decodeLoopLifecycleForm(composerRequest(values)); err == nil {
		t.Fatal("unsupported reverse lifecycle transition was accepted")
	}
}

func TestLoopLifecycleEventIDIsStableAndOpaque(t *testing.T) {
	first := loopLifecycleEventID("activate-loop-review-r1")
	if first != loopLifecycleEventID("activate-loop-review-r1") {
		t.Fatal("same lifecycle idempotency key produced different event IDs")
	}
	if first == loopLifecycleEventID("retire-loop-review-r1") || strings.Contains(first, "activate-loop-review-r1") {
		t.Fatalf("lifecycle event ID did not bind the key opaquely: %q", first)
	}
}

func TestLoopRevisionTargetRoundTripsSlashAndColonIdentifiers(t *testing.T) {
	for _, loopID := range []string{"team/review", "tenant:team/review"} {
		target := loopRevisionTargetID(loopID, 42)
		gotID, gotRevision, err := parseLoopRevisionTargetID(target)
		if err != nil || gotID != loopID || gotRevision != 42 {
			t.Fatalf("target %q round trip: id=%q revision=%d err=%v", target, gotID, gotRevision, err)
		}
	}
}

func TestLoopCommandDefinitionsNormalizeTypedInputAndRejectBrowserAuthority(t *testing.T) {
	definitions := loopCommandDefinitions(&app.Service{})
	if len(definitions) != 2 || definitions[0].ID != loopPublishCommandID || definitions[1].ID != loopLifecycleCommandID {
		t.Fatalf("unexpected closed Loop command catalog: %+v", definitions)
	}
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(loopPublishCommandInput{PublisherID: form.PublisherID, Revision: form.Revision, PublicationKey: form.PublicationKey})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := definitions[0].Normalize(raw)
	if err != nil {
		t.Fatalf("valid publication was not normalized: %v", err)
	}
	var publication loopPublishCommandInput
	if err = json.Unmarshal(normalized, &publication); err != nil || publication.Revision.Digest != "" || publication.Revision.Validator.ID != "" {
		t.Fatalf("normalized publication retained derived fields or was unreadable: publication=%+v err=%v", publication, err)
	}
	forged := append(raw[:len(raw)-1], []byte(`,"authority_id":"browser-forged"}`)...)
	if _, err = definitions[0].Normalize(forged); !errors.Is(err, console.ErrInvalidInput) {
		t.Fatalf("browser authority field was not denied: %v", err)
	}
	invalidLifecycle := json.RawMessage(`{"publisher_id":"agent-builder","state":"draft","idempotency_key":"reverse"}`)
	if _, err = definitions[1].Normalize(invalidLifecycle); !errors.Is(err, console.ErrInvalidInput) {
		t.Fatalf("unsupported lifecycle state was not denied: %v", err)
	}
}

func TestDecodeLoopComposerRejectsOversizedAndDuplicateInput(t *testing.T) {
	oversized := validLoopComposerValues()
	oversized.Set("inputs", strings.Repeat("x", loopComposerBytesMax))
	if _, err := decodeLoopComposerForm(composerRequest(oversized)); err == nil {
		t.Fatal("oversized Loop composer input was accepted")
	}
	duplicate := validLoopComposerValues()
	duplicate["publisher_id"] = []string{"agent-builder", "agent-forged"}
	if _, err := decodeLoopComposerForm(composerRequest(duplicate)); err == nil {
		t.Fatal("ambiguous publisher input was accepted")
	}
}
