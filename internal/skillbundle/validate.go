package skillbundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	slugPattern          = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	semverPattern        = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestPattern        = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	revisionPattern      = regexp.MustCompile(`^(repository|[0-9a-f]{40})$`)
	operationPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	remoteActivePattern  = regexp.MustCompile(`(?i)<\s*(script|iframe|object|embed)\b|<\s*link\b[^>]*rel\s*=\s*["']?stylesheet|javascript\s*:`)
	inlineNetworkPattern = regexp.MustCompile(`(?i)(^|[\s` + "`" + `])(curl|wget|nc|ncat|telnet)\s+|https?://[^\s)>]+\.(js|mjs|wasm)([?#\s)>]|$)`)
	secretPatterns       = []*regexp.Regexp{
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|client[_-]?secret)\s*[:=]\s*["']?[A-Za-z0-9_./+=-]{16,}`),
	}
	positiveAuthorityPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(this|the) skill\s+(authenticates|authorizes|approves|provisions|activates|completes|verifies completion)\b`),
		regexp.MustCompile(`(?i)\b(prompt text|display name|model narration|process exit|mutable tag)\s+(is|counts as|proves|establishes)\s+(authentication|authorization|approval|completion)`),
		regexp.MustCompile(`(?i)\bpermissions?\s+(are|is)\s+unioned\b`),
	}
)

type frontmatter struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Version     string              `yaml:"version"`
	Metadata    frontmatterMetadata `yaml:"metadata"`
}

type frontmatterMetadata struct {
	Hermes frontmatterHermes `yaml:"hermes"`
}

type frontmatterHermes struct {
	Tags []string `yaml:"tags"`
}

func Validate(root string) (Manifest, error) {
	manifestPath := filepath.Join(root, ManifestName)
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return Manifest{}, deny("manifest_missing", ManifestName, err.Error())
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, deny("unsafe_file_type", ManifestName, "manifest must be a regular non-symlink file")
	}
	manifestBytes, err := readBounded(manifestPath, MaxManifestBytes)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return Manifest{}, deny("manifest_malformed", ManifestName, err.Error())
	}
	if err := validateManifestShape(manifest); err != nil {
		return Manifest{}, err
	}

	declared := map[string]File{ManifestName: {Path: ManifestName}}
	skillBySlug := make(map[string]Skill, len(manifest.Skills))
	operationOwners := make(map[string]string, len(manifest.Operations))
	for _, owner := range manifest.Operations {
		if previous, exists := operationOwners[owner.Operation]; exists {
			return Manifest{}, deny("duplicate_operation", ManifestName, fmt.Sprintf("operation %q has primary owners %q and %q", owner.Operation, previous, owner.PrimarySkill))
		}
		operationOwners[owner.Operation] = owner.PrimarySkill
	}
	for _, skill := range manifest.Skills {
		if _, exists := skillBySlug[skill.Slug]; exists {
			return Manifest{}, deny("duplicate_skill", ManifestName, skill.Slug)
		}
		skillBySlug[skill.Slug] = skill
		for _, file := range skill.Files {
			full := path.Join(skill.Path, file.Path)
			if _, exists := declared[full]; exists {
				return Manifest{}, deny("duplicate_file", full, "file is declared more than once")
			}
			declared[full] = file
		}
	}
	declared[EvaluationsName] = File{Path: EvaluationsName}
	if err := validateDependencyGraph(skillBySlug); err != nil {
		return Manifest{}, err
	}

	for _, skill := range manifest.Skills {
		if err := validateSkill(root, skill, skillBySlug, operationOwners); err != nil {
			return Manifest{}, err
		}
	}
	if err := validateInventory(root, declared); err != nil {
		return Manifest{}, err
	}
	if _, err := Evaluate(root, manifest); err != nil {
		return Manifest{}, err
	}
	if got := bundleDigest(manifest.Skills); got != manifest.Bundle.ContentDigest {
		return Manifest{}, deny("bundle_digest_mismatch", ManifestName, fmt.Sprintf("declared %s computed %s", manifest.Bundle.ContentDigest, got))
	}
	return manifest, nil
}

func validateDependencyGraph(skills map[string]Skill) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(skills))
	var visit func(string) error
	visit = func(slug string) error {
		state[slug] = visiting
		dependencies := append([]string(nil), skills[slug].Dependencies...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if _, ok := skills[dependency]; !ok {
				return deny("missing_dependency", skills[slug].Path, dependency)
			}
			switch state[dependency] {
			case visiting:
				return deny("dependency_cycle", skills[slug].Path, fmt.Sprintf("dependency edge %q -> %q closes a cycle", slug, dependency))
			case unvisited:
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		state[slug] = visited
		return nil
	}
	slugs := make([]string, 0, len(skills))
	for slug := range skills {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	for _, slug := range slugs {
		if state[slug] == unvisited {
			if err := visit(slug); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateManifestShape(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchema {
		return deny("unsupported_schema", ManifestName, fmt.Sprintf("schema_version must be %d", ManifestSchema))
	}
	if manifest.Bundle.Name != "aegis-hermes-skills" || !semverPattern.MatchString(manifest.Bundle.Version) {
		return deny("invalid_bundle_identity", ManifestName, "bundle name or stable version is invalid")
	}
	if manifest.Bundle.AegisRange == "" || manifest.Bundle.HermesRange != ">=0.18.0,<0.19.0" {
		return deny("invalid_compatibility", ManifestName, "Aegis range must be non-empty and Hermes range must match the supported adapter")
	}
	if !digestPattern.MatchString(manifest.Bundle.ContentDigest) || !revisionPattern.MatchString(manifest.Bundle.SourceRevision) {
		return deny("invalid_provenance", ManifestName, "content digest or source revision is invalid")
	}
	if len(manifest.Skills) == 0 || len(manifest.Operations) == 0 {
		return deny("empty_bundle", ManifestName, "at least one skill and operation are required")
	}
	for _, owner := range manifest.Operations {
		if !operationPattern.MatchString(owner.Operation) || !slugPattern.MatchString(owner.PrimarySkill) {
			return deny("invalid_operation", ManifestName, fmt.Sprintf("invalid operation owner %#v", owner))
		}
		if owner.Availability != "shipped" && owner.Availability != "unavailable" {
			return deny("invalid_availability", ManifestName, owner.Operation)
		}
	}
	return nil
}

func validateSkill(root string, skill Skill, skills map[string]Skill, owners map[string]string) error {
	if !slugPattern.MatchString(skill.Slug) || skill.Path != "skills/"+skill.Slug || !semverPattern.MatchString(skill.Version) {
		return deny("invalid_skill_identity", skill.Path, "slug, path, or version is invalid")
	}
	if strings.TrimSpace(skill.Description) == "" || skill.AuthorityClass != "advisory" {
		return deny("invalid_authority_class", skill.Path, "description is required and authority_class must be advisory")
	}
	if skill.Network != "none" || skill.Filesystem != "none" || len(skill.RequiredToolsets) != 0 || len(skill.Sensitivity) != 0 {
		return deny("undeclared_capability", skill.Path, "official foundation skills require no network, filesystem, toolset, or sensitive-data capability")
	}
	if len(skill.Files) == 0 || !digestPattern.MatchString(skill.ContentDigest) {
		return deny("invalid_skill_digest", skill.Path, "files and content_digest are required")
	}
	for _, dependency := range skill.Dependencies {
		if dependency == skill.Slug {
			return deny("dependency_cycle", skill.Path, "skill depends on itself")
		}
		if _, ok := skills[dependency]; !ok {
			return deny("missing_dependency", skill.Path, dependency)
		}
	}
	owned := map[string]bool{}
	for operation, primary := range owners {
		if primary == skill.Slug {
			owned[operation] = true
		}
	}
	for _, operation := range skill.RequiredOperations {
		if !owned[operation] {
			return deny("orphan_operation", skill.Path, fmt.Sprintf("%q is not owned by this skill", operation))
		}
		delete(owned, operation)
	}
	if len(owned) != 0 {
		keys := make([]string, 0, len(owned))
		for operation := range owned {
			keys = append(keys, operation)
		}
		sort.Strings(keys)
		return deny("orphan_operation", skill.Path, "primary operations absent from required_operations: "+strings.Join(keys, ", "))
	}

	seen := map[string]bool{}
	var digestFiles []File
	for _, file := range skill.Files {
		if !safeRelative(file.Path) || seen[file.Path] || !digestPattern.MatchString(file.SHA256) || file.Size < 0 || file.Size > MaxSkillFileBytes {
			return deny("invalid_file_declaration", path.Join(skill.Path, file.Path), "path, digest, size, or uniqueness is invalid")
		}
		seen[file.Path] = true
		fullPath := filepath.Join(root, filepath.FromSlash(path.Join(skill.Path, file.Path)))
		info, err := os.Lstat(fullPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return deny("unsafe_file_type", path.Join(skill.Path, file.Path), "declared file must be a regular non-symlink file")
		}
		if info.Mode().Perm()&0o111 != 0 {
			return deny("unexpected_executable", path.Join(skill.Path, file.Path), "skill content must not be executable")
		}
		content, err := readBounded(fullPath, MaxSkillFileBytes)
		if err != nil {
			return err
		}
		got := sha256Digest(content)
		if got != file.SHA256 || int64(len(content)) != file.Size {
			return deny("file_digest_mismatch", path.Join(skill.Path, file.Path), fmt.Sprintf("declared %s/%d computed %s/%d", file.SHA256, file.Size, got, len(content)))
		}
		if err := scanContent(path.Join(skill.Path, file.Path), content); err != nil {
			return err
		}
		digestFiles = append(digestFiles, file)
	}
	if !seen["SKILL.md"] {
		return deny("skill_missing", skill.Path, "SKILL.md is required")
	}
	if got := fileSetDigest(digestFiles); got != skill.ContentDigest {
		return deny("skill_digest_mismatch", skill.Path, fmt.Sprintf("declared %s computed %s", skill.ContentDigest, got))
	}
	content, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(skill.Path), "SKILL.md"))
	fm, err := parseFrontmatter(content)
	if err != nil {
		return deny("frontmatter_malformed", path.Join(skill.Path, "SKILL.md"), err.Error())
	}
	if fm.Name != skill.Slug || fm.Version != skill.Version || fm.Description != skill.Description {
		return deny("frontmatter_mismatch", path.Join(skill.Path, "SKILL.md"), "name, version, and description must exactly match the manifest")
	}
	if len(fm.Metadata.Hermes.Tags) == 0 {
		return deny("hermes_metadata_missing", path.Join(skill.Path, "SKILL.md"), "metadata.hermes.tags is required")
	}
	return nil
}

func validateInventory(root string, declared map[string]File) error {
	inventoryRoot := filepath.Join(root, "skills")
	return filepath.WalkDir(inventoryRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return deny("inventory_unreadable", current, walkErr.Error())
		}
		if current == inventoryRoot {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(current)
		if err != nil {
			return deny("inventory_unreadable", rel, err.Error())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return deny("symlink", rel, "symlinks are forbidden")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return deny("unsafe_file_type", rel, "only directories and regular files are allowed")
		}
		if _, ok := declared[rel]; !ok {
			return deny("undeclared_file", rel, "file is absent from the manifest inventory")
		}
		return nil
	})
}

func scanContent(file string, content []byte) error {
	if bytes.IndexByte(content, 0) >= 0 || !bytes.Equal(bytes.ToValidUTF8(content, nil), content) {
		return deny("non_text_content", file, "skill content must be UTF-8 text")
	}
	text := string(content)
	if remoteActivePattern.MatchString(text) {
		return deny("remote_active_content", file, "remote or inline active content is forbidden")
	}
	if inlineNetworkPattern.MatchString(text) {
		return deny("inline_network", file, "inline network behavior is forbidden")
	}
	if strings.Contains(text, "```sh") || strings.Contains(text, "```bash") || strings.Contains(text, "```shell") {
		return deny("inline_shell", file, "executable shell blocks are forbidden")
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(text) {
			return deny("secret_literal", file, "secret-shaped content is forbidden")
		}
	}
	for _, pattern := range positiveAuthorityPatterns {
		if pattern.MatchString(text) {
			return deny("prompt_authority_claim", file, "skills may not claim authoritative effects")
		}
	}
	return nil
}

