package contribution

import (
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"path"
	"reflect"
	"strings"
)

type EngineeringPreparation struct {
	ReadStart         int                          `json:"read_start,omitempty"`
	ReadCount         int                          `json:"read_count,omitempty"`
	InventoryFiles    []loop.RepositoryContextFile `json:"inventory_files,omitempty"`
	Authority         reference.DigestRef          `json:"authority"`
	SessionID         string                       `json:"session_id"`
	MandateID         string                       `json:"mandate_id"`
	Scope             IssueEngineeringScope        `json:"scope"`
	SourceDigest      string                       `json:"source_digest"`
	Base              string                       `json:"base"`
	TemplateDigest    string                       `json:"template_digest"`
	PolicyDigest      string                       `json:"policy_digest"`
	InventoryDigest   string                       `json:"inventory_digest"`
	Inputs            []string                     `json:"inputs"`
	Outputs           []string                     `json:"outputs"`
	RuntimeSessionIDs []string                     `json:"runtime_session_ids"`
	InputBodies       []string                     `json:"input_bodies"`
	OutputBodies      []string                     `json:"output_bodies"`
	Digest            string                       `json:"digest"`
}

// Validate binds full retained transcripts, never acceptance or authority.
func (r EngineeringPreparation) Validate(source Source, policy loop.IssueEngineeringPolicy, templateDigest, base string) error {
	deny := errors.New("invalid engineering preparation evidence")
	wire, _ := json.Marshal(source)
	policyWire, _ := json.Marshal(policy)
	if r.Authority.Validate() != nil || r.SessionID == "" || r.MandateID == "" || r.SourceDigest != Digest(wire) || r.PolicyDigest != Digest(policyWire) || r.TemplateDigest != templateDigest || r.Base != base || r.InventoryDigest == "" || (len(r.Inputs)-r.ReadCount != 2 && len(r.Inputs)-r.ReadCount != 3) || r.ReadCount < 0 || r.ReadCount > MaxEngineeringReadChunks || len(r.Outputs) != len(r.Inputs) || len(r.InputBodies) != len(r.Inputs) || len(r.OutputBodies) != len(r.Inputs) || len(r.RuntimeSessionIDs) != len(r.Inputs) || r.RuntimeSessionIDs[0] == "" || r.RuntimeSessionIDs[1] == "" || r.RuntimeSessionIDs[0] == r.RuntimeSessionIDs[1] {
		return deny
	}
	for i := range r.Inputs {
		if r.RuntimeSessionIDs[i] == "" {
			return deny
		}
		for j := 0; j < i; j++ {
			if r.RuntimeSessionIDs[i] == r.RuntimeSessionIDs[j] {
				return deny
			}
		}
		if len(r.InputBodies[i]) == 0 || len(r.InputBodies[i]) > 65536 || len(r.OutputBodies[i]) == 0 || len(r.OutputBodies[i]) > 16384 || Digest([]byte(r.InputBodies[i])) != r.Inputs[i] || Digest([]byte(r.OutputBodies[i])) != r.Outputs[i] {
			return deny
		}
	}
	offset := len(r.Inputs) - 2 - r.ReadCount
	scopeIndex := len(r.Inputs) - 1
	var inspect struct {
		Source    Source
		Policy    loop.IssueEngineeringPolicy
		Inventory struct {
			Files  []loop.RepositoryContextFile `json:"files"`
			Digest string                       `json:"digest"`
		}
	}
	var derive struct {
		Source   Source
		Policy   loop.IssueEngineeringPolicy
		Coverage []EngineeringReadNote
		Context  []struct {
			Path   string
			Role   string
			Digest string
			Text   string
		}
	}
	var selection struct {
		ReadFiles []string `json:"read_files"`
	}
	if json.Unmarshal([]byte(r.InputBodies[offset]), &inspect) != nil || json.Unmarshal([]byte(r.InputBodies[scopeIndex]), &derive) != nil || json.Unmarshal([]byte(r.OutputBodies[offset]), &selection) != nil || !reflect.DeepEqual(inspect.Source, source) || !reflect.DeepEqual(derive.Source, source) || !reflect.DeepEqual(inspect.Policy, policy) || !reflect.DeepEqual(derive.Policy, policy) || inspect.Inventory.Digest != r.InventoryDigest || len(selection.ReadFiles) == 0 || len(selection.ReadFiles) > 64 || len(selection.ReadFiles) != len(derive.Context) {
		return deny
	}
	complete := inspect.Inventory.Files
	if offset == 1 {
		complete = r.InventoryFiles
		var discovery struct {
			Source          Source
			Policy          loop.IssueEngineeringPolicy
			InventoryDigest string
			Directories     []EngineeringDirectory
		}
		var chosen struct {
			ReadDirectories []string `json:"read_directories"`
		}
		if json.Unmarshal([]byte(r.InputBodies[0]), &discovery) != nil || json.Unmarshal([]byte(r.OutputBodies[0]), &chosen) != nil || !reflect.DeepEqual(discovery.Source, source) || !reflect.DeepEqual(discovery.Policy, policy) || discovery.InventoryDigest != r.InventoryDigest || !reflect.DeepEqual(discovery.Directories, EngineeringDirectories(complete)) {
			return deny
		}
		narrowed, ok := NarrowEngineeringInventory(complete, chosen.ReadDirectories)
		if !ok || !reflect.DeepEqual(narrowed, inspect.Inventory.Files) {
			return deny
		}
	} else if len(r.InventoryFiles) != 0 {
		return deny
	}
	inventoryWire, _ := json.Marshal(struct {
		Kind  string                       `json:"kind"`
		Files []loop.RepositoryContextFile `json:"files"`
	}{"explicit-go-source.v1", complete})
	if Digest(inventoryWire) != r.InventoryDigest {
		return deny
	}
	inventory := map[string]loop.RepositoryContextFile{}
	for _, f := range inspect.Inventory.Files {
		if !policy.Allows(f.Path) || inventory[f.Path].Path != "" {
			return deny
		}
		inventory[f.Path] = f
	}
	covered := map[string]string{}
	if r.ReadCount > 0 {
		if r.ReadStart != offset+1 || r.ReadStart+r.ReadCount != scopeIndex {
			return deny
		}
		var notes []EngineeringReadNote
		var err error
		covered, notes, err = ValidateEngineeringCoverage(r.InputBodies[r.ReadStart:scopeIndex], r.OutputBodies[r.ReadStart:scopeIndex])
		if err != nil || len(covered) != len(derive.Context) || !reflect.DeepEqual(notes, derive.Coverage) {
			return deny
		}
		for _, body := range r.InputBodies[r.ReadStart:scopeIndex] {
			var chunkSource struct{ Source Source }
			if json.Unmarshal([]byte(body), &chunkSource) != nil || !reflect.DeepEqual(chunkSource.Source, source) {
				return deny
			}
		}
	} else if r.ReadStart != 0 || len(derive.Coverage) != 0 {
		return deny
	}
	inspected := map[string]bool{}
	parents := map[string]bool{}
	for i, f := range derive.Context {
		if r.ReadCount > 0 {
			if f.Text != "" {
				return deny
			}
			var ok bool
			f.Text, ok = covered[f.Path]
			if !ok {
				return deny
			}
		}
		expected, ok := inventory[f.Path]
		if !ok || f.Path != selection.ReadFiles[i] || inspected[f.Path] || f.Digest != expected.Digest || Digest([]byte(f.Text)) != f.Digest || int64(len(f.Text)) != expected.Bytes {
			return deny
		}
		inspected[f.Path] = true
		parents[path.Dir(f.Path)] = true
	}
	scope, err := DecodeEngineeringScope([]byte(r.OutputBodies[scopeIndex]), policy)
	if err != nil || !reflect.DeepEqual(scope, r.Scope) {
		return deny
	}
	production, regression := false, false
	for _, p := range scope.WritableFiles {
		if _, ok := inventory[p]; ok {
			if !inspected[p] {
				return deny
			}
		} else if !strings.HasSuffix(p, "_test.go") || !parents[path.Dir(p)] {
			return deny
		}
		regression = regression || strings.HasSuffix(p, "_test.go")
		production = production || !strings.HasSuffix(p, "_test.go")
	}
	if !production || !regression {
		return deny
	}
	for _, p := range scope.ContextFiles {
		if !inspected[p] || strings.HasSuffix(p, "_test.go") {
			return deny
		}
	}
	expected := r.Digest
	r.Digest = ""
	wire, _ = json.Marshal(r)
	if expected != Digest(wire) {
		return deny
	}
	return nil
}
