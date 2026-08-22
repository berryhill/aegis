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
	Authenticated  bool
	CSRF           string
	Authentication AuthenticationModel
	Surface        SurfaceModel
	CharterImport  bool
	AgentOperation *AgentOperationModel
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

type AuthenticationModel struct {
	Status          string
	ReasonCode      string
	RecoveryCommand string
	BootstrapTTL    string
	SessionTTL      string
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
	CharterImportProposal CharterImportProposal
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
	Label   string
	Enabled bool
	Reason  string
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

// CredentialDetailModel is the metadata-only inspector projection of an
// authoritative encrypted credential record. It deliberately omits secret
// values, ciphertext, wrapped DEKs, nonces, and KEK bytes. The only KEK field
// exposed is the immutable version.
type CredentialDetailModel struct {
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
