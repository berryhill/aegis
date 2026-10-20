package consoleweb

// QueueRunResultModel is joined only for the selected record after exact
// content authorization. All values are untrusted text escaped by templ.
type QueueRunResultModel struct {
	Task, Status, Verification, Usage, Notice string
	Fields                                    []FieldModel
	Name, Content, Encoding                   string
	Verified                                  bool
}
