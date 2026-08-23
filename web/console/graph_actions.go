package consoleweb

import (
	"context"
	"html/template"
	"io"

	"github.com/a-h/templ"
)

// GraphReferenceOption is one immutable revision offered by the Graph composer.
// Value is an opaque server-generated selection token; Label is review text.
type GraphReferenceOption struct {
	Value string
	Label string
}

type GraphComposerModel struct {
	CSRF   string
	Agents []GraphReferenceOption
	Loops  []GraphReferenceOption
}

type GraphRunInputModel struct {
	ID       string
	Type     string
	Required bool
}

type GraphRunFormModel struct {
	CSRF      string
	GraphID   string
	Revision  uint64
	Digest    string
	Lifecycle string
	Inputs    []GraphRunInputModel
}

type GraphActionResultModel struct {
	Title       string
	Outcome     string
	Reason      string
	GraphID     string
	RecordKey   string
	QueueItemID string
}

var graphComposerTemplate = template.Must(template.New("graph-composer").Funcs(template.FuncMap{
	"seq": seq,
	"types": func() []string {
		return []string{"string", "boolean", "integer", "number", "object", "array", "artifact"}
	},
}).Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><title>Compose Graph · Aegis</title><link rel="stylesheet" href="/console/assets/app.css"></head><body><a class="skip" href="#main">Skip to content</a><header class="topbar"><span class="topbar-brand"><span class="brand-mark" aria-hidden="true"></span>Aegis</span><span class="topbar-sep">/</span><span class="topbar-context">Graph composer</span><span class="topbar-status"><span class="status-dot"></span>Authenticated control plane</span></header><main class="content action-page" id="main"><nav class="crumb-row"><a class="back-crumb" href="/console/graphs#/graphs">← Back to Graphs</a></nav><section class="panel"><div class="panel-body"><span class="eyebrow">Immutable definition</span><h1>Compose and publish a Graph revision</h1><p class="inline-notice">Every participant and Loop selection is an exact immutable revision. The server reloads each selection, derives node ports from the exact Loop interface, validates topology and policy declarations, computes the canonical digest, and performs fresh authority admission. Blank rows are ignored.</p><form class="graph-action-form" method="post" action="/console/graphs/publish"><input type="hidden" name="csrf" value="{{.CSRF}}"><fieldset><legend>Revision and authority</legend><label>Authority-bound runtime session ID<input name="authority_session_id" required maxlength="255" autocomplete="off"></label><label>Graph ID<input name="graph_id" required maxlength="255"></label><label>Revision<input name="revision" type="number" min="1" required></label><label>Expected previous digest<input name="expected_previous_digest" maxlength="71" placeholder="blank for revision 1"></label><label>Publication idempotency key<input name="idempotency_key" required maxlength="255"></label></fieldset><fieldset><legend>Graph input and output schema</legend><p>Declare up to eight typed ports. Required is explicit; types are not inferred.</p>{{range $i := seq 8}}<div class="form-row"><label>Input ID<input name="input_id_{{$i}}" maxlength="255"></label><label>Type<select name="input_type_{{$i}}"><option value=""></option>{{range types}}<option>{{.}}</option>{{end}}</select></label><label><input name="input_required_{{$i}}" type="checkbox" value="true"> Required</label><label>Output ID<input name="output_id_{{$i}}" maxlength="255"></label><label>Type<select name="output_type_{{$i}}"><option value=""></option>{{range types}}<option>{{.}}</option>{{end}}</select></label><label><input name="output_required_{{$i}}" type="checkbox" value="true"> Required</label></div>{{end}}</fieldset><fieldset><legend>Exact nodes</legend><p>Node ports are re-resolved from the selected exact Loop revision. Up to eight nodes.</p>{{range $i := seq 8}}<div class="form-row"><label>Node ID<input name="node_id_{{$i}}" maxlength="255"></label><label>Participant<select name="node_agent_{{$i}}"><option value=""></option>{{range $.Agents}}<option value="{{.Value}}">{{.Label}}</option>{{end}}</select></label><label>Loop<select name="node_loop_{{$i}}"><option value=""></option>{{range $.Loops}}<option value="{{.Value}}">{{.Label}}</option>{{end}}</select></label></div>{{end}}</fieldset><fieldset><legend>Graph-input mappings</legend>{{range $i := seq 8}}<div class="form-row"><label>Graph input<input name="input_map_graph_{{$i}}"></label><label>To node<input name="input_map_node_{{$i}}"></label><label>To port<input name="input_map_port_{{$i}}"></label></div>{{end}}</fieldset><fieldset><legend>Typed dependencies</legend><p>Mappings use <code>source_port&gt;target_port</code>, comma-separated.</p>{{range $i := seq 8}}<div class="form-row"><label>Dependency ID<input name="dependency_id_{{$i}}"></label><label>From node<input name="dependency_from_{{$i}}"></label><label>To node<input name="dependency_to_{{$i}}"></label><label>Mappings<input name="dependency_mappings_{{$i}}" placeholder="result&gt;draft"></label></div>{{end}}</fieldset><fieldset><legend>Graph-output mappings</legend>{{range $i := seq 8}}<div class="form-row"><label>From node<input name="output_map_node_{{$i}}"></label><label>From port<input name="output_map_port_{{$i}}"></label><label>Graph output<input name="output_map_graph_{{$i}}"></label></div>{{end}}</fieldset><fieldset><legend>Admission policies</legend>{{range $i := seq 4}}<div class="form-row"><label>Rule ID<input name="policy_rule_{{$i}}"></label><label>Exact policy ID<input name="policy_id_{{$i}}"></label><label>Exact digest<input name="policy_digest_{{$i}}" maxlength="71"></label></div>{{end}}</fieldset><button class="primary" type="submit">Validate and publish exact revision</button></form></div></section></main></body></html>`))

var graphRunTemplate = template.Must(template.New("graph-run").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><title>Run Graph · Aegis</title><link rel="stylesheet" href="/console/assets/app.css"></head><body><a class="skip" href="#main">Skip to content</a><header class="topbar"><span class="topbar-brand"><span class="brand-mark"></span>Aegis</span><span class="topbar-sep">/</span><span class="topbar-context">Run Graph</span><span class="topbar-status"><span class="status-dot"></span>Authenticated control plane</span></header><main class="content action-page" id="main"><nav class="crumb-row"><a class="back-crumb" href="/console/graphs?record_key={{.GraphID}}%3A{{.Revision}}#/graphs">← Back to exact Graph</a></nav><section class="panel"><div class="panel-body"><span class="eyebrow">Typed immutable submission</span><h1>Run {{.GraphID}} revision {{.Revision}}</h1><dl class="spec"><dt>Exact digest</dt><dd>{{.Digest}}</dd><dt>Lifecycle</dt><dd>{{.Lifecycle}}</dd></dl><p class="inline-notice">Review the authority-bound runtime session and typed inputs below. The server reloads this exact active Graph, derives authority only from that session, canonicalizes each value, and creates either one durable rejection or one immutable Queue admission.</p><form class="graph-action-form" method="post" action="/console/graphs/submit"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="graph_id" value="{{.GraphID}}"><input type="hidden" name="graph_revision" value="{{.Revision}}"><input type="hidden" name="graph_digest" value="{{.Digest}}"><fieldset><legend>Authority and immutable identities</legend><label>Authority-bound runtime session ID<input name="authority_session_id" required maxlength="255"></label><label>Submission idempotency key<input name="idempotency_key" required maxlength="255"></label><label>Maximum attempts<input name="max_attempts" type="number" min="1" max="100" value="1" required></label></fieldset><fieldset><legend>Typed Graph inputs</legend>{{if .Inputs}}{{range .Inputs}}<label>{{.ID}} ({{.Type}}; required={{.Required}})<textarea name="input.{{.ID}}" {{if .Required}}required{{end}} maxlength="1048576" placeholder="{{if eq .Type "string"}}plain text{{else}}canonical JSON {{.Type}}{{end}}"></textarea></label>{{end}}{{else}}<p>This Graph declares no inputs.</p>{{end}}</fieldset><button class="primary" type="submit">Normalize, review, and submit run</button></form></div></section></main></body></html>`))

