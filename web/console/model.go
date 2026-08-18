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
	Authenticated bool
	CSRF          string
	Surface       SurfaceModel
}

type SurfaceModel struct {
	Domain        string
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
	TotalRecords  int
	Actions       []ActionModel
	Records       []RecordModel
	ActiveRecords []RecordModel
	FailedRecords []RecordModel
	QueueStates   []string
	Inspector     *RecordModel
	InspectorOpen bool
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
}

// QueueDetailModel is a presentation-only projection of authoritative queue
// records. States remain attached to their source record; the UI never derives
// success from the presence of runtime output.
type QueueDetailModel struct {
	QueueItem        []FieldModel
	Runtime          []FieldModel
	GraphRun         QueueExecutionNodeModel
	Loops            []QueueExecutionNodeModel
	Attempts         []QueueAttemptModel
	Timeline         []QueueTimelineModel
	Artifact         []FieldModel
	Receipts         []QueueReceiptModel
	Disposition      []FieldModel
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