func parseFrontmatter(content []byte) (frontmatter, error) {
	var value frontmatter
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return value, errors.New("frontmatter must begin with ---")
	}
	end := bytes.Index(content[4:], []byte("\n---\n"))
	if end < 0 {
		return value, errors.New("frontmatter closing delimiter is missing")
	}
	front := content[4 : 4+end]
	decoder := yaml.NewDecoder(bytes.NewReader(front))
	decoder.KnownFields(true)
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return value, errors.New("frontmatter must contain exactly one YAML document")
	}
	return value, nil
}

func decodeStrictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing JSON data is forbidden")
	}
	return nil
}

func readBounded(file string, limit int64) ([]byte, error) {
	opened, err := os.Open(file)
	if err != nil {
		return nil, deny("file_unreadable", file, err.Error())
	}
	defer opened.Close()
	data, err := io.ReadAll(io.LimitReader(opened, limit+1))
	if err != nil {
		return nil, deny("file_unreadable", file, err.Error())
	}
	if int64(len(data)) > limit {
		return nil, deny("file_too_large", file, fmt.Sprintf("limit is %d bytes", limit))
	}
	return data, nil
}

func safeRelative(value string) bool {
	return value != "" && value == path.Clean(value) && !strings.HasPrefix(value, "/") && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "\\")
}

func sha256Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fileSetDigest(files []File) string {
	copyFiles := append([]File(nil), files...)
	sort.Slice(copyFiles, func(i, j int) bool { return copyFiles[i].Path < copyFiles[j].Path })
	hash := sha256.New()
	for _, file := range copyFiles {
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00", file.Path, file.SHA256, file.Size)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func bundleDigest(skills []Skill) string {
	copySkills := append([]Skill(nil), skills...)
	sort.Slice(copySkills, func(i, j int) bool { return copySkills[i].Slug < copySkills[j].Slug })
	hash := sha256.New()
	for _, skill := range copySkills {
		fmt.Fprintf(hash, "%s\x00%s\x00", skill.Slug, skill.ContentDigest)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