var graphResultTemplate = template.Must(template.New("graph-result").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="dark"><title>{{.Title}} · Aegis</title><link rel="stylesheet" href="/console/assets/app.css"></head><body><main class="content action-page" id="main"><section class="panel"><div class="panel-body"><span class="eyebrow">Authoritative result</span><h1>{{.Title}}</h1><dl class="spec"><dt>Outcome</dt><dd>{{.Outcome}}</dd><dt>Reason</dt><dd>{{.Reason}}</dd>{{if .QueueItemID}}<dt>Queue item</dt><dd>{{.QueueItemID}}</dd>{{end}}</dl><a class="primary" href="/console/graphs{{if .RecordKey}}?record_key={{.RecordKey}}{{end}}#/graphs">Return to Graphs</a>{{if .QueueItemID}} <a class="secondary" href="/console/queue?record_key={{.QueueItemID}}#/queue">Open exact Queue execution</a>{{end}}</div></section></main></body></html>`))

func seq(count int) []int {
	values := make([]int, count)
	for i := range values {
		values[i] = i + 1
	}
	return values
}

func GraphComposerDocument(model GraphComposerModel) templ.Component {
	return templateComponent(graphComposerTemplate, model, template.FuncMap{"seq": seq, "types": func() []string {
		return []string{"string", "boolean", "integer", "number", "object", "array", "artifact"}
	}})
}

func GraphRunDocument(model GraphRunFormModel) templ.Component {
	return templateComponent(graphRunTemplate, model, nil)
}

func GraphActionResultDocument(model GraphActionResultModel) templ.Component {
	return templateComponent(graphResultTemplate, model, nil)
}

func templateComponent(source *template.Template, data any, funcs template.FuncMap) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, writer io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		selected := source
		if funcs != nil {
			clone, err := source.Clone()
			if err != nil {
				return err
			}
			selected = clone.Funcs(funcs)
		}
		return selected.Execute(writer, data)
	})
}
