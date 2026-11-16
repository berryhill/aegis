package hermes

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// Pin the installation-selected metadata across credential custody and Start.
// Bootstrap still validates it and retains the real PM/source locks in the child.
// This does not infer a generation from HOME or accept a caller's generation.
func installedLaunchMetadata(root, python string) ([]string, error) {
	store := filepath.Dir(python)
	for filepath.Base(store) != "tools" && store != "/" {
		store = filepath.Dir(store)
	}
	if store == "/" {
		// Non-store developer launchers cannot pass the actual activation API.
		// Keep protocol-only fixtures separate from production qualification.
		return nil, nil
	}
	owner := filepath.Dir(store)
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(root)))[:16]
	state := filepath.Join(owner, "installs", key)
	factsPath := filepath.Join(state, "facts.json")
	var facts struct {
		Packages struct {
			Venv struct {
				Environment string `json:"environment"`
			} `json:"venv"`
		} `json:"packages"`
	}
	if err := qualificationJSON(factsPath, &facts); err != nil {
		return nil, err
	}
	environment := facts.Packages.Venv.Environment
	generation := filepath.Dir(environment)
	if !filepath.IsAbs(environment) || filepath.Base(environment) != "venv" || filepath.Dir(generation) != filepath.Join(state, "environments") {
		return nil, errGatewayCommand
	}
	return []string{filepath.Join(store, "facts.json"), factsPath, filepath.Join(generation, "activation.json"), filepath.Join(environment, "pyvenv.cfg"), filepath.Join(generation, "workspace/pyproject.toml"), filepath.Join(generation, "workspace/uv.lock")}, nil
}
