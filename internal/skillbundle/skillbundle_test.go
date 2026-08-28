package skillbundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestValidateRepositoryBundle(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if manifest.Bundle.Name != "aegis-hermes-skills" || len(manifest.Skills) == 0 {
		t.Fatalf("Validate() returned unexpected manifest: %#v", manifest.Bundle)
	}

	result, err := Evaluate(root, manifest)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Cases < len(requiredEvaluationClasses) || result.Passed != result.Cases {
		t.Fatalf("Evaluate() result = %#v", result)
	}
}

func TestTrustContextInspectionSkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	const (
		slug      = "aegis-trust-context-inspection"
		operation = "aegis.trust-context.inspect"
	)
	var owner *OperationOwner
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
			break
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("operation owner = %#v, want shipped %q owner", owner, slug)
	}

	var skill *Skill
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("manifest does not declare %q", slug)
	}
	if skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" {
		t.Fatalf("skill authority boundary = class %q, network %q, filesystem %q", skill.AuthorityClass, skill.Network, skill.Filesystem)
	}
	if len(skill.Dependencies) != 1 || skill.Dependencies[0] != "aegis" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("skill routing contract = dependencies %#v, operations %#v", skill.Dependencies, skill.RequiredOperations)
	}

	skillText := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{
		"aegis charter effective AGENT REVISION --stanza STANZA --environment local",
		"aegis session show SESSION_ID",
		"aegis session authority SESSION_ID",
		"authority_not_unioned",
		"Zero matches and multiple matches both deny",
		"session preview` issues and stores a new short-lived mandate",
	} {
		if !strings.Contains(skillText, required) {
			t.Errorf("SKILL.md missing required inspection boundary %q", required)
		}
	}

	var fixtures struct {
		Fixtures []struct {
			ID                     string         `json:"id"`
			EvaluationTime         string         `json:"evaluation_time"`
			AuthoritativeResult    map[string]any `json:"authoritative_result"`
			ExpectedInterpretation string         `json:"expected_interpretation"`
		} `json:"fixtures"`
	}
	mustDecode(t, mustRead(t, filepath.Join(root, skill.Path, "references", "inspection-fixtures.json")), &fixtures)
	byID := make(map[string]map[string]any, len(fixtures.Fixtures))
	for _, fixture := range fixtures.Fixtures {
		byID[fixture.ID] = fixture.AuthoritativeResult
	}
	for _, id := range []string{
		"single-authenticated-match",
		"zero-authorized-matches",
		"multiple-authorized-matches",
		"forged-conversational-identity",
		"expired-existing-session",
		"revoked-existing-session",
		"stale-or-mismatched-authority",
	} {
		if byID[id] == nil {
			t.Errorf("inspection fixtures missing %q", id)
		}
	}
	for _, id := range []string{"zero-authorized-matches", "multiple-authorized-matches", "forged-conversational-identity"} {
		result := byID[id]
		authority, ok := result["authority"].(map[string]any)
		if !ok || len(authority) != 0 {
			t.Errorf("fixture %q authority = %#v, want an empty projection", id, result["authority"])
		}
		decision, ok := result["decision"].(map[string]any)
		if !ok || decision["allowed"] != false || result["authority_not_unioned"] != true {
			t.Errorf("fixture %q is not a fail-closed, non-unioned denial: %#v", id, result)
		}
	}
	for _, fixture := range fixtures.Fixtures {
		if fixture.ID != "single-authenticated-match" {
			continue
		}
		evaluatedAt, err := time.Parse(time.RFC3339, fixture.EvaluationTime)
		if err != nil {
			t.Fatalf("fixture %q evaluation_time = %q: %v", fixture.ID, fixture.EvaluationTime, err)
		}
		mandate, ok := fixture.AuthoritativeResult["mandate"].(map[string]any)
		if !ok {
			t.Fatalf("fixture %q mandate = %#v", fixture.ID, fixture.AuthoritativeResult["mandate"])
		}
		issuedValue, issuedOK := mandate["issued_at"].(string)
		expiresValue, expiresOK := mandate["expires_at"].(string)
		if !issuedOK || !expiresOK {
			t.Fatalf("fixture %q mandate timestamps = issued_at %#v, expires_at %#v", fixture.ID, mandate["issued_at"], mandate["expires_at"])
		}
		issuedAt, issuedErr := time.Parse(time.RFC3339, issuedValue)
		expiresAt, expiresErr := time.Parse(time.RFC3339, expiresValue)
		if issuedErr != nil || expiresErr != nil || evaluatedAt.Before(issuedAt) || !evaluatedAt.Before(expiresAt) {
			t.Fatalf("fixture %q evaluation time %s is outside mandate validity [%v, %v)", fixture.ID, evaluatedAt, mandate["issued_at"], mandate["expires_at"])
		}
		for _, required := range []string{"At the declared fixture evaluation_time only", "not evidence of current live authority"} {
			if !strings.Contains(fixture.ExpectedInterpretation, required) {
				t.Errorf("fixture %q interpretation must preserve historical-only scope with %q", fixture.ID, required)
			}
		}
	}
}

func TestCharterDesignSkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	const (
		slug      = "aegis-charter-design"
		operation = "aegis.charter.design"
	)
	var owner *OperationOwner
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
			break
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("operation owner = %#v, want shipped %q owner", owner, slug)
	}

	var skill *Skill
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("manifest does not declare %q", slug)
	}
	if skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" || len(skill.RequiredToolsets) != 0 || len(skill.Sensitivity) != 0 {
		t.Fatalf("skill authority boundary = %#v", skill)
	}
	if len(skill.Dependencies) != 1 || skill.Dependencies[0] != "aegis" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("skill routing contract = dependencies %#v, operations %#v", skill.Dependencies, skill.RequiredOperations)
	}

	skillText := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{
		"aegis design --draft REQUIREMENTS_FILE",
		"aegis design --smoke",
		"aegis charter validate FILE",
		"aegis charter import FILE",
		"aegis charter list AGENT",
		"aegis charter show AGENT [REVISION]",
		"aegis charter explain AGENT [REVISION] --stanza STANZA --environment ENVIRONMENT",
		"aegis charter effective AGENT [REVISION] --stanza STANZA --environment ENVIRONMENT",
		"authority_not_unioned",
		"Charter import is a consequential canonical write",
		"Both shipped design modes import a successful proposal",
		"Smoke mode is non-interactive, but it is not non-mutating",
		"Never describe smoke as a protocol-only or non-mutating check",
		"Never describe a proposal as validated",
	} {
		if !strings.Contains(skillText, required) {
			t.Errorf("SKILL.md missing charter design contract %q", required)
		}
	}
}

func TestAuditVerificationSkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	const (
		slug      = "aegis-audit-verification"
		operation = "aegis.audit.verify"
	)
	var owner *OperationOwner
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
			break
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("operation owner = %#v, want shipped %q owner", owner, slug)
	}

	var skill *Skill
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("manifest does not declare %q", slug)
	}
	if skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" || len(skill.RequiredToolsets) != 0 || len(skill.Sensitivity) != 0 {
		t.Fatalf("skill authority boundary = %#v", skill)
	}
	if strings.Join(skill.Dependencies, ",") != "aegis,aegis-trust-context-inspection" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("skill routing contract = dependencies %#v, operations %#v", skill.Dependencies, skill.RequiredOperations)
	}

	skillText := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{
		"aegis audit list",
		"aegis audit verify",
		"aegis audit delivery-status",
		"aegis audit verify-delivery",
		"aegis audit deliver --limit LIMIT",
		"aegis audit rebuild-projection",
		"there is no separate shipped `aegis audit timeline` command",
		"Never rewrite, append, suppress, reorder, delete, repair, sign, or attest canonical history",
		"Keep these classes distinct",
	} {
		if !strings.Contains(skillText, required) {
			t.Errorf("SKILL.md missing audit verification contract %q", required)
		}
	}

	var document struct {
		Fixtures []struct {
			ID                  string         `json:"id"`
			AuthoritativeResult map[string]any `json:"authoritative_result"`
		} `json:"fixtures"`
	}
	fixtureBytes := mustRead(t, filepath.Join(root, skill.Path, "references", "audit-fixtures.json"))
	mustDecode(t, fixtureBytes, &document)
	fixtureByID := make(map[string]map[string]any, len(document.Fixtures))
	for _, fixture := range document.Fixtures {
		fixtureByID[fixture.ID] = fixture.AuthoritativeResult
	}
	for _, id := range []string{
		"valid-identity-to-runtime-lineage",
		"tampered-event",
		"tampered-checkpoint",
		"delivery-pending",
		"delivery-degraded",
		"delivery-unverifiable",
		"audit-storage-unavailable",
		"interrupted-projection-recovery",
	} {
		if fixtureByID[id] == nil {
			t.Errorf("audit fixtures missing %q", id)
		}
	}

	valid := fixtureByID["valid-identity-to-runtime-lineage"]
	verification, ok := valid["verification"].(map[string]any)
	if !ok || verification["valid"] != true {
		t.Fatalf("valid lineage lacks authoritative verification: %#v", valid["verification"])
	}
	events, ok := valid["events"].([]any)
	if !ok || len(events) != 4 {
		t.Fatalf("valid lineage events = %#v", valid["events"])
	}
	previous := ""
	for index, raw := range events {
		event, eventOK := raw.(map[string]any)
		if !eventOK || event["previous_digest"] != previous {
			t.Fatalf("valid lineage breaks at event %d: %#v", index, raw)
		}
		previous, _ = event["event_digest"].(string)
		if event["id"] == "" || previous == "" {
			t.Fatalf("valid lineage event %d lacks immutable identity: %#v", index, event)
		}
	}
	checkpoint, ok := valid["checkpoint"].(map[string]any)
	if !ok || checkpoint["last_digest"] != previous || checkpoint["signature_verified"] != true {
		t.Fatalf("valid lineage checkpoint does not bind verified head: %#v", checkpoint)
	}
	last := events[len(events)-1].(map[string]any)
	for _, field := range []string{"principal_id", "agent_id", "stanza_id", "charter_digest", "approval_id", "provisioning_id", "mandate_id", "session_id", "runtime"} {
		if last[field] == nil || last[field] == "" {
			t.Errorf("valid lineage final event missing %q: %#v", field, last)
		}
	}

	if failure := fixtureByID["tampered-event"]; failure["valid"] != false || failure["failure_kind"] != "event_digest" || failure["failure_index"] != float64(2) {
		t.Errorf("tampered-event does not stop at exact event: %#v", failure)
	}
	if failure := fixtureByID["tampered-checkpoint"]; failure["valid"] != false || failure["failure_kind"] != "checkpoint_head" {
		t.Errorf("tampered-checkpoint does not stop at checkpoint: %#v", failure)
	}
	states := map[string]string{
		"delivery-pending":          "pending",
		"delivery-degraded":         "degraded",
		"delivery-unverifiable":     "unverifiable",
		"audit-storage-unavailable": "unavailable",
	}
	for id, want := range states {
		if got := fixtureByID[id]["state"]; got != want {
			t.Errorf("fixture %q state = %#v, want %q", id, got, want)
		}
	}
	if events := fixtureByID["audit-storage-unavailable"]["events"]; events != nil {
		t.Errorf("unavailable storage rendered as event data: %#v", events)
	}
	for _, forbidden := range []string{"/home/", "private_key", "access_token", "runtime_home"} {
		if strings.Contains(strings.ToLower(string(fixtureBytes)), forbidden) {
			t.Errorf("audit fixture contains forbidden secret/path material %q", forbidden)
		}
	}
}

func TestSessionOperationsSkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	const (
		slug      = "aegis-session-operations"
		operation = "aegis.session.operate"
	)
	var owner *OperationOwner
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
			break
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("operation owner = %#v, want shipped %q owner", owner, slug)
	}

	var skill *Skill
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("manifest does not declare %q", slug)
	}
	if skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" || len(skill.RequiredToolsets) != 0 || len(skill.Sensitivity) != 0 {
		t.Fatalf("skill authority boundary = %#v", skill)
	}
	if strings.Join(skill.Dependencies, ",") != "aegis,aegis-trust-context-inspection,aegis-audit-verification" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("skill routing contract = dependencies %#v, operations %#v", skill.Dependencies, skill.RequiredOperations)
	}

	skillText := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{
		"aegis session preview AGENT --revision REVISION --stanza STANZA --environment local",
		"aegis session start MANDATE_ID",
		"aegis session list",
		"aegis session show SESSION_ID",
		"aegis session authority SESSION_ID",
		"aegis session revoke SESSION_ID --reason REASON",
		"aegis session terminate SESSION_ID --reason REASON",
		"Preview is consequential",
		"Never union stanzas",
		"not host filesystem, network, container, or VM sandboxing",
		"A PID alone never identifies the authorized process",
		"there is no separate shipped session-receipt command",
	} {
		if !strings.Contains(skillText, required) {
			t.Errorf("SKILL.md missing session operations contract %q", required)
		}
	}

	var document struct {
		Fixtures []struct {
			ID                  string         `json:"id"`
			AuthoritativeResult map[string]any `json:"authoritative_result"`
		} `json:"fixtures"`
	}
	fixtureBytes := mustRead(t, filepath.Join(root, skill.Path, "references", "session-fixtures.json"))
	mustDecode(t, fixtureBytes, &document)
	fixtureByID := make(map[string]map[string]any, len(document.Fixtures))
	for _, fixture := range document.Fixtures {
		fixtureByID[fixture.ID] = fixture.AuthoritativeResult
	}
	for _, id := range []string{
		"exact-preview-and-clean-start",
		"ambiguous-stanza-denial",
		"malformed-start-denial",
		"stale-process-replay-denial",
		"interrupted-revocation-recovery",
		"cross-stanza-canary-denial",
	} {
		if fixtureByID[id] == nil {
			t.Errorf("session fixtures missing %q", id)
		}
	}
	for _, id := range []string{"ambiguous-stanza-denial", "malformed-start-denial", "stale-process-replay-denial", "cross-stanza-canary-denial"} {
		if fixtureByID[id]["allowed"] != false {
			t.Errorf("fixture %q does not fail closed: %#v", id, fixtureByID[id])
		}
	}
	for _, forbidden := range []string{"/home/", "runtime_home", "private_key", "access_token"} {
		if strings.Contains(strings.ToLower(string(fixtureBytes)), forbidden) {
			t.Errorf("session fixture contains forbidden secret/path material %q", forbidden)
		}
	}
}

func TestApprovalProvisioningSkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	const (
		slug      = "aegis-approval-provisioning"
		operation = "aegis.approval-provisioning.review"
	)
	var owner *OperationOwner
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
			break
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("operation owner = %#v, want shipped %q owner", owner, slug)
	}

	var skill *Skill
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
			break
		}
	}
	if skill == nil {
		t.Fatalf("manifest does not declare %q", slug)
	}
	if skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" || len(skill.RequiredToolsets) != 0 || len(skill.Sensitivity) != 0 {
		t.Fatalf("skill authority boundary = %#v", skill)
	}
	if strings.Join(skill.Dependencies, ",") != "aegis,aegis-charter-design,aegis-trust-context-inspection,aegis-audit-verification" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("skill routing contract = dependencies %#v, operations %#v", skill.Dependencies, skill.RequiredOperations)
	}

	skillText := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{
		"aegis plan preview AGENT --revision REVISION --environment local",
		"aegis plan show PLAN_ID",
		"aegis approval request PLAN_ID --ttl DURATION",
		"aegis approval show APPROVAL_ID",
		"aegis approval approve APPROVAL_ID",
		"aegis approval reject APPROVAL_ID",
		"aegis provision PLAN_ID APPROVAL_ID",
		"GET /v1/receipts/:id",
		"there is no shipped receipt CLI command",
		"no standalone recovery CLI is shipped",
		"authority_not_unioned: true",
		"Conversational words such as “looks good” are not the typed decision",
		"must not be retried blindly with the consumed approval",
		"does not activate a runtime",
	} {
		if !strings.Contains(skillText, required) {
			t.Errorf("SKILL.md missing approval/provisioning contract %q", required)
		}
	}

	var document struct {
		Fixtures []struct {
			ID                  string         `json:"id"`
			AuthoritativeResult map[string]any `json:"authoritative_result"`
		} `json:"fixtures"`
	}
	fixtureBytes := mustRead(t, filepath.Join(root, skill.Path, "references", "provisioning-fixtures.json"))
	mustDecode(t, fixtureBytes, &document)
	fixtureByID := make(map[string]map[string]any, len(document.Fixtures))
	for _, fixture := range document.Fixtures {
		fixtureByID[fixture.ID] = fixture.AuthoritativeResult
	}
	for _, id := range []string{
		"verified-exact-plan",
		"changed-plan",
		"consumed-replay",
		"interrupted-recovered",
		"interrupted-manual-intervention",
	} {
		if fixtureByID[id] == nil {
			t.Errorf("provisioning fixtures missing %q", id)
		}
	}

	verified := fixtureByID["verified-exact-plan"]
	plan, planOK := verified["plan"].(map[string]any)
	approval, approvalOK := verified["approval"].(map[string]any)
	receipt, receiptOK := verified["receipt"].(map[string]any)
	effective, effectiveOK := verified["effective_authority"].(map[string]any)
	if !planOK || !approvalOK || !receiptOK || !effectiveOK {
		t.Fatalf("verified fixture lacks correlated records: %#v", verified)
	}
	for _, field := range []string{"plan_id", "plan_digest", "charter_digest"} {
		planField := field
		if field == "plan_id" {
			planField = "id"
		} else if field == "plan_digest" {
			planField = "digest"
		}
		if approval[field] != plan[planField] || receipt[field] != plan[planField] {
			t.Errorf("verified fixture %s is not exactly correlated: plan=%#v approval=%#v receipt=%#v", field, plan[planField], approval[field], receipt[field])
		}
	}
	if receipt["approval_id"] != approval["id"] || receipt["status"] != "verified" || receipt["failure"] != nil || receipt["finished_at"] == nil || approval["status"] != "consumed" || effective["authority_not_unioned"] != true {
		t.Errorf("verified fixture does not prove finished receipt, consumed exact approval, and separated authority: %#v", verified)
	}
	runtime, runtimeOK := plan["runtime"].(map[string]any)
	planEnvironment, planEnvironmentOK := plan["environment"].(map[string]any)
	approvalEnvironment, approvalEnvironmentOK := approval["environment"].(map[string]any)
	if !runtimeOK || !planEnvironmentOK || !approvalEnvironmentOK || approval["runtime"] != runtime["runtime"] || approval["runtime_version"] != runtime["version"] || approvalEnvironment["name"] != planEnvironment["name"] {
		t.Errorf("verified fixture runtime/environment bindings do not match: plan=%#v approval=%#v", plan, approval)
	}
	review, reviewOK := verified["review"].(map[string]any)
	requestedByStanza, requestedOK := review["requested_toolsets"].(map[string]any)
	selectedTools, selectedOK := effective["tools"].([]any)
	stanzaID, stanzaIDOK := effective["stanza_id"].(string)
	requestedTools, stanzaOK := requestedByStanza[stanzaID].([]any)
	if !reviewOK || !requestedOK || !selectedOK || !stanzaIDOK || !stanzaOK || len(selectedTools) != len(requestedTools) {
		t.Fatalf("verified fixture lacks separately matched requested/effective toolsets: review=%#v effective=%#v", review, effective)
	}
	for i := range selectedTools {
		if selectedTools[i] != requestedTools[i] {
			t.Errorf("verified fixture effective toolset does not match requested stanza toolset: requested=%#v selected=%#v", requestedTools, selectedTools)
		}
	}
	effects, effectsOK := plan["effects"].([]any)
	artifacts, artifactsOK := receipt["artifacts"].([]any)
	if !effectsOK || !artifactsOK || len(effects) != 1 || len(artifacts) != len(effects) {
		t.Fatalf("verified fixture effect/artifact cardinality mismatch: effects=%#v artifacts=%#v", plan["effects"], receipt["artifacts"])
	}
	effect, effectOK := effects[0].(map[string]any)
	artifact, artifactOK := artifacts[0].(map[string]any)
	if !effectOK || !artifactOK || artifact["path"] != effect["target"] || artifact["action"] != effect["kind"] || artifact["digest"] != effect["digest"] || artifact["verified"] != true {
		t.Errorf("verified fixture artifact does not exactly match approved effect: effect=%#v artifact=%#v", effects[0], artifacts[0])
	}

	changed := fixtureByID["changed-plan"]
	if changed["status"] != "denied" || changed["approval_plan_digest"] == changed["current_plan_digest"] {
		t.Errorf("changed-plan fixture is not a fail-closed mismatch: %#v", changed)
	}
	if replay := fixtureByID["consumed-replay"]; replay["status"] != "consumed" || replay["consumed_at"] == nil {
		t.Errorf("consumed replay fixture is reusable or incomplete: %#v", replay)
	}
	for _, id := range []string{"interrupted-recovered", "interrupted-manual-intervention"} {
		if got := fixtureByID[id]["status"]; got != "failed" {
			t.Errorf("interrupted fixture %q status = %#v, want failed", id, got)
		}
	}
	for _, forbidden := range []string{"/home/", "private_key", "access_token", "runtime_home"} {
		if strings.Contains(strings.ToLower(string(fixtureBytes)), forbidden) {
			t.Errorf("provisioning fixture contains forbidden secret/path material %q", forbidden)
		}
	}
}

