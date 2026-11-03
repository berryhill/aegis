package consoleweb

func registryCardFact(record RecordModel, kind string) string {
	value := ""
	if record.Agent != nil {
		switch kind {
		case "stanzas":
			value = record.Agent.DeclaredStanzaSummary
		case "authority":
			value = record.Agent.EffectiveAuthoritySummary
		case "receipt":
			value = record.Agent.ReceiptSummary
		}
	}
	if value == "" {
		return "Unavailable"
	}
	return value
}

// Registry cards summarize evidence; they never turn lifecycle eligibility into
// runtime authority. Full source statements remain in the detail and title text.
func registryCardReadiness(value string) string {
	switch value {
	case "Lifecycle eligible; fresh authority admission required":
		return "Fresh admission required"
	case "Execution denied until a new enabled revision":
		return "Denied until enabled"
	case "Terminal; no later revisions permitted":
		return "Terminal"
	default:
		return value
	}
}

func registryCardProvisioning(value string) string {
	if value == "Not asserted by Registry record" {
		return "Not asserted"
	}
	return value
}
