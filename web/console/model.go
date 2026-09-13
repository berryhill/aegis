package consoleweb

//go:generate go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate -path .

const (
	DomainAgents      = "agents"
	DomainLoops       = "loops"
	DomainGraphs      = "graphs"
	DomainQueue       = "queue"
	DomainCredentials = "credentials"
)

type PageModel struct {
	Authenticated       bool
	CSRF                string
	Authentication      AuthenticationModel
	Surface             SurfaceModel
	CharterImport       bool
	AgentOperation      *AgentOperationModel
	LoopComposer        *LoopComposerModel
	CommandPreview      *CommandPreviewModel
	CommandReceipt      *OperationReceiptModel
	CredentialOperation *CredentialOperationModel
}

// AgentOperationModel is a server-owned preparation or result projection. Raw
// artifacts are used only to refill the preparation form after a denied review;
// reviewed execution carries only a session-bound opaque receipt.
type AgentOperationModel struct {
	Stage, Status, ReasonCode, Charter, Fixture  string
	Receipt, FleetID, SourceID, AgentID          string
	CharterDigest, Revision, RevisionDigest      string
	Runtime, Owner, Accountability               string
	Capabilities, Policies, Lifecycle, ResultURL string
}

type LoopPublisherModel struct {
	ID, Revision, Digest, Runtime string
}

type LoopComposerModel struct {
	Publishers []LoopPublisherModel
	Errors     []string
}

type CommandPreviewModel struct {
	IntentID, CommandID, TargetID, TargetDigest, InputDigest, ExpiresAt string
}

type AuthenticationModel struct {
	Status     string
	ReasonCode string
	SessionTTL string
}

type SurfaceModel struct {
	Domain        string
	CSRF          string
	Title         string
	Eyebrow       string
	Description   string
	State         string
	Status        string
	Source        string
	ReasonCode    string
	Authoritative bool
	TotalCount    int
	Query         string
	Lifecycle     string
	QueueState    string
	TotalRecords  int
	Actions       []ActionModel
	Records       []RecordModel
	ActiveRecords []RecordModel
	FailedRecords []RecordModel
	QueueStates   []string
	Inspector     *RecordModel
	InspectorOpen bool
	// CollectionURL is the server-rendered return location for the current
	// bounded collection context. It contains presentation-only filters and
	// pagination, never identity or authority inputs.
	CollectionURL         string
	Pagination            PaginationModel
	CharterImportProposal CharterImportProposal
}

// CredentialOperationModel contains only metadata safe for browser rendering.
// Secret input remains only in the session-bound, one-use review receipt.
type CredentialOperationModel struct {
	Stage, Operation, Status, ReasonCode, Receipt    string
	RecordID, Reference, Kind, Version, Reason       string
	AgentID, StanzaID, DeploymentID, Scope           string
	Destinations, Mode, VersionPolicy, PinnedVersion string
	Result                                           *OperationReceiptModel
}

// CharterImportProposal is a review-only bridge to the existing CLI charter
// workflow. It contains no browser mutation endpoint or authority input.
type CharterImportProposal struct {
	Notice          string
	ValidateCommand string
	ImportCommand   string
}

type ActionModel struct {
	Key           string
	Label         string
	State         string
	ReasonCode    string
	RepairActions []string
	Primary       bool
}

type RecordModel struct {
	Key          string
	Digest       string
	Label        string
	Summary      string
	JSON         string
	Lifecycle    string
	Readiness    string
	Revision     string
	Runtime      string
	Source       string
	Owner        string
	Authority    string
	Provisioning string
	Fields       []FieldModel
	Links        []LinkModel
	Graph        *GraphDetailModel
	Queue        *QueueDetailModel
	Credential   *CredentialDetailModel
	Loop         *LoopDetailModel
	Agent        *AgentDetailModel
}

// AgentDetailModel is a structured, display-only projection of one exact
// immutable Registry revision. DeclaredAuthority is not an effective stanza,
// mandate, or permission grant.
type AgentDetailModel struct {
	StableID, FleetID, SourceKind, SourceID, OwnerID, AccountabilityID              string
	RuntimeAdapter, Runtime, RuntimeTarget                                          string
	CharterID, CharterDigest, RevisionDigest                                        string
	CharterRevision, Revision                                                       uint64
	Capabilities, Policies                                                          []string
	DeclaredAuthority, EffectiveAuthority, ProvisioningEvidence                     string
	AuthorityState, EvaluatedFor, CharterEvidence, HistoryEvidence, SessionEvidence string
	Historical, LifecycleEligible                                                   bool
	AuthorityFields, ReceiptFields, SessionFields                                   []FieldModel
	CharterHistoryEvidence                                                          string
	CharterHistory                                                                  []FieldModel
	History, Executions                                                             []LinkModel
	Stanzas                                                                         []AgentStanzaModel
}