func TestAgentRegistrySkill(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	const slug, operation = "aegis-agent-registry", "aegis.agent-registry.govern"
	var skill *Skill
	var owner *OperationOwner
	for i := range manifest.Skills {
		if manifest.Skills[i].Slug == slug {
			skill = &manifest.Skills[i]
		}
	}
	for i := range manifest.Operations {
		if manifest.Operations[i].Operation == operation {
			owner = &manifest.Operations[i]
		}
	}
	if owner == nil || owner.PrimarySkill != slug || owner.Availability != "shipped" {
		t.Fatalf("Agent Registry operation owner = %#v", owner)
	}
	if skill == nil || skill.AuthorityClass != "advisory" || skill.Network != "none" || skill.Filesystem != "none" || strings.Join(skill.Dependencies, ",") != "aegis,aegis-trust-context-inspection" || len(skill.RequiredOperations) != 1 || skill.RequiredOperations[0] != operation {
		t.Fatalf("Agent Registry skill contract = %#v", skill)
	}
	text := string(mustRead(t, filepath.Join(root, skill.Path, "SKILL.md")))
	for _, required := range []string{"aegis agents register FILE", "aegis agents list", "aegis agents show AGENT [REVISION]", "aegis agents history AGENT", "aegis agents enable AGENT FILE", "aegis agents disable AGENT FILE", "aegis agents retire AGENT FILE", "there is no shipped standalone `aegis agents readiness` CLI command", "A Hermes profile is a runtime/provisioning artifact or projection, never the canonical Agent Registry", "An identical retry is idempotent", "A retired Agent cannot be re-enabled or disabled"} {
		if !strings.Contains(text, required) {
			t.Errorf("SKILL.md missing Agent Registry contract %q", required)
		}
	}
	var document struct {
		Fixtures []struct {
			ID                  string         `json:"id"`
			AuthoritativeResult map[string]any `json:"authoritative_result"`
		} `json:"fixtures"`
	}
	fixtureBytes := mustRead(t, filepath.Join(root, skill.Path, "references", "registry-fixtures.json"))
	mustDecode(t, fixtureBytes, &document)
	byID := make(map[string]map[string]any, len(document.Fixtures))
	for _, fixture := range document.Fixtures {
		byID[fixture.ID] = fixture.AuthoritativeResult
	}
	for _, id := range []string{"registered-exact-participant", "duplicate-identical", "duplicate-different-conflict", "stale-lifecycle-revision", "disabled-history-readable", "retired-reactivation-denied", "interrupted-registration-readback"} {
		if byID[id] == nil {
			t.Errorf("Registry fixtures missing %q", id)
		}
	}
	registered := byID["registered-exact-participant"]
	registration, registrationOK := registered["registration"].(map[string]any)
	revision, revisionOK := registered["revision"].(map[string]any)
	initial, initialOK := registration["initial_revision"].(map[string]any)
	if !registrationOK || !revisionOK || !initialOK || registration["agent_id"] != revision["agent_id"] || initial["revision"] != revision["revision"] || initial["digest"] != revision["digest"] {
		t.Errorf("registered fixture does not exactly bind initial revision: %#v", registered)
	}
	if byID["duplicate-identical"]["created"] != false || byID["duplicate-different-conflict"]["state"] != "conflict" || byID["retired-reactivation-denied"]["history_readable"] != true {
		t.Errorf("Registry fixtures do not preserve idempotent, conflict, and readable-history states")
	}
	for _, forbidden := range []string{"/home/", "access_token", "runtime_home", "private_key"} {
		if strings.Contains(strings.ToLower(string(fixtureBytes)), forbidden) {
			t.Errorf("Registry fixture contains forbidden material %q", forbidden)
		}
	}
}

