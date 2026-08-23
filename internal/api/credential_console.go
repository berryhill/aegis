package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
)

const (
	credentialReviewPurpose = "credential-operation"
	maxCredentialFormBytes  = 64 << 10
)

type credentialOperationForm struct {
	Operation     string   `json:"operation"`
	RecordID      string   `json:"record_id,omitempty"`
	Reference     string   `json:"reference,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Value         []byte   `json:"value,omitempty"`
	Version       uint64   `json:"version,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	AgentID       string   `json:"agent_id,omitempty"`
	StanzaID      string   `json:"stanza_id,omitempty"`
	DeploymentID  string   `json:"deployment_id,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Destinations  []string `json:"destinations,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	VersionPolicy string   `json:"version_policy,omitempty"`
	PinnedVersion uint64   `json:"pinned_version,omitempty"`
	CSRF          string   `json:"-"`
}

func decodeCredentialOperationForm(request *http.Request) (credentialOperationForm, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > maxCredentialFormBytes {
		return credentialOperationForm{}, errors.New("invalid credential operation form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxCredentialFormBytes+1))
	if err != nil || len(body) > maxCredentialFormBytes {
		wipeBytes(body)
		return credentialOperationForm{}, errors.New("invalid credential operation form")
	}
	defer wipeBytes(body)
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return credentialOperationForm{}, errors.New("invalid credential operation form")
	}
	allowed := map[string]bool{"csrf": true, "operation": true, "record_id": true, "reference": true, "kind": true, "value": true, "version": true, "reason": true, "agent_id": true, "stanza_id": true, "deployment_id": true, "scope": true, "destinations": true, "mode": true, "version_policy": true, "pinned_version": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return credentialOperationForm{}, errors.New("invalid credential operation form")
		}
	}
	form := credentialOperationForm{
		CSRF: values.Get("csrf"), Operation: values.Get("operation"), RecordID: values.Get("record_id"),
		Reference: values.Get("reference"), Kind: values.Get("kind"), Value: []byte(values.Get("value")),
		Reason: values.Get("reason"), AgentID: values.Get("agent_id"), StanzaID: values.Get("stanza_id"),
		DeploymentID: values.Get("deployment_id"), Scope: values.Get("scope"), Mode: values.Get("mode"),
		VersionPolicy: values.Get("version_policy"),
	}
	if raw := values.Get("version"); raw != "" {
		form.Version, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || form.Version == 0 {
			return credentialOperationForm{}, errors.New("invalid credential version")
		}
	}
	if raw := values.Get("pinned_version"); raw != "" {
		form.PinnedVersion, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || form.PinnedVersion == 0 {
			return credentialOperationForm{}, errors.New("invalid pinned version")
		}
	}
	for _, destination := range strings.Split(values.Get("destinations"), ",") {
		if value := strings.TrimSpace(destination); value != "" {
			form.Destinations = append(form.Destinations, value)
		}
	}
	if err := validateCredentialOperation(form); err != nil {
		wipeBytes(form.Value)
		return credentialOperationForm{}, err
	}
	return form, nil
}

func validateCredentialOperation(form credentialOperationForm) error {
	if form.CSRF == "" {
		return errors.New("credential CSRF proof is required")
	}
	switch form.Operation {
	case "create":
		if form.Reference == "" || form.Kind == "" || len(form.Value) == 0 {
			return errors.New("create requires reference, kind, and value")
		}
	case "rotate":
		if form.RecordID == "" || len(form.Value) == 0 {
			return errors.New("rotate requires exact record and value")
		}
	case "revoke":
		if form.RecordID == "" || form.Version == 0 || strings.TrimSpace(form.Reason) == "" {
			return errors.New("revoke requires exact record, version, and reason")
		}
	case "bind":
		if form.RecordID == "" || form.AgentID == "" || form.StanzaID == "" || form.DeploymentID == "" || form.Scope == "" || len(form.Destinations) == 0 || form.Mode == "" || form.VersionPolicy == "" {
			return errors.New("binding requires an exact complete tuple")
		}
	case "backup":
	default:
		return errors.New("credential operation is not browser-enabled")
	}
	return nil
}

