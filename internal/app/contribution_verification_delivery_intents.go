package app

import "github.com/berryhill/aegis/internal/delivery"

// The plan is derived by the existing owning Source projection from actual
// accepted native evidence and reviewed policy. An observed intent selects only
// its closed operation; it cannot supply its own expected payload or targets.
func assessRetainedIntentPayloads(h delivery.HistorySnapshot, p ContributionDeliveryPolicy, plan delivery.Plan) string {
	objects := make(map[string]delivery.GitObject, len(plan.Objects))
	for _, object := range plan.Objects {
		if _, duplicate := objects[object.OID]; duplicate {
			return "object_invalid"
		}
		objects[object.OID] = object
	}
	if plan.Delivery != "local-only" && plan.Delivery != "no-publish" {
		pr, err := retainedContributionPR(h, p)
		if err != nil {
			return "pr_invalid"
		}
		plan.PR = pr
	}
	for _, chain := range h.Histories {
		if len(chain) == 0 {
			return "history_invalid"
		}
		record := chain[len(chain)-1].Record
		intent := record.Intent
		if intent.Candidate != p.Binding.Candidate {
			continue
		}
		expected := plan
		switch intent.Operation {
		case delivery.ReconcileIssue:
			// A retained evidence assessment can never authorize closing an issue.
			return "completion_authority_unavailable"
		case delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit:
			object, ok := objects[record.Head]
			if !ok {
				return "object_invalid"
			}
			expected.Object = object
		}
		if delivery.ValidateIntentForPlan(expected, intent) != nil {
			return "intent_invalid"
		}
	}
	return "intent_payloads_assessed"
}
