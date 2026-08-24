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
	Domain                string
	CSRF                  string
	Title                 string
	Eyebrow               string
	Description           string
	State                 string
	Status                string
	Source                string
	ReasonCode            string
	Authoritative         bool
	TotalCount            int
	Query                 string
	Lifecycle             string
	QueueState            string
	TotalRecords          int
	Actions               []ActionModel
	Records               []RecordModel
	ActiveRecords         []RecordModel
	FailedRecords         []RecordModel
	QueueStates           []string
	Inspector             *RecordModel
	InspectorOpen         bool
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
	Graph        *GraphDetailModel
	Queue        *QueueDetailModel
	Credential   *CredentialDetailModel
	Loop         *LoopDetailModel
}

type LoopDetailModel struct {
	TargetID, Digest, PublisherID, ExpectedLifecycleDigest string
	CanActivate, CanRetire                                 bool
}

// QueueDetailModel is a presentation-only projection of authoritative queue
// records. States remain attached to their source record; the UI never derives
// success from the presence of runtime output.
type QueueDetailModel struct {
	QueueItem        []FieldModel
	Dependencies     []FieldModel
	Runtime          []FieldModel
	GraphRun         QueueExecutionNodeModel
	Loops            []QueueExecutionNodeModel
	Attempts         []QueueAttemptModel
	Timeline         []QueueTimelineModel
	Artifact         []FieldModel
	Receipts         []QueueReceiptModel
	Disposition      []FieldModel
	Claims           []FieldModel
	Retries          []FieldModel
	Cancellations    []FieldModel
	Controls         []QueueControlModel
	ArtifactState    string
	ReceiptState     string
	DispositionState string
}

type QueueExecutionNodeModel struct{ ID, Kind, State, Binding, Digest string }

type QueueAttemptModel struct {
	ID, State, LoopID, ClaimID, Created, Digest string
	Number                                      uint32
}

type QueueTimelineModel struct{ Title, State, At, Detail string }

type QueueControlModel struct {
	Operation   string
	Label       string
	Enabled     bool
	Reason      string
	Consequence string
}

type QueueReceiptModel struct {
	ID, Outcome, Claim, Verifier, ExpectedDigest, ObservedDigest, FailureCategory, ObservedAt string
}

type GraphDetailModel struct {
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
}

type GraphNodeModel struct {
	ID          string
	Participant string
	Loop        string
	Inputs      string
	Outputs     string
}

type GraphEdgeModel struct {
	ID       string
	From     string
	To       string
	Mappings string
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
