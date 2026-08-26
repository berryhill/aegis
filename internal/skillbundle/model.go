package skillbundle

import "fmt"

const (
	ManifestName       = "skills/aegis-skills.json"
	EvaluationsName    = "skills/evaluations.json"
	ManifestSchema     = 1
	EvaluationSchema   = 1
	MaxManifestBytes   = 1 << 20
	MaxSkillFileBytes  = 256 << 10
	MaxEvaluationBytes = 1 << 20
)

type Manifest struct {
	SchemaVersion int              `json:"schema_version"`
	Bundle        Bundle           `json:"bundle"`
	Operations    []OperationOwner `json:"operations"`
	Skills        []Skill          `json:"skills"`
}

type Bundle struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	AegisRange     string `json:"aegis_compatibility"`
	HermesRange    string `json:"hermes_compatibility"`
	ContentDigest  string `json:"content_digest"`
	SourceRevision string `json:"source_revision"`
}

type OperationOwner struct {
	Operation    string `json:"operation"`
	PrimarySkill string `json:"primary_skill"`
	Availability string `json:"availability"`
}

type Skill struct {
	Slug               string   `json:"slug"`
	Version            string   `json:"version"`
	Path               string   `json:"path"`
	Description        string   `json:"description"`
	Dependencies       []string `json:"dependencies"`
	AuthorityClass     string   `json:"authority_class"`
	RequiredOperations []string `json:"required_operations"`
	RequiredToolsets   []string `json:"required_toolsets"`
	Sensitivity        []string `json:"sensitivity"`
	Network            string   `json:"network"`
	Filesystem         string   `json:"filesystem"`
	ContentDigest      string   `json:"content_digest"`
	Files              []File   `json:"files"`
}

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type EvaluationSuite struct {
	SchemaVersion int              `json:"schema_version"`
	Cases         []EvaluationCase `json:"cases"`
}

type EvaluationCase struct {
	ID             string `json:"id"`
	Class          string `json:"class"`
	Prompt         string `json:"prompt"`
	Expected       string `json:"expected"`
	Operation      string `json:"operation,omitempty"`
	ExpectedSignal string `json:"expected_signal"`
}

type EvaluationResult struct {
	Cases  int `json:"cases"`
	Passed int `json:"passed"`
}

type Denial struct {
	Code string
	Path string
	Why  string
}

func (d *Denial) Error() string {
	if d.Path == "" {
		return fmt.Sprintf("skill bundle denied [%s]: %s", d.Code, d.Why)
	}
	return fmt.Sprintf("skill bundle denied [%s] at %s: %s", d.Code, d.Path, d.Why)
}

func deny(code, path, why string) error {
	return &Denial{Code: code, Path: path, Why: why}
}