func TestValidateFailsClosed(t *testing.T) {
	t.Run("trailing manifest document", func(t *testing.T) {
		root := copyFixture(t)
		manifestPath := filepath.Join(root, ManifestName)
		data := mustRead(t, manifestPath)
		mustWrite(t, manifestPath, append(data, []byte("{}\n")...))
		assertDenial(t, ValidateBundle(root), "manifest_malformed")
	})

	t.Run("tampered content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		mustWrite(t, skillPath, append(mustRead(t, skillPath), []byte("\nTampered.\n")...))
		assertDenial(t, ValidateBundle(root), "file_digest_mismatch")
	})

	t.Run("secret shaped content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		content := append(mustRead(t, skillPath), []byte("\nSynthetic canary: "+"AKIA"+strings.Repeat("A", 16)+"\n")...)
		mustWrite(t, skillPath, content)
		rehashFirstSkill(t, root, content)
		assertDenial(t, ValidateBundle(root), "secret_literal")
	})

	t.Run("missing denial coverage", func(t *testing.T) {
		root := copyFixture(t)
		var suite EvaluationSuite
		mustDecode(t, mustRead(t, filepath.Join(root, EvaluationsName)), &suite)
		filtered := suite.Cases[:0]
		for _, evaluation := range suite.Cases {
			if evaluation.Class != "secret_canary" {
				filtered = append(filtered, evaluation)
			}
		}
		suite.Cases = filtered
		mustWriteJSON(t, filepath.Join(root, EvaluationsName), suite)
		assertDenial(t, ValidateBundle(root), "evaluation_coverage_missing")
	})

	t.Run("symlinked skill content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		if err := os.Remove(skillPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(repositoryRoot(t), "skills", "aegis", "SKILL.md"), skillPath); err != nil {
			t.Fatal(err)
		}
		assertDenial(t, ValidateBundle(root), "unsafe_file_type")
	})
}

func TestValidateDependencyGraph(t *testing.T) {
	skill := func(slug string, dependencies ...string) Skill {
		return Skill{Slug: slug, Path: "skills/" + slug, Dependencies: dependencies}
	}
	t.Run("self cycle", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{"aegis": skill("aegis", "aegis")})
		assertDenial(t, err, "dependency_cycle")
	})
	t.Run("multi node cycle", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{
			"aegis":           skill("aegis", "aegis-secondary"),
			"aegis-secondary": skill("aegis-secondary", "aegis"),
		})
		assertDenial(t, err, "dependency_cycle")
	})
	t.Run("acyclic graph", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{
			"aegis":           skill("aegis", "aegis-secondary"),
			"aegis-secondary": skill("aegis-secondary"),
		})
		if err != nil {
			t.Fatalf("validateDependencyGraph() error = %v", err)
		}
	})
}

func TestBuildAndVerifyArchiveDeterministically(t *testing.T) {
	root := repositoryRoot(t)
	one := filepath.Join(t.TempDir(), "one.tar.gz")
	two := filepath.Join(t.TempDir(), "two.tar.gz")
	digestOne, err := BuildArchive(root, one, "1.2.3", testRevision)
	if err != nil {
		t.Fatalf("BuildArchive(one) error = %v", err)
	}
	digestTwo, err := BuildArchive(root, two, "1.2.3", testRevision)
	if err != nil {
		t.Fatalf("BuildArchive(two) error = %v", err)
	}
	if digestOne != digestTwo || string(mustRead(t, one)) != string(mustRead(t, two)) {
		t.Fatal("BuildArchive() output is not deterministic")
	}
	manifest, err := VerifyArchive(one, testRevision)
	if err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if manifest.Bundle.Version != "1.2.3" || manifest.Bundle.SourceRevision != testRevision {
		t.Fatalf("verified provenance = %#v", manifest.Bundle)
	}
}

