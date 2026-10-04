package api

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func consoleDoerDraft(d app.DoerDraft) consoleweb.DoerDraftModel {
	model := consoleweb.DoerDraftModel{ID: d.ID, Version: strconv.FormatUint(d.Version, 10), PublisherID: d.Agent.ID, LoopID: d.LoopID, Revision: strconv.FormatUint(d.Revision, 10), PreviousDigest: d.PreviousDigest, PublicationKey: d.PublicationKey, Task: d.Contract.Task, Workspace: d.Contract.Workspace, WritableFiles: strings.Join(d.Contract.WritableFiles, "\n"), VerifyFile: d.Contract.VerifyFile, Assertion: "presence", MaxAttempts: strconv.FormatUint(uint64(d.Contract.MaxAttempts), 10)}
	if d.Contract.ExpectedText != nil {
		model.Assertion = "exact"
		model.ExpectedText = *d.Contract.ExpectedText
		if d.Contract.ExactBytes {
			model.Assertion = "bytes"
		}
	}
	return model
}

func consoleDoerReadiness(model *consoleweb.DoerReviewModel, readiness app.DoerCandidateReadiness, draftID string) {
	model.CanAuthor, model.CanExecute, model.ReadinessReason = readiness.CanAuthor, readiness.CanExecute, readiness.Reason
	if draftID != "" {
		model.DraftURL = "/console/loops/doer?draft_id=" + url.QueryEscape(draftID)
		model.SetupURL = "/console/loops/doer/setup?draft_id=" + url.QueryEscape(draftID)
	}
	for _, check := range []struct {
		name   string
		status app.DoerPrerequisiteStatus
	}{
		{"Authenticated requester", readiness.Subject}, {"Exact enabled Agent", readiness.AgentReference}, {"Canonical candidate", readiness.CanonicalCandidate}, {"Exact charter", readiness.Charter}, {"Authenticated stanza", readiness.Selection}, {"Usable configured model", readiness.Model}, {"Tool-free scope", readiness.ToolFree}, {"Credential-free scope", readiness.CredentialFree}, {"Provisioning receipt", readiness.Receipt}, {"Supported Hermes runtime", readiness.Runtime}, {"Exact host-write contract and local controller", readiness.Controller}, {"Local Laya helper", readiness.Helper},
	} {
		value := check.status.State
		if check.status.Reason != "" {
			value += " · " + check.status.Reason
		}
		model.Prerequisites = append(model.Prerequisites, consoleweb.FieldModel{Label: check.name, Value: value})
	}
}