// AgentStanzaModel contains declarations only, never an authorization grant.
type AgentStanzaModel struct {
	ID, Name string
	Enabled  bool
	Fields   []FieldModel
}

// LinkModel is a presentation-only transition to an exact related record.
// Its stable record URL never carries identity or authority input.
type LinkModel struct{ Label, Detail, URL string }

// LoopDetailModel is a presentation-only projection of one authoritative,
// immutable Loop revision. Definition data is deliberately separate from
// execution records: this model never carries a run, attempt, artifact,
// receipt, or disposition.
type LoopDetailModel struct {
	TargetID, Digest, PreviousDigest, PublisherID, ExpectedLifecycleDigest string
	EntryStepID, Validation, ValidationDigest                              string
	Description, LatestVersion, GraphChildSummary                          string
	CycleSummary, ExitCondition, ExhaustionDestination                     string
	CanActivate, CanRetire                                                 bool
	CanvasWidth, CanvasHeight                                              int
	Inputs, Outputs                                                        []LoopPortModel
	Steps                                                                  []LoopStepModel
	Transitions                                                            []LoopTransitionModel
	RequiredEvidence                                                       []LoopEvidenceRequirementModel
	ValidationIssues                                                       []FieldModel
	Provenance                                                             []FieldModel
	LifecycleHistory                                                       []LoopLifecycleEventModel
}

type LoopPortModel struct {
	ID, Type string
	Required bool
}

type LoopStepModel struct {
	ID, Kind, GateMode, TerminalOutcome         string
	DisplayName, Description, Instruction, Tool string
	Capabilities                                []string
	TimeoutSeconds                              uint32
	FailureBehavior                             string
	ExpectedOutput                              string
	MaxAttempts                                 uint16
	Entry                                       bool
	X, Y                                        int
	Inputs, Outputs                             []LoopPortModel
	EvidenceClaims                              []LoopEvidenceClaimModel
	TerminalMappings                            []LoopPortMappingModel
}

type LoopEvidenceClaimModel struct {
	Claim, MediaType, ExpectedDigest, VerifierID, PolicyVersion string
}

type LoopEvidenceRequirementModel struct{ Claim, ProducerStepID string }

type LoopPortMappingModel struct{ SourcePort, TargetPort string }

type LoopTransitionModel struct {
	ID, FromStepID, ToStepID, Condition string
	MaxTraversals                       uint16
	Path                                string
	LabelX, LabelY                      int
	Return                              bool
	Mappings                            []LoopPortMappingModel
}

type LoopLifecycleEventModel struct {
	EventID, State, Revision, PreviousDigest, Publisher, Authority string
	MandateID, StanzaID, OccurredAt, Digest                        string
}

// QueueDetailModel is a presentation-only projection of authoritative queue
// records. The pinned Graph/Loop revision is reconstructed independently of the
// current catalogue; the UI never derives success from the presence of runtime
// output. Six supporting tabs are populated directly from authoritative runtime
// facts so the contextual action can be evaluated without leaving the workspace.
type QueueDetailModel struct {
	// Header / summary facts.
	ExecutionType       string // Graph run · pinned Graph/Loop snapshot identity.
	QueueItemIdentity   string
	QueueItemDigest     string
	SnapshotDigest      string
	PinnedGraph         string
	PinnedGraphRevision string
	PinnedGraphDigest   string
	Participant         string
	CatalogueDrift      string
	SubmittedAt         string
	AdmittedAt          string
	StartedAt           string
	EndedAt             string
	ReconstructionWarn  string
	ContextualAction    *QueueControlModel // Only one reviewed operation may surface at a time.

	// Pinned control-flow projection.
	Nodes     []QueueControlNodeModel
	Edges     []QueueControlEdgeModel
	NodeCount int
	EdgeCount int

	// Tab inputs.
	Inputs    []QueueInputModel
	Outputs   []QueueOutputModel
	Timeline  []QueueTimelineModel
	Authority []FieldModel
	Evidence  []QueueEvidenceModel
	Admission []FieldModel
	Snapshot  []FieldModel

	// Tab references (related records) and durable state facts.
	Links            []LinkModel
	GraphRunDigest   string
	ArtifactState    string
	ReceiptState     string
	DispositionState string
	TerminalOutcome  string
	FailureLocation  string // Graph node ID where the authoritative failure lives.
	CycleWarning     string // Bounded-cycle summary if any node is part of a cycle.
}

