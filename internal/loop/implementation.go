package loop

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// VerifiedImplementation is an additive action contract, not a shell program.
// It is deliberately separate from v2 so legacy canonical bytes are unchanged.
const VerifiedImplementationSchema = "aegis.loop.verified-implementation.v1"

type RequiredGoTest struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

type GoTestPolicy struct {
	RequiredTests  []RequiredGoTest `json:"required_tests"`
	Kind           string           `json:"kind"`
	Packages       []string         `json:"packages"`
	TimeoutSeconds uint16           `json:"timeout_seconds"`
}

type VerifiedImplementation struct {
	SchemaVersion string       `json:"schema_version"`
	Task          string       `json:"task"`
	Acceptance    string       `json:"acceptance"`
	Workspace     string       `json:"workspace"`
	WritableFiles []string     `json:"writable_files"`
	Policy        GoTestPolicy `json:"policy"`
	MaxPasses     uint8        `json:"max_passes"`
}

// ImplementationDraft explicitly leaves workspace unresolved. It cannot execute
// or be published as an executable contract until the operator binds it.
func ImplementationDraft(task, acceptance string) VerifiedImplementation {
	return VerifiedImplementation{SchemaVersion: VerifiedImplementationSchema, Task: task, Acceptance: acceptance, Policy: GoTestPolicy{Kind: "go-test.v1", Packages: []string{"./..."}, TimeoutSeconds: 60}, MaxPasses: 2}
}

var goPackagePath = regexp.MustCompile(`^\./[A-Za-z0-9_./-]*$`)

func (v VerifiedImplementation) Validate() error {
	if v.SchemaVersion != VerifiedImplementationSchema || strings.TrimSpace(v.Task) == "" || strings.TrimSpace(v.Acceptance) == "" || len(v.Task) > 32768 || len(v.Acceptance) > 32768 {
		return errors.New("exact version, bounded task and acceptance are required")
	}
	if !filepath.IsAbs(v.Workspace) || filepath.Clean(v.Workspace) != v.Workspace || v.Workspace == "/" || v.MaxPasses < 1 || v.MaxPasses > 2 {
		return errors.New("explicit bounded workspace and one or two passes are required")
	}
	if len(v.WritableFiles) == 0 || len(v.WritableFiles) > 128 {
		return errors.New("explicit writable source files required")
	}
	seen := map[string]bool{}
	for _, p := range v.WritableFiles {
		if !filepath.IsLocal(p) || filepath.Clean(p) != p || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "\\") || seen[p] {
			return errors.New("only unique local non-test Go source paths are writable")
		}
		seen[p] = true
	}
	if v.Policy.Kind != "go-test.v1" || v.Policy.TimeoutSeconds < 1 || v.Policy.TimeoutSeconds > 120 || len(v.Policy.Packages) == 0 || len(v.Policy.Packages) > 32 {
		return errors.New("bounded typed Go test policy required")
	}
	for _, p := range v.Policy.Packages {
		if p != "." && (!goPackagePath.MatchString(p) || strings.Contains(strings.TrimSuffix(p, "/..."), "..")) {
			return errors.New("local Go package pattern required")
		}
	}
	if len(v.Policy.RequiredTests) == 0 || len(v.Policy.RequiredTests) > 128 {
		return errors.New("explicit required test identities required")
	}
	tests := map[string]bool{}
	for _, test := range v.Policy.RequiredTests {
		key := test.Package + "/" + test.Name
		if test.Package == "" || strings.ContainsAny(test.Package, " 	\n") || !regexp.MustCompile(`^Test[A-Za-z0-9_]+$`).MatchString(test.Name) || tests[key] {
			return errors.New("unique package and top-level test identities required")
		}
		tests[key] = true
	}
	return nil
}

func (v VerifiedImplementation) Digest() (string, error) {
	if err := v.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return sha256Digest(b), nil
}

func DecodeVerifiedImplementation(b []byte) (VerifiedImplementation, error) {
	v, err := decodeStrict[VerifiedImplementation](b)
	if err != nil {
		return v, err
	}
	return v, v.Validate()
}
