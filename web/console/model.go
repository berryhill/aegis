package consoleweb

//go:generate go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate -path .

const (
	DomainAgents = "agents"
	DomainLoops  = "loops"
	DomainGraphs = "graphs"
	DomainQueue  = "queue"
)

type PageModel struct {
	Authenticated bool
	CSRF          string
	Surface       SurfaceModel
}

type SurfaceModel struct {
	Domain        string
	Title         string
	State         string
	Status        string
	Records       []RecordModel
	Inspector     *RecordModel
	InspectorOpen bool
}

type RecordModel struct {
	Key     string
	Label   string
	Summary string
	JSON    string
}
