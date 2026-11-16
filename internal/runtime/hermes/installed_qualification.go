package hermes

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// This record is compiled controller policy, never caller/config supplied.
// Path-independent content identities permit PM to publish the same reviewed
// inputs at another installation, not a version range or arbitrary generation.
//
//go:embed qualification/installed-codex-v1.json
var installedCodexRecordJSON []byte

type installedCodexRecord struct {
	Schema          int                `json:"schema"`
	Accepted        bool               `json:"accepted"`
	Version         string             `json:"version"`
	Source          string             `json:"source"`
	Launcher        string             `json:"launcher"`
	Python          string             `json:"python"`
	PythonPackage   string             `json:"python_package"`
	PythonVersion   []int              `json:"python_version"`
	CacheTag        string             `json:"cache_tag"`
	SOABI           string             `json:"soabi"`
	Inputs          map[string]*string `json:"inputs"`
	Workspace       map[string]string  `json:"workspace"`
	WorkspaceSource string             `json:"workspace_source"`
	Dependencies    string             `json:"dependencies"`
}

type installedActivation struct {
	Schema      int                `json:"schema"`
	Project     string             `json:"project"`
	Environment string             `json:"environment"`
	Stamp       string             `json:"stamp"`
	Inputs      map[string]*string `json:"inputs"`
	Workspace   map[string]string  `json:"workspace"`
	Interpreter struct {
		Python   string `json:"python"`
		Version  []int  `json:"version"`
		CacheTag string `json:"cache_tag"`
		SOABI    string `json:"soabi"`
	} `json:"interpreter"`
}

func qualificationJSON(path string, out any) error {
	data, err := readCommandFile(path, 1<<20)
	if err != nil || json.Unmarshal(data, out) != nil {
		return ErrProviderAuthRuntimeUnqualified
	}
	return nil
}

