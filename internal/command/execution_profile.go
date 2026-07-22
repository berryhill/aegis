package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/layout"
)

type ExecutionProfile string

const (
	ProductionProfile  ExecutionProfile = "production"
	DevelopmentProfile ExecutionProfile = "development"
)

func ProfileForVersion(version string) ExecutionProfile {
	if version == "dev" {
		return DevelopmentProfile
	}
	if version == "test" {
		return ""
	}
	return ProductionProfile
}

func resolveExecutionProfile(profile ExecutionProfile, developmentRoot string) (layout.Layout, error) {
	switch profile {
	case ProductionProfile:
		home, err := profileHome()
		if err != nil {
			return layout.Layout{}, err
		}
		return profileLayout(home, filepath.Join(home, layout.CanonicalDirectory)), nil
	case DevelopmentProfile:
		root, err := resolveDevelopmentRepository(developmentRoot)
		if err != nil {
			return layout.Layout{}, err
		}
		return profileLayout(root, filepath.Join(root, layout.DevelopmentDirectory)), nil
	case "":
		return layout.Layout{}, nil
	default:
		return layout.Layout{}, fmt.Errorf("unsupported execution profile %q", profile)
	}
}

func profileHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve execution profile home: %w", err)
	}
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return "", errors.New("execution profile home must be a clean absolute path")
	}
	return home, nil
}

func resolveDevelopmentRepository(configured string) (string, error) {
	root := configured
	if root == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve development executable: %w", err)
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return "", fmt.Errorf("resolve development executable links: %w", err)
		}
		root = filepath.Dir(executable)
	}
	root, err := filepath.Abs(root)
	if err != nil || root == "" || filepath.Clean(root) != root || root == filepath.VolumeName(root)+string(filepath.Separator) {
		return "", errors.New("development repository must be a clean absolute non-root path")
	}
	home, err := profileHome()
	if err != nil {
		return "", err
	}
	if root == home || !pathWithinRoot(root, home) {
		return "", errors.New("development repository must be a child of the authenticated operator home")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("development executable must reside in a real repository directory")
	}
	modulePath := filepath.Join(root, "go.mod")
	moduleInfo, err := os.Lstat(modulePath)
	if err != nil || !moduleInfo.Mode().IsRegular() || moduleInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("development executable directory has no real go.mod")
	}
	module, err := os.ReadFile(modulePath)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(module)), "module github.com/berryhill/aegis\n") {
		return "", errors.New("development executable directory is not the Aegis module root")
	}
	gitInfo, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || gitInfo.Mode()&os.ModeSymlink != 0 || !gitInfo.IsDir() && !gitInfo.Mode().IsRegular() {
		return "", errors.New("development executable directory is not an Aegis Git worktree")
	}
	developmentRoot := filepath.Join(root, layout.DevelopmentDirectory)
	legacyDevelopmentRoot := filepath.Join(root, ".aegis-dev")
	legacyPresent := pathExistsNoFollow(legacyDevelopmentRoot)
	if legacyPresent && pathExistsNoFollow(developmentRoot) {
		return "", errors.New("both .aegis and pre-rename .aegis-dev development roots exist; refusing ambiguous state")
	}
	if legacyPresent {
		return "", errors.New("pre-rename .aegis-dev development state exists; migrate it explicitly before using .aegis")
	}
	return root, nil
}

func pathExistsNoFollow(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func profileLayout(scope, root string) layout.Layout {
	state := filepath.Join(root, "state")
	return layout.Layout{Home: scope, Root: root, Config: filepath.Join(root, "aegis.yaml"), State: state}
}

func validateExecutionProfile(profile ExecutionProfile, resolved layout.Layout, options *rootOptions, destructive bool) error {
	if profile == "" {
		return nil
	}
	if resolved.Root == "" || resolved.Config == "" || resolved.State == "" {
		return errors.New("execution profile layout is unresolved")
	}
	if destructive {
		if options.configFile != "" && options.configFile != resolved.Config {
			return fmt.Errorf("%s executable refuses reset outside its fixed profile: got configuration %q, require %q", profile, options.configFile, resolved.Config)
		}
		if options.stateDir != "" && options.stateDir != resolved.State {
			return fmt.Errorf("%s executable refuses reset outside its fixed profile: got state %q, require %q", profile, options.stateDir, resolved.State)
		}
		return nil
	}
	if profile != DevelopmentProfile {
		return nil
	}
	production, err := resolveExecutionProfile(ProductionProfile, "")
	if err != nil {
		return err
	}
	for name, path := range map[string]string{"configuration": options.configFile, "state": options.stateDir} {
		if path != "" && pathWithinRoot(path, production.Root) {
			return fmt.Errorf("development executable refuses %s inside the production profile root: %q", name, path)
		}
	}
	return nil
}

func validateConfiguredPathsProfile(profile ExecutionProfile, paths map[string]string) error {
	if profile != DevelopmentProfile {
		return nil
	}
	production, err := resolveExecutionProfile(ProductionProfile, "")
	if err != nil {
		return err
	}
	for name, path := range paths {
		if path != "" && pathWithinRoot(path, production.Root) {
			return fmt.Errorf("development executable refuses configured %s inside the production profile root: %q", name, path)
		}
	}
	return nil
}

func pathWithinRoot(path, root string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absolute, root = filepath.Clean(absolute), filepath.Clean(root)
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	relative, err := filepath.Rel(root, absolute)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