// QueueControlNodeModel projects one exact pinned Graph node onto authoritative
// runtime state for that exact coordinate. Selection is by array index, not by
// any untrusted ID.
type QueueControlNodeModel struct {
	Index            int
	GridColumn       int
	GridRow          int
	NodeID           string
	State            string // projected from authoritative runtime: pending, started, succeeded, failed, denied, cancelled, expired, revoked, terminal.
	ExecutionState   string // authoritative Loop execution state.
	AttemptNumber    uint32 // authoritative latest attempt number for this node (0 if none).
	AttemptState     string // authoritative latest attempt state.
	Current          bool   // node is the current control point.
	TerminalEligible bool   // step carries a terminal outcome that may finish here.
	FailureLocation  bool   // authoritative failure location.
	CycleMember      bool   // node participates in a bounded cycle.
	Reachable        bool   // successor edges that have not been taken in the current causal chain.
}

// QueueControlEdgeModel projects one exact pinned Graph edge onto its
// authoritative transition outcome.
type QueueControlEdgeModel struct {
	Index    int
	EdgeID   string
	From     string
	To       string
	Mappings string
	Outcome  string // taken / not-taken / pending / unreachable.
	Cycle    bool   // edge participates in a cycle.
}

type QueueInputModel struct {
	PortID string
	Type   string
	Value  string
	Source string // default / caller / system / pinned reference.
	Status string // applicable / missing / rejected / applied.
}

type QueueOutputModel struct {
	PortID       string
	Type         string
	Applicability string
	Completeness  string // complete / partial / unavailable / inapplicable.

}

type QueueEvidenceModel struct {
	Claim, MediaType, ExpectedDigest, VerifierID, PolicyVersion, Outcome, AttemptDigest, FailureCategory, ObservedAt string
}

type QueueTimelineModel struct {
	Title, State, At, Detail, Cause string
}

type QueueControlModel struct {
	Operation   string
	Label       string
	Enabled     bool
	Reason      string
	Consequence string
}

// QueueTopology is a presentation-only projection of the exact pinned Graph
// revision onto authoritative runtime state. It cannot validate, sequence, or
// admit execution. Array positions, never untrusted IDs, identify inspector
// panels.
//
// Positioning uses CSS-grid track placement (gridColumn/gridRow) rather than
// inline pixel styles. This keeps the rendered HTML compatible with the
// console's strict Content Security Policy (style-src 'self').
type QueueTopology = QueuePinnedTopology

type GraphDetailModel struct {
	GraphID             string
	LatestVersion       string
	CurrentValidation   string
	ValidationIssues    []GraphIssueModel
	SubmissionIssues    []GraphIssueModel
	Digest              string
	PreviousDigest      string
	Validation          string
	InputSchema         []FieldModel
	OutputSchema        []FieldModel
	Nodes               []GraphNodeModel
	Edges               []GraphEdgeModel
	Policies            []FieldModel
	AcceptedRuns        []GraphRunModel
	RejectedSubmissions []FieldModel
	Links               []LinkModel
}

type GraphIssueModel struct {
	Code, Path, Message string
}

type GraphNodeModel struct {
	Links                         []LinkModel
	InputMappings, OutputMappings []FieldModel
	ID                            string
	Participant                   string
	Loop                          string
	Inputs                        string
	Outputs                       string
}

type GraphEdgeModel struct {
	ID       string
	From     string
	To       string
	Mappings string
}

// LoopTopology is a presentation-only projection of one Loop revision. It
// cannot validate, sequence, or admit execution. Array positions, never
// untrusted IDs, identify inspector panels.
//
// Positioning uses CSS-grid track placement (gridColumn/gridRow) rather than
// inline pixel styles. This keeps the rendered HTML compatible with the
// console's strict Content Security Policy (style-src 'self').
type LoopTopology struct {
	Nodes                 []LoopPosition
	Edges                 []LoopLine
	Issues                []LoopIssueModel
	Columns               int
	Rows                  int
	CycleMembers          []string
	CycleMaxIterations    uint16
	CycleExitCondition    string
	CycleExhaustionToID   string
	ExhaustionDestination string
}