func qualificationHash(path string) (string, error) {
	// WalkDir has already rejected namespace symlinks. Reopen each leaf without
	// following substitutions and compare its identity after streaming the hash.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", ErrProviderAuthRuntimeUnqualified
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > 128<<20 {
		return "", ErrProviderAuthRuntimeUnqualified
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, (128<<20)+1))
	after, statErr := os.Lstat(path)
	if err != nil || statErr != nil || n != before.Size() || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return "", ErrProviderAuthRuntimeUnqualified
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Hash the complete importable source namespace, including unexpected added
// modules. Exclude only non-runtime/build/state trees, never a caller allowlist.
func qualificationTree(root string, dependencies bool, workspace string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && (entry.Name() == "__pycache__" || (!dependencies && (strings.HasPrefix(entry.Name(), ".") || slices.Contains([]string{"venv", "tests", "website", "docs", "research", "skills", "ui-tui", "apps", "web", "node_modules"}, entry.Name())))) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".pyc") || strings.HasSuffix(rel, ".pyo") {
			return nil
		}
		if dependencies {
			// Installer receipts contain installation paths and are not executable.
			if strings.Contains(rel, ".dist-info/") && slices.Contains([]string{"RECORD", "direct_url.json"}, entry.Name()) {
				return nil
			}
		} else if !strings.HasSuffix(rel, ".py") && !strings.HasSuffix(rel, ".so") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrProviderAuthRuntimeUnqualified
		}
		digest, err := qualificationHash(path)
		if err != nil {
			return err
		}
		if dependencies && strings.HasPrefix(entry.Name(), "__editable___hermes_agent_") && strings.HasSuffix(rel, "_finder.py") {
			data, err := readCommandFile(path, 2<<20)
			if err != nil {
				return err
			}
			digest = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(string(data), workspace, "$WORKSPACE"))))
		}
		fmt.Fprintf(h, "%s\x00%s\n", filepath.ToSlash(rel), digest)
		return nil
	})
	if err != nil {
		return "", ErrProviderAuthRuntimeUnqualified
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validateInstalledCodexRecord(root, version string, record installedCodexRecord) error {
	deny := ErrProviderAuthRuntimeUnqualified
	if record.Schema != 1 || version != record.Version || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return deny
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return deny
	}
	source, err := qualificationTree(root, false, "")
	if err != nil || source != record.Source {
		return deny
	}
	launcher := filepath.Join(root, ".hermes/bin/hermes")
	data, err := readCommandFile(launcher, 64<<10)
	if err != nil {
		return deny
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(string(data), "#!/bin/sh\nexec "), " -I -c ")
	python := strings.Trim(first, "'")
	if !strings.HasPrefix(string(data), "#!/bin/sh\nexec ") || !launchPath.MatchString(python) || (first != python && first != "'"+python+"'") {
		return deny
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(data), python, "$PYTHON"), root, "$SOURCE")
	if fmt.Sprintf("%x", sha256.Sum256([]byte(normalized))) != record.Launcher {
		return deny
	}
	pythonID, err := readLaunchIdentity(python)
	if err != nil || hex.EncodeToString(pythonID.hash[:]) != record.Python {
		return deny
	}
	store := filepath.Dir(python)
	for filepath.Base(store) != "tools" && store != "/" {
		store = filepath.Dir(store)
	}
	if filepath.Base(store) != "tools" {
		return deny
	}
	owner := filepath.Dir(store)
	var tools struct {
		Schema   int `json:"schema"`
		Packages struct {
			Python struct {
				Entry  string `json:"entry"`
				Digest string `json:"digest"`
			} `json:"python"`
		} `json:"packages"`
	}
	if qualificationJSON(filepath.Join(store, "facts.json"), &tools) != nil || tools.Schema != 1 || record.PythonPackage == "" || tools.Packages.Python.Digest != record.PythonPackage || filepath.Join(store, tools.Packages.Python.Entry, "bin/python3") != python {
		return deny
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(root)))[:16]
	state := filepath.Join(owner, "installs", key)
	var facts struct {
		Schema   int `json:"schema"`
		Packages struct {
			Venv struct {
				Environment  string `json:"environment"`
				Stamp        string `json:"stamp"`
				ResolvedLock string `json:"resolved_lock"`
			} `json:"venv"`
		} `json:"packages"`
	}
	if qualificationJSON(filepath.Join(state, "facts.json"), &facts) != nil || facts.Schema != 1 {
		return deny
	}
	environment := facts.Packages.Venv.Environment
	generation := filepath.Dir(environment)
	if !filepath.IsAbs(environment) || filepath.Base(environment) != "venv" || filepath.Dir(generation) != filepath.Join(state, "environments") {
		return deny
	}
	var activation installedActivation
	if qualificationJSON(filepath.Join(generation, "activation.json"), &activation) != nil || activation.Schema != 1 || activation.Project != root || activation.Environment != environment || activation.Stamp == "" || activation.Stamp != facts.Packages.Venv.Stamp {
		return deny
	}
	resolvedPython, err := filepath.EvalSymlinks(python)
	if err != nil || activation.Interpreter.Python != resolvedPython || !slices.Equal(activation.Interpreter.Version, record.PythonVersion) || activation.Interpreter.CacheTag != record.CacheTag || activation.Interpreter.SOABI != record.SOABI {
		return deny
	}
	if len(activation.Inputs) != len(record.Inputs) || len(activation.Workspace) != len(record.Workspace) {
		return deny
	}
	for name, expected := range record.Inputs {
		actual, ok := activation.Inputs[name]
		if !ok || (expected == nil) != (actual == nil) {
			return deny
		}
		if expected == nil {
			if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
				return deny
			}
			continue
		}
		hash, err := qualificationHash(filepath.Join(root, name))
		if err != nil || hash != *expected || *actual != *expected {
			return deny
		}
	}
	workspace := filepath.Join(generation, "workspace")
	if facts.Packages.Venv.ResolvedLock != filepath.Join(workspace, "uv.lock") {
		return deny
	}
	for name, expected := range record.Workspace {
		hash, err := qualificationHash(filepath.Join(workspace, name))
		if err != nil || hash != expected || activation.Workspace[name] != expected {
			return deny
		}
	}
	workspaceSource, err := qualificationTree(workspace, false, "")
	if err != nil || workspaceSource != record.WorkspaceSource {
		return deny
	}
	deps, err := qualificationTree(filepath.Join(environment, "lib", "python3.14", "site-packages"), true, workspace)
	if err != nil || deps != record.Dependencies {
		return deny
	}
	return nil
}