func TestVerifyArchiveRequiresExactImmutableProvenance(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := BuildArchive(repositoryRoot(t), archive, "1.2.3", testRevision); err != nil {
		t.Fatal(err)
	}

	t.Run("mismatched expected revision", func(t *testing.T) {
		_, err := VerifyArchive(archive, "abcdef0123456789abcdef0123456789abcdef01")
		assertDenial(t, err, "archive_provenance_mismatch")
	})
	t.Run("mutable embedded revision", func(t *testing.T) {
		mutable := filepath.Join(t.TempDir(), "mutable.tar.gz")
		rewriteArchiveMember(t, archive, mutable, "skills/aegis-skills.json", func(content []byte) []byte {
			var manifest Manifest
			mustDecode(t, content, &manifest)
			manifest.Bundle.SourceRevision = "repository"
			data, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			return append(data, '\n')
		})
		_, err := VerifyArchive(mutable, testRevision)
		assertDenial(t, err, "archive_provenance_mismatch")
	})
	t.Run("mutable expected revision", func(t *testing.T) {
		_, err := VerifyArchive(archive, "repository")
		assertDenial(t, err, "invalid_source_revision")
	})
}

func TestVerifyArchiveRejectsUndeclaredMember(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := BuildArchive(repositoryRoot(t), archive, "1.2.3", testRevision); err != nil {
		t.Fatal(err)
	}
	withRogue := filepath.Join(t.TempDir(), "rogue.tar.gz")
	addArchiveMember(t, archive, withRogue, "aegis-skills-v1.2.3/README.md", []byte("not declared\n"))
	_, err := VerifyArchive(withRogue, testRevision)
	assertDenial(t, err, "undeclared_archive_member")
}

func ValidateBundle(root string) error {
	_, err := Validate(root)
	return err
}

func assertDenial(t *testing.T, err error, code string) {
	t.Helper()
	var denial *Denial
	if !errors.As(err, &denial) {
		t.Fatalf("error = %v, want Denial code %q", err, code)
	}
	if denial.Code != code {
		t.Fatalf("denial code = %q, want %q (error: %v)", denial.Code, code, err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyFixture(t *testing.T) string {
	t.Helper()
	destination := t.TempDir()
	source := filepath.Join(repositoryRoot(t), "skills")
	err := filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, "skills", rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return os.WriteFile(target, mustRead(t, current), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func rehashFirstSkill(t *testing.T, root string, content []byte) {
	t.Helper()
	manifestPath := filepath.Join(root, ManifestName)
	var manifest Manifest
	mustDecode(t, mustRead(t, manifestPath), &manifest)
	file := &manifest.Skills[0].Files[0]
	file.SHA256 = sha256Digest(content)
	file.Size = int64(len(content))
	manifest.Skills[0].ContentDigest = fileSetDigest(manifest.Skills[0].Files)
	manifest.Bundle.ContentDigest = bundleDigest(manifest.Skills)
	mustWriteJSON(t, manifestPath, manifest)
}

func addArchiveMember(t *testing.T, source, destination, name string, content []byte) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gzIn, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	defer gzIn.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzOut := gzip.NewWriter(out)
	tarOut := tar.NewWriter(gzOut)
	tarIn := tar.NewReader(gzIn)
	for {
		header, nextErr := tarIn.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		copyHeader := *header
		if err := tarOut.WriteHeader(&copyHeader); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(tarOut, tarIn); err != nil {
			t.Fatal(err)
		}
	}
	header := &tar.Header{Name: name, Mode: archiveMode, Size: int64(len(content)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
	if err := tarOut.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tarOut.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func rewriteArchiveMember(t *testing.T, source, destination, member string, rewrite func([]byte) []byte) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gzIn, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	defer gzIn.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzOut := gzip.NewWriter(out)
	tarOut := tar.NewWriter(gzOut)
	tarIn := tar.NewReader(gzIn)
	found := false
	for {
		header, nextErr := tarIn.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		content, readErr := io.ReadAll(tarIn)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasSuffix(header.Name, "/"+member) {
			content = rewrite(content)
			found = true
		}
		copyHeader := *header
		copyHeader.Size = int64(len(content))
		if err := tarOut.WriteHeader(&copyHeader); err != nil {
			t.Fatal(err)
		}
		if _, err := tarOut.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatalf("archive member %q not found", member)
	}
	if err := tarOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustWrite(t *testing.T, file string, data []byte) {
	t.Helper()
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustDecode(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, file string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, file, append(data, '\n'))
}