func credentialOperationModel(form credentialOperationForm, stage string) *consoleweb.CredentialOperationModel {
	return &consoleweb.CredentialOperationModel{
		Stage: stage, Operation: form.Operation, RecordID: form.RecordID, Reference: form.Reference, Kind: form.Kind,
		Version: uintString(form.Version), Reason: form.Reason, AgentID: form.AgentID, StanzaID: form.StanzaID,
		DeploymentID: form.DeploymentID, Scope: form.Scope, Destinations: strings.Join(form.Destinations, ", "),
		Mode: form.Mode, VersionPolicy: form.VersionPolicy, PinnedVersion: uintString(form.PinnedVersion),
	}
}

func uintString(value uint64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(value, 10)
}
func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func verifyCredentialTarget(ctxSubject core.Subject, service *app.Service, request *http.Request, form credentialOperationForm) error {
	if form.Operation == "create" || form.Operation == "backup" {
		return nil
	}
	record, err := service.CredentialAs(request.Context(), ctxSubject, form.RecordID)
	if err != nil {
		return err
	}
	if record.Status != "active" {
		return errors.New("credential target is revoked")
	}
	if form.Operation == "revoke" && record.CurrentVersion != form.Version {
		return errors.New("credential version conflict")
	}
	return nil
}

func executeCredentialOperation(service *app.Service, request *http.Request, subject core.Subject, form credentialOperationForm) (string, string, error) {
	switch form.Operation {
	case "create":
		view, err := service.CreateCredentialAs(request.Context(), subject, app.CreateCredentialInput{Reference: form.Reference, Kind: form.Kind, Value: form.Value})
		return view.ID, "created metadata-only credential record", err
	case "rotate":
		view, err := service.RotateCredentialAs(request.Context(), subject, form.RecordID, app.RotateCredentialInput{Value: form.Value})
		return view.ID + ":v" + strconv.FormatUint(view.CurrentVersion, 10), "rotated to immutable version", err
	case "revoke":
		view, err := service.RevokeCredentialAs(request.Context(), subject, form.RecordID, app.RevokeCredentialInput{Version: form.Version, Reason: form.Reason})
		return view.RecordID + ":v" + strconv.FormatUint(view.Version, 10), "revoked exact version", err
	case "bind":
		view, err := service.BindCredentialAs(request.Context(), subject, form.RecordID, app.BindCredentialInput{AgentID: form.AgentID, StanzaID: form.StanzaID, DeploymentID: form.DeploymentID, Scope: form.Scope, Destinations: form.Destinations, Mode: form.Mode, VersionPolicy: form.VersionPolicy, PinnedVersion: form.PinnedVersion, Enabled: true})
		return view.RecordID + ":" + view.AgentID + ":" + view.StanzaID + ":" + view.DeploymentID + ":" + view.Scope, "created exact binding tuple", err
	case "backup":
		_, err := service.BackupCredentialsAs(request.Context(), subject)
		return "configured_ciphertext_backup", "created policy-constrained ciphertext backup", err
	default:
		return "", "", errors.New("credential operation denied")
	}
}

func credentialReceipt(operation, id, message string) *consoleweb.OperationReceiptModel {
	return &consoleweb.OperationReceiptModel{Title: "Credential operation receipt", Outcome: "succeeded", OperationID: operation + ":" + id, RecordedAt: time.Now().UTC().Format(time.RFC3339), ReasonCode: "authoritative_metadata_readback", Message: message + "; no secret value was returned"}
}

func encodeCredentialReview(form credentialOperationForm) ([]byte, error) { return json.Marshal(form) }
func decodeCredentialReview(payload []byte) (credentialOperationForm, error) {
	var form credentialOperationForm
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoderErr := decoder.Decode(&form)
	if decoderErr == nil {
		decoderErr = decoder.Decode(&struct{}{})
		if errors.Is(decoderErr, io.EOF) {
			decoderErr = nil
		}
	}
	if decoderErr != nil || validateCredentialOperation(credentialOperationForm{Operation: form.Operation, RecordID: form.RecordID, Reference: form.Reference, Kind: form.Kind, Value: form.Value, Version: form.Version, Reason: form.Reason, AgentID: form.AgentID, StanzaID: form.StanzaID, DeploymentID: form.DeploymentID, Scope: form.Scope, Destinations: form.Destinations, Mode: form.Mode, VersionPolicy: form.VersionPolicy, PinnedVersion: form.PinnedVersion, CSRF: "receipt"}) != nil {
		wipeBytes(form.Value)
		return credentialOperationForm{}, errors.New("credential review receipt denied")
	}
	return form, nil
}
