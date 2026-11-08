package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
)

// Generation intent is retained before executing the literal compiler. Full
// source preimages and modes remain in private content-addressed custody.
type templGenerationIntent struct {
	Run          string          `json:"run"`
	Pass         uint8           `json:"pass"`
	Contract     string          `json:"contract"`
	Policy       string          `json:"policy"`
	Tool         string          `json:"tool"`
	Toolchain    string          `json:"toolchain"`
	Dependencies string          `json:"dependencies"`
	Source       SourceInventory `json:"source"`
	Inputs       []string        `json:"inputs"`
	Outputs      []string        `json:"outputs"`
}
type templGenerationReceipt struct {
	Intent       string `json:"intent"`
	OutputSource string `json:"output_source"`
	Edits        []Edit `json:"edits"`
}

func generateTemplPairs(ctx context.Context, driver, workspace string, env []string, source SourceInventory, pairs []string, run func(context.Context, *exec.Cmd) error, admit Admit) error {
	if pinnedTemplModule(source) != nil {
		return errTemplConsistency
	}
	for _, input := range pairs {
		if !loop.SafeRepositoryContextPath(input) {
			return errTemplConsistency
		}
		if err := admit(ctx, "generate:"+input+"->"+templOutput(input)); err != nil {
			return err
		}
		cmd := exec.Command(driver, "run", templTool, "generate", "-f", input, "-include-timestamp=false", "-include-version=true")
		cmd.Dir, cmd.Env = workspace, append([]string(nil), env...)
		var output, diagnostic boundedOutput
		cmd.Stdout, cmd.Stderr = &output, &diagnostic
		if err := run(ctx, cmd); err != nil {
			return errors.Join(errTemplConsistency, err)
		}
		if output.overflow || diagnostic.overflow {
			return errTemplConsistency
		}
	}
	return nil
}

// Exact generated output bytes are the only permitted source delta; reject
// extra, missing, mode-changed or nonregular inputs, including go.mod/go.sum.
func generatedTemplEdits(before, after SourceInventory, pairs []string) ([]Edit, error) {
	allowed := map[string]bool{}
	for _, p := range pairs {
		allowed[templOutput(p)] = true
	}
	originals := map[string]SourceFile{}
	for _, f := range before.Files {
		originals[f.Path] = f
	}
	if len(before.Files) != len(after.Files) {
		return nil, errTemplConsistency
	}
	edits := []Edit{}
	seen := map[string]bool{}
	for _, f := range after.Files {
		old, ok := originals[f.Path]
		if !ok || seen[f.Path] || old.Mode != f.Mode {
			return nil, errTemplConsistency
		}
		seen[f.Path] = true
		if !allowed[f.Path] && !bytes.Equal(old.Content, f.Content) {
			return nil, errTemplConsistency
		}
		if allowed[f.Path] {
			edits = append(edits, Edit{Path: f.Path, Content: f.Content, preserveMode: os.FileMode(old.Mode)})
		}
	}
	if len(edits) != len(allowed) {
		return nil, errTemplConsistency
	}
	return edits, nil
}

func (e *ManagedContribution) generateTempl(ctx context.Context, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy, r ContributionRecord, source SourceInventory, expected *CollectedRepositoryContext) (string, SourceInventory, error) {
	pairs := selectedTemplPairs(c.WritableFiles)
	if len(pairs) == 0 || !p.TemplGeneratedWrites {
		return "", source, nil
	}
	if validateTemplSelection(c, p) != nil {
		return "", source, errTemplConsistency
	}
	intent := templGenerationIntent{Run: r.RunID, Pass: r.Consumed, Contract: r.Contract, Policy: contributionPolicyDigest(p), Tool: templTool, Toolchain: e.toolchainDigest, Dependencies: p.Dependencies.Digest, Source: source, Inputs: pairs, Outputs: []string{}}
	for _, input := range pairs {
		intent.Outputs = append(intent.Outputs, templOutput(input))
	}
	wire, _ := json.Marshal(intent)
	intentDigest, err := e.blob(wire)
	if err != nil {
		return "", source, err
	}
	if err = e.DB.Create([]byte(fmt.Sprintf("contribution/generation-intent/%s/%d", digest([]byte(r.RunID)), r.Consumed)), wire); err != nil {
		return "", source, err
	}
	output, err := e.nativeOperation(ctx, c, source, nil, true)
	if err != nil {
		return "", source, err
	}
	var edits []Edit
	if err = json.Unmarshal(output, &edits); err != nil {
		return "", source, err
	}
	// The native copy was isolated; re-observe the actual task-owned preimages
	// before fresh per-path authorization and atomic replacement.
	observed, err := InventoryWithPolicy(ctx, c.Workspace, e.sourcePolicy)
	if err != nil || observed.Digest != source.Digest {
		return "", source, errTemplConsistency
	}
	for i := range edits {
		for _, f := range source.Files {
			if f.Path == edits[i].Path {
				edits[i].preserveMode = os.FileMode(f.Mode)
			}
		}
	}
	if err = (&Executor{Admit: e.Admit}).apply(ctx, c, edits, expected); err != nil {
		return "", source, err
	}
	frozen, err := InventoryWithPolicy(ctx, c.Workspace, e.sourcePolicy)
	if err != nil {
		return "", source, err
	}
	if _, err = generatedTemplEdits(source, frozen, pairs); err != nil {
		return "", source, err
	}
	receipt := templGenerationReceipt{Intent: intentDigest, OutputSource: frozen.Digest, Edits: edits}
	wire, _ = json.Marshal(receipt)
	result, err := e.blob(wire)
	return result, frozen, err
}

func (e *ManagedContribution) validateTemplGeneration(r ContributionRecord, c ContributionCandidate) error {
	if c.Generation == "" {
		return nil
	}
	wire := previousBytes(e.DB, c.Generation)
	var receipt templGenerationReceipt
	if json.Unmarshal(wire, &receipt) != nil || receipt.OutputSource != c.Source.Digest {
		return errTemplConsistency
	}
	wire = previousBytes(e.DB, receipt.Intent)
	var intent templGenerationIntent
	if json.Unmarshal(wire, &intent) != nil || !r.Policy.TemplGeneratedWrites || intent.Run != r.RunID || intent.Pass != c.Pass || intent.Contract != r.Contract || intent.Policy != c.Policy || intent.Tool != templTool || intent.Toolchain != c.Toolchain || r.Policy.Dependencies == nil || intent.Dependencies != r.Policy.Dependencies.Digest || pinnedTemplModule(intent.Source) != nil {
		return errTemplConsistency
	}
	stored, err := e.DB.Get([]byte(fmt.Sprintf("contribution/generation-intent/%s/%d", digest([]byte(r.RunID)), c.Pass)))
	if err != nil || !bytes.Equal(wire, stored) {
		return errTemplConsistency
	}
	expected, err := generatedTemplEdits(intent.Source, c.Source, intent.Inputs)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(receipt.Edits)
	if !bytes.Equal(a, b) || len(intent.Inputs) == 0 || len(intent.Inputs) != len(intent.Outputs) {
		return errTemplConsistency
	}
	for i, input := range intent.Inputs {
		if !loop.SafeRepositoryContextPath(input) || intent.Outputs[i] != templOutput(input) {
			return errTemplConsistency
		}
	}
	return nil
}
