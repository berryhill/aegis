package loop

// NewImplementationRevision builds a publishable draft, never authority or a
// Queue submission. Publication still uses the authenticated fleet service.
func NewImplementationRevision(id string, revision uint64, previous string, contract VerifiedImplementation) (LoopRevision, LoopValidationResult, error) {
	return NewRevision(LoopRevision{
		SchemaVersion: ImplementationRevisionSchemaVersion, LoopID: id, Revision: revision, PreviousDigest: previous, EntryStepID: "implement",
		Steps: []Step{
			{ID: "implement", Kind: StepAction, Retry: RetryPolicy{MaxAttempts: 1}, Implementation: &contract,
				EvidenceClaims: []EvidenceClaim{{Claim: "verified-implementation", MediaType: "application/json", VerifierID: "aegis.implementation.verifier", PolicyVersion: VerifiedImplementationSchema}}},
			{ID: "done", Kind: StepTerminal, Retry: RetryPolicy{MaxAttempts: 1}, Terminal: &TerminalDefinition{Outcome: OutcomeSucceeded}},
		},
		Transitions:      []Transition{{ID: "verified-done", FromStepID: "implement", ToStepID: "done"}},
		RequiredEvidence: []EvidenceRequirement{{Claim: "verified-implementation", ProducerStepID: "implement"}},
	})
}
