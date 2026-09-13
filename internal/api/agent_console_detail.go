package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// consoleAgentDetail is an authenticated, read-only evidence join. It never
// previews a session (which would issue a mandate), provisions, or admits work.
// No browser field is accepted as a subject, stanza, or environment selector.
func consoleAgentDetail(ctx context.Context, svc *app.Service, subject core.Subject, selected, latest app.FleetAgent, surface app.FleetSurface) (consoleweb.RecordModel, error) {
	if err := svc.RequirePrincipal(subject); err != nil {
		return consoleweb.RecordModel{}, err
	}
	record := consoleAgentRecord(selected, surface)
	detail := record.Agent
	revision := selected.Revision
	detail.Historical = revision.Digest != latest.Revision.Digest || revision.Revision != latest.Revision.Revision
	detail.LifecycleEligible = !detail.Historical && revision.Lifecycle != "retired" && revision.AgentID != "aegis"
	detail.EvaluatedFor = subject.ID + " · " + subject.Issuer + " · " + subject.Method + " · local"
	detail.AuthorityState = "unavailable"
	detail.EffectiveAuthority = "Unavailable: exact charter evidence could not be verified. No authority is asserted."
	detail.CharterEvidence = "Unavailable: exact charter evidence could not be verified."
	detail.ProvisioningEvidence = "Unavailable: provisioning evidence could not be read."
	detail.HistoryEvidence = "Unavailable: revision history could not be read."
	detail.SessionEvidence = "Unavailable: session records could not be read."

	if history, err := svc.ListFleetAgentRevisionsAs(ctx, subject, revision.AgentID); err == nil {
		detail.HistoryEvidence = "Immutable Registry revisions with exact charter bindings; selecting history does not activate it."
		for _, value := range history {
			detail.History = append(detail.History, consoleweb.LinkModel{Label: fmt.Sprintf("Registry r%d · %s", value.Revision, value.Lifecycle), Detail: fmt.Sprintf("%s · charter %s r%d @ %s", value.Digest, value.Charter.ID, value.Charter.Revision, value.Charter.Digest), URL: consoleAgentRevisionURL(value.AgentID, value.Revision)})
		}
	}
	detail.CharterHistoryEvidence = "Unavailable: charter revision history could not be read."
	if history, err := svc.ListCharters(revision.Charter.ID); err == nil {
		detail.CharterHistoryEvidence = "Verified immutable charter revisions. These are distinct from Registry lifecycle revisions; publication is not activation."
		for _, value := range history {
			detail.CharterHistory = append(detail.CharterHistory, consoleweb.FieldModel{Label: fmt.Sprintf("%s r%d", value.Charter.AgentID, value.Charter.Revision), Value: value.Digest})
		}
	}
	charter, charterErr := svc.GetCharter(revision.Charter.ID, revision.Charter.Revision)
	if charterErr == nil && charter.Digest == revision.Charter.Digest && charter.Charter.AgentID == revision.Charter.ID && charter.Charter.Revision == revision.Charter.Revision && charter.Charter.Runtime.Adapter == revision.Runtime.Adapter && charter.Charter.Runtime.Runtime == revision.Runtime.Runtime && charter.Charter.Runtime.Target == revision.Runtime.Target {
		detail.CharterEvidence = "Exact stored charter revision and digest verified. Declarations are not effective authority."
		for _, stanza := range charter.Charter.Stanzas {
			detail.Stanzas = append(detail.Stanzas, consoleweb.AgentStanzaModel{ID: stanza.ID, Name: stanza.Name, Enabled: stanza.Enabled, Fields: []consoleweb.FieldModel{
				{Label: "Authentication and identity selectors", Value: agentPolicyJSON(stanza.Authentication)},
				{Label: "Capabilities", Value: agentPolicyList(stanza.Grant.Capabilities)}, {Label: "Tools", Value: agentPolicyList(stanza.Grant.Tools)},
				{Label: "Memory scopes", Value: agentPolicyList(stanza.Scopes.Memory)}, {Label: "Credential scopes (references only)", Value: agentPolicyList(stanza.Scopes.Credentials)},
				{Label: "Session policy", Value: agentPolicyJSON(stanza.Session)}, {Label: "Approval policy", Value: agentPolicyJSON(stanza.Approval)},
				{Label: "Information flow", Value: agentPolicyJSON(stanza.InformationFlow)}, {Label: "Hermes runtime declaration", Value: agentPolicyJSON(stanza.Hermes)},
			}})
		}
		digest, authority, decision, err := svc.EffectiveAuthorityAs(subject, revision.Charter.ID, revision.Charter.Revision, "", core.Environment{Name: "local"})
		if digest == revision.Charter.Digest && err == nil && decision.Allowed && decision.Selected != nil && decision.MatchingCount == 1 {
			detail.AuthorityState = "matched"
			detail.EffectiveAuthority = "Exactly one stanza matches the authenticated operator. This read-only evaluation issues no mandate and grants no runtime admission."
			detail.AuthorityFields = []consoleweb.FieldModel{
				{Label: "Selected stanza", Value: authority.StanzaID}, {Label: "Authentication", Value: agentPolicyJSON(decision.Selected.Authentication)},
				{Label: "Capabilities", Value: agentPolicyList(authority.Capabilities)}, {Label: "Tools", Value: agentPolicyList(authority.Tools)},
				{Label: "Memory scopes", Value: agentPolicyList(authority.Memory)}, {Label: "Credential scopes (references only)", Value: agentPolicyList(authority.Credentials)},
				{Label: "Session lifetime and delegation", Value: agentPolicyJSON(authority.Session)}, {Label: "Approvals", Value: agentPolicyJSON(authority.Approval)},
			}
		} else if digest == revision.Charter.Digest {
			detail.AuthorityState = "denied"
			reason := decision.Reason
			if decision.Allowed || reason == "" {
				reason = "effective_tool_validation_failed"
			}
			detail.EffectiveAuthority = "Denied: " + reason + ". No effective permissions are asserted."
		}
	}
	if receipts, err := svc.ListReceipts(); err == nil {
		detail.ProvisioningEvidence = "No provisioning receipt recorded for this exact charter digest."
		for _, receipt := range receipts {
			if receipt.CharterDigest != revision.Charter.Digest {
				continue
			}
			detail.ProvisioningEvidence = "Provisioning receipt recorded for the exact charter digest. Historical evidence is not current readiness or runtime admission."
			detail.ReceiptFields = append(detail.ReceiptFields, consoleweb.FieldModel{Label: receipt.ID, Value: receipt.Status + " · plan " + receipt.PlanID + " @ " + receipt.PlanDigest + " · charter " + receipt.CharterDigest})
		}
	}
	if sessions, err := svc.ListSessions(); err == nil {
		detail.SessionEvidence = "No session record for this exact charter and runtime binding. Session history does not establish current admission; no session-detail browser route is available."
		for _, session := range sessions {
			mandate := session.Mandate
			if mandate.AgentID != revision.Charter.ID || mandate.CharterRevision != revision.Charter.Revision || mandate.CharterDigest != revision.Charter.Digest || mandate.Target != revision.Runtime.Target || mandate.Runtime.Runtime != revision.Runtime.Runtime {
				continue
			}
			detail.SessionEvidence = "Recorded sessions for the exact charter, runtime and target binding. Registry revision ownership is not encoded in these session records; they are not proof of current admission, and no session-detail browser route is available."
			detail.SessionFields = append(detail.SessionFields, consoleweb.FieldModel{Label: session.ID, Value: session.Status + " · stanza " + mandate.StanzaID + " · mandate " + mandate.ID + " · recorded session, not fresh admission"})
		}
	}
	for _, accepted := range surface.Submissions.Accepted {
		for _, participant := range accepted.Snapshot.Participants {
			if participant.ID == revision.AgentID && participant.Revision == revision.Revision && participant.Digest == revision.Digest {
				for _, queued := range surface.Queue {
					if queued.Item.ItemID == accepted.QueueItem.ItemID && queued.Item.Snapshot.ID == accepted.Snapshot.SnapshotID && queued.Item.Snapshot.Digest == accepted.Snapshot.Digest {
						related := consoleQueueRecord(queued)
						detail.Executions = append(detail.Executions, consoleweb.LinkModel{Label: "Execution · " + related.Label, Detail: related.Lifecycle, URL: consoleRecordURL(consoleQueue, related.Key)})
					}
				}
				break
			}
		}
	}
	record.Readiness = "Current readiness unavailable: runtime compatibility, provisioning state and fresh execution admission are not established by this read."
	if detail.AuthorityState == "denied" {
		record.Readiness = "Blocked: authenticated operator authority denied. Fresh execution admission remains required."
	}
	if detail.Historical {
		record.Readiness = "Historical revision: not eligible for lifecycle changes or current execution through this detail."
	}
	if revision.Lifecycle == "disabled" {
		record.Readiness = "Disabled: execution denied until a new enabled revision."
	}
	if revision.Lifecycle == "retired" {
		record.Readiness = "Retired: terminal Registry lifecycle; execution denied."
	}
	record.Authority = detail.EffectiveAuthority
	record.Provisioning = detail.ProvisioningEvidence
	return record, nil
}

func preserveAgentHistoryContext(detail *consoleweb.AgentDetailModel, collectionURL string) {
	base, err := url.Parse(collectionURL)
	if err != nil || detail == nil {
		return
	}
	for index := range detail.History {
		target, err := url.Parse(detail.History[index].URL)
		if err != nil {
			continue
		}
		query := target.Query()
		for _, key := range []string{"q", "lifecycle", "page", "limit"} {
			if value := base.Query().Get(key); value != "" {
				query.Set(key, value)
			}
		}
		target.RawQuery = query.Encode()
		detail.History[index].URL = target.String()
	}
}

func agentPolicyJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "Unavailable"
	}
	return string(data)
}
func agentPolicyList(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	return strings.Join(values, ", ")
}