type LoopPosition struct {
	Index               int
	GridColumn, GridRow int
	Step                LoopStepModel
	Incoming, Outgoing  []LoopTransitionModel
	DisplayName         string
	InDegree, OutDegree int
}

type LoopLine struct {
	Index int
	Path  string
}

// LoopIssueModel describes a presentation-time concern derived from a stored
// revision. It is never an admission decision.
type LoopIssueModel struct {
	Code, Path, Message string
}

type GraphRunModel struct {
	Submission string
	Snapshot   string
	QueueItem  string
	GraphRun   string
	Authority  string
	Mandate    string
	Runtime    string
	Inputs     string
}

type FieldModel struct {
	Label string
	Value string
}

// VisualState presents an authoritative readback outcome. The browser never
// derives or promotes it.
type VisualState string

const (
	StateLoading       VisualState = "loading"
	StateEmpty         VisualState = "empty"
	StateFilteredEmpty VisualState = "filtered-empty"
	StateDenied        VisualState = "denied"
	StateUnavailable   VisualState = "unavailable"
	StateDegraded      VisualState = "degraded"
	StateError         VisualState = "error"
)

type NoticeModel struct{ Kind, Title, Message, ReasonCode string }

type FormFieldModel struct {
	ID, Name, Label, Type, Value, Help, Error, Autocomplete, MinLength string
	Required, Secret                                                   bool
}

type ExactReferenceModel struct{ Label, ID, Revision, Digest, Lifecycle, Provenance string }

// AuthorityContextModel is deliberately display-only. State is authoritative
// admission readback, never an input or selector.
type AuthorityContextModel struct{ Identity, Stanza, Mandate, State, ReasonCode string }

type OperationReceiptModel struct{ Title, Outcome, OperationID, RecordedAt, ReasonCode, Message string }
type FilterOptionModel struct{ Value, Label string }
type FilterModel struct {
	ID, Label, Name, Value string
	Options                []FilterOptionModel
}
type PaginationModel struct {
	Label, PreviousURL, NextURL, Summary string
	HasPrevious, HasNext                 bool
}
type OverlayModel struct {
	ID, Title, Description, CloseLabel string
}
type ConfirmationModel struct {
	Title, Message, ConfirmLabel, CancelLabel, DialogID string
	CancelURL                                           string
	Dangerous                                           bool
}

// CredentialDetailModel is the metadata-only inspector projection of an
// authoritative encrypted credential record. It deliberately omits secret
// values, ciphertext, wrapped DEKs, nonces, and KEK bytes. The only KEK field
// exposed is the immutable version.
type CredentialDetailModel struct {
	ID             string
	Reference      string
	Kind           string
	Status         string
	CurrentVersion uint64
	CreatedAt      string
	CreatedBy      string
	RevokedAt      string
	Revocation     string
	BindingCount   int
	Versions       []CredentialVersionDetail
	Vault          CredentialVaultDetail
	Backup         CredentialBackupDetail
	Proposal       CredentialProposalDetail
}

// CredentialVersionDetail is one immutable encrypted version entry. It is
// metadata-only; ciphertext, wrapped DEK, and record nonce are never rendered.
type CredentialVersionDetail struct {
	Version        uint64
	Algorithm      string
	KEKVersion     uint64
	CiphertextHash string
	CreatedAt      string
}

// CredentialVaultDetail summarises the encrypted authority vault that owns
// the record. Database path and KEK bytes are never included.
type CredentialVaultDetail struct {
	DeploymentID      string
	StoreID           string
	KEKID             string
	KEKVersion        uint64
	SchemaVersion     string
	Custody           string
	LastCleanShutdown bool
	InitializedAt     string
	State             string
	ReasonCode        string
}

// CredentialBackupDetail records the last successful ciphertext-only backup.
// Backups require the same KEK to reopen; bytes are never included.
type CredentialBackupDetail struct {
	Available  bool
	TargetPath string
	Note       string
}

// CredentialProposalDetail carries the review-only CLI previews the operator
// can copy. They never POST; browser state cannot authorize credential
// mutation.
type CredentialProposalDetail struct {
	PutCommand    string
	BackupCommand string
	Notice        string
}
