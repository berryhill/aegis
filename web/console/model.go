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
	Actions       []ActionModel
	Records       []RecordModel
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
	Key     string
	Label   string
	Summary string
	JSON    string
}
