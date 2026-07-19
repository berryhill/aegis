//go:build linux

// Package migration implements authenticated migration from exact legacy local
// defaults to the canonical per-operator layout.
package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/layout"
	"github.com/berryhill/aegis/internal/safefs"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const Confirmation = "migrate aegis to ~/.argis"

type Identity struct {
	Device, Inode uint64
	Mode          uint32
	UID, GID      uint32
	Links         uint64
	Size          int64
	ModTime       int64
}
type Artifact struct {
	Source, Destination, Kind string
	identity                  Identity
}
type Plan struct {
	SourceConfig      string     `json:"source_config"`
	SourceState       string     `json:"source_state"`
	SourceCheckpoints string     `json:"source_checkpoints"`
	DestinationRoot   string     `json:"destination_root"`
	DestinationConfig string     `json:"destination_config"`
	DestinationState  string     `json:"destination_state"`
	Artifacts         []Artifact `json:"artifacts"`
	Preserved         []string   `json:"preserve"`
	Confirmation      string     `json:"confirmation"`
	PrincipalUID      string     `json:"principal_uid"`
	PrincipalUser     string     `json:"principal_user"`
	document          []byte
}
type Service struct {
	Home     func() (string, error)
	Current  func() (*user.User, error)
	LookupID func(string) (*user.User, error)
}

func New() *Service {
	return &Service{Home: os.UserHomeDir, Current: user.Current, LookupID: user.LookupId}
}

func (s *Service) Plan(ctx context.Context) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	principal, err := s.authenticate()
	if err != nil {
		return Plan{}, err
	}
	home, err := s.Home()
	if err != nil {
		return Plan{}, err
	}
	resolved, err := (layout.Resolver{Home: func() (string, error) { return home, nil }, EUID: os.Geteuid}).Resolve()
	if err != nil {
		return Plan{}, err
	}
	if _, collisionErr := os.Lstat(resolved.Root); collisionErr == nil {
		return Plan{}, fmt.Errorf("canonical destination already exists: %s", resolved.Root)
	} else if !errors.Is(collisionErr, os.ErrNotExist) {
		return Plan{}, fmt.Errorf("inspect canonical destination: %w", collisionErr)
	}
	discovery, err := resolved.Discover()
	if err != nil {
		return Plan{}, err
	}
	if discovery.Presence != layout.Legacy {
		return Plan{}, fmt.Errorf("migration requires legacy-only layout; discovered %s", discovery.Presence)
	}
	ci := config.Inspect(resolved.LegacyConfig)
	if ci.State != config.StateValid || !ci.FilePresent {
		return Plan{}, errors.New("legacy migration requires one secure valid legacy configuration")
	}
	if err = validateConfigTraversal(resolved.Home, resolved.LegacyConfig); err != nil {
		return Plan{}, err
	}
	if ci.Config.Principal.UID != principal.Uid || ci.Config.Principal.User != principal.Username {
		return Plan{}, errors.New("legacy principal does not match authenticated OS identity")
	}
	checkpoint := ci.Config.Audit.CheckpointDir
	if ci.Config.StateDir != resolved.LegacyState || (checkpoint != resolved.LegacyCheckpoints && checkpoint != filepath.Join(resolved.LegacyState, "audit-checkpoints")) {
		return Plan{}, errors.New("legacy configuration uses paths outside exact recognized legacy defaults")
	}
	if err = verifyAuthority(ctx, ci.Config, ci.Config.Credentials.Authority.Database, ci.Config.Credentials.Authority.KEKFile); err != nil {
		return Plan{}, fmt.Errorf("verify legacy credential authority before migration: %w", err)
	}
	document, err := rewriteConfig(resolved)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{SourceConfig: resolved.LegacyConfig, SourceState: resolved.LegacyState, SourceCheckpoints: checkpoint, DestinationRoot: resolved.Root, DestinationConfig: resolved.Config, DestinationState: resolved.State, Confirmation: Confirmation, PrincipalUID: principal.Uid, PrincipalUser: principal.Username, document: document, Preserved: []string{"Hermes installation and normal profiles", "operator-managed Ollama daemons and external stores", "systemd credentials, external TLS and external credentials", "Aegis executable and source checkout"}}
	configID, err := inspectRegular(resolved.LegacyConfig)
	if err != nil {
		return Plan{}, err
	}
	plan.Artifacts = append(plan.Artifacts, Artifact{Source: resolved.LegacyConfig, Destination: resolved.Config, Kind: "configuration", identity: configID})
	stateArtifacts, err := inventory(ctx, resolved.LegacyState, resolved.State, true)
	if err != nil {
		return Plan{}, err
	}
	plan.Artifacts = append(plan.Artifacts, stateArtifacts...)
	if checkpoint == resolved.LegacyCheckpoints {
		checkpointArtifacts, inventoryErr := inventory(ctx, checkpoint, resolved.AuditCheckpoints, false)
		if inventoryErr != nil {
			return Plan{}, inventoryErr
		}
		plan.Artifacts = append(plan.Artifacts, checkpointArtifacts...)
	}
	sort.Slice(plan.Artifacts, func(i, j int) bool { return plan.Artifacts[i].Source < plan.Artifacts[j].Source })
	sort.Strings(plan.Preserved)
	return plan, nil
}

func rewriteConfig(l layout.Layout) ([]byte, error) {
	data, err := os.ReadFile(l.LegacyConfig)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) != 1 {
		return nil, errors.New("legacy YAML cannot be safely rewritten")
	}
	for _, keys := range [][]string{{"state_dir"}, {"audit", "checkpoint_dir"}, {"credentials", "authority", "database"}, {"credentials", "authority", "kek_file"}, {"credentials", "authority", "broker", "socket"}, {"manager", "inference", "certification"}, {"api", "unix_socket"}} {
		node := scalarAt(doc.Content[0], keys...)
		if node == nil || node.Value == "" {
			continue
		}
		switch {
		case node.Value == l.LegacyState:
			node.Value = l.State
		case node.Value == l.LegacyCheckpoints || node.Value == filepath.Join(l.LegacyState, "audit-checkpoints"):
			node.Value = l.AuditCheckpoints
		case within(l.LegacyState, node.Value):
			rel, _ := filepath.Rel(l.LegacyState, node.Value)
			node.Value = filepath.Join(l.State, rel)
		}
	}
	var output strings.Builder
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	err = encoder.Encode(&doc)
	_ = encoder.Close()
	if err != nil {
		return nil, err
	}
	return []byte(output.String()), nil
}

func scalarAt(node *yaml.Node, keys ...string) *yaml.Node {
	current := node
	for _, key := range keys {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for index := 0; index+1 < len(current.Content); index += 2 {
			if current.Content[index].Value == key {
				next = current.Content[index+1]
				break
			}
		}
		current = next
	}
	if current == nil || current.Kind != yaml.ScalarNode {
		return nil
	}
	return current
}

func validateConfigTraversal(home, path string) error {
	relative, err := filepath.Rel(home, filepath.Dir(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("legacy config escaped authenticated home")
	}
	current := home
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return statErr
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || int(stat.Uid) != os.Geteuid() || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("legacy configuration parent is unsafe: %s", current)
		}
	}
	return nil
}

func inventory(ctx context.Context, source, destination string, state bool) ([]Artifact, error) {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	id, err := inspectDirectoryInfo(info)
	if err != nil {
		return nil, err
	}
	artifacts := []Artifact{{Source: source, Destination: destination, Kind: "directory", identity: id}}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == source {
			return nil
		}
		rel, _ := filepath.Rel(source, path)
		parts := strings.Split(rel, string(filepath.Separator))
		if state && len(parts) == 1 && !allowedStateTop(parts[0]) {
			return fmt.Errorf("unknown legacy state artifact %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var identity Identity
		kind := "file"
		if info.IsDir() {
			identity, err = inspectDirectoryInfo(info)
			kind = "directory"
		} else {
			identity, err = inspectRegularInfo(info)
		}
		if err != nil {
			return fmt.Errorf("unsafe legacy artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, Artifact{Source: path, Destination: filepath.Join(destination, rel), Kind: kind, identity: identity})
		return nil
	})
	return artifacts, err
}
func allowedStateTop(name string) bool {
	switch name {
	case ".lock", "audit.jsonl", "plans", "approvals", "receipts", "mandates", "sessions", "charters", "provisioned", "runtime", "manager", "audit-checkpoints", "credentials":
		return true
	}
	return strings.HasPrefix(name, ".aegis-")
}

func (s *Service) Apply(ctx context.Context, plan Plan) error {
	principal, err := s.authenticate()
	if err != nil {
		return err
	}
	if principal.Uid != plan.PrincipalUID || principal.Username != plan.PrincipalUser {
		return errors.New("authenticated identity changed after migration preview")
	}
	current, err := s.Plan(ctx)
	if err != nil {
		return fmt.Errorf("revalidate migration plan: %w", err)
	}
	if Digest(current) != Digest(plan) {
		return errors.New("migration plan changed; no mutation performed")
	}
	staging := plan.DestinationRoot + ".migration-" + strings.TrimPrefix(Digest(plan), "sha256:")[:12]
	if _, err = os.Lstat(staging); err == nil {
		return fmt.Errorf("interrupted migration staging exists at %s; verify and remove it before retrying", staging)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Mkdir(staging, 0700); err != nil {
		return fmt.Errorf("create migration staging: %w", err)
	}
	stagingInfo, err := os.Lstat(staging)
	if err != nil {
		return err
	}
	stagingStat := stagingInfo.Sys().(*syscall.Stat_t)
	homeInfo, err := os.Lstat(filepath.Dir(staging))
	if err != nil {
		return err
	}
	homeStat := homeInfo.Sys().(*syscall.Stat_t)
	published := false
	defer func() {
		if !published {
			_ = safefs.EmptyDirectoryIdentity(staging, uint64(stagingStat.Dev), stagingStat.Ino)
			_ = safefs.RemoveRelativeIdentity(filepath.Dir(staging), filepath.Base(staging), true, uint64(homeStat.Dev), homeStat.Ino)
		}
	}()
	if err = os.Mkdir(filepath.Join(staging, "state"), 0700); err != nil {
		return err
	}
	if exists(plan.SourceState) {
		identity := plannedIdentity(plan, plan.SourceState)
		if err = copyTree(plan.SourceState, filepath.Join(staging, "state"), identity); err != nil {
			return fmt.Errorf("copy legacy state: %w", err)
		}
	}
	if plan.SourceCheckpoints != filepath.Join(plan.SourceState, "audit-checkpoints") && exists(plan.SourceCheckpoints) {
		if err = os.Mkdir(filepath.Join(staging, "state", "audit-checkpoints"), 0700); err != nil {
			return fmt.Errorf("create canonical checkpoint directory: %w", err)
		}
		identity := plannedIdentity(plan, plan.SourceCheckpoints)
		if err = copyTree(plan.SourceCheckpoints, filepath.Join(staging, "state", "audit-checkpoints"), identity); err != nil {
			return fmt.Errorf("copy legacy checkpoints: %w", err)
		}
	}
	if err = os.WriteFile(filepath.Join(staging, "aegis.yaml"), plan.document, 0600); err != nil {
		return err
	}
	if err = verifySourceInventory(ctx, plan); err != nil {
		return fmt.Errorf("legacy source changed during verified copy; no canonical root published: %w", err)
	}
	if err = syncTree(staging); err != nil {
		return fmt.Errorf("sync migration staging: %w", err)
	}
	stagedConfig, err := config.Load(filepath.Join(staging, "aegis.yaml"), nil)
	if err != nil {
		return fmt.Errorf("verify migrated configuration: %w", err)
	}
	stagedDatabase := stagedConfig.Credentials.Authority.Database
	stagedKEK := stagedConfig.Credentials.Authority.KEKFile
	if within(plan.DestinationState, stagedDatabase) {
		relative, _ := filepath.Rel(plan.DestinationState, stagedDatabase)
		stagedDatabase = filepath.Join(staging, "state", relative)
	}
	if within(plan.DestinationState, stagedKEK) {
		relative, _ := filepath.Rel(plan.DestinationState, stagedKEK)
		stagedKEK = filepath.Join(staging, "state", relative)
	}
	if err = verifyAuthority(ctx, stagedConfig, stagedDatabase, stagedKEK); err != nil {
		return fmt.Errorf("verify staged credential authority: %w", err)
	}
	if err = os.Rename(staging, plan.DestinationRoot); err != nil {
		return fmt.Errorf("publish canonical root: %w", err)
	}
	published = true
	if err = syncDir(filepath.Dir(plan.DestinationRoot)); err != nil {
		return err
	}
	// Destination is complete before any source cleanup. Revalidate source roots
	// by identity, then use descriptor-anchored deletion under unsafe XDG parents.
	if exists(plan.SourceState) {
		if err = verifyPlannedIdentity(plan, plan.SourceState); err != nil {
			return fmt.Errorf("canonical migration complete; legacy state identity changed and was preserved: %w", err)
		}
		identity := plannedIdentity(plan, plan.SourceState)
		if err = safefs.EmptyDirectoryIdentity(plan.SourceState, identity.Device, identity.Inode); err != nil {
			return fmt.Errorf("canonical migration complete but legacy state cleanup failed: %w", err)
		}
	}
	if plan.SourceCheckpoints != filepath.Join(plan.SourceState, "audit-checkpoints") && exists(plan.SourceCheckpoints) {
		if err = verifyPlannedIdentity(plan, plan.SourceCheckpoints); err != nil {
			return fmt.Errorf("canonical migration complete; legacy checkpoint identity changed and was preserved: %w", err)
		}
		identity := plannedIdentity(plan, plan.SourceCheckpoints)
		if err = safefs.EmptyDirectoryIdentity(plan.SourceCheckpoints, identity.Device, identity.Inode); err != nil {
			return fmt.Errorf("canonical migration complete but legacy checkpoint cleanup failed: %w", err)
		}
	}
	if err = verifyPlannedIdentity(plan, plan.SourceConfig); err != nil {
		return fmt.Errorf("canonical migration complete; legacy configuration identity changed and was preserved: %w", err)
	}
	if err = safefs.RemoveFile(filepath.Dir(plan.SourceConfig), filepath.Base(plan.SourceConfig)); err != nil {
		return fmt.Errorf("canonical migration complete but legacy config cleanup failed: %w", err)
	}
	if inspection := config.Inspect(""); inspection.State != config.StateValid {
		return fmt.Errorf("migrated readiness inspection is %s", inspection.State)
	}
	return nil
}

func verifyPlannedIdentity(plan Plan, path string) error {
	for _, artifact := range plan.Artifacts {
		if artifact.Source != path {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		var got Identity
		if artifact.Kind == "directory" {
			got, err = inspectDirectoryInfo(info)
		} else {
			got, err = inspectRegularInfo(info)
		}
		if err != nil || got != artifact.identity {
			return errors.New("artifact identity differs from the authenticated plan")
		}
		return nil
	}
	return errors.New("artifact is absent from the authenticated plan")
}

func plannedIdentity(plan Plan, path string) Identity {
	for _, artifact := range plan.Artifacts {
		if artifact.Source == path {
			return artifact.identity
		}
	}
	return Identity{}
}

func verifySourceInventory(ctx context.Context, plan Plan) error {
	current := []Artifact{}
	state, err := inventory(ctx, plan.SourceState, plan.DestinationState, true)
	if err != nil {
		return err
	}
	current = append(current, state...)
	if plan.SourceCheckpoints != filepath.Join(plan.SourceState, "audit-checkpoints") {
		checkpoints, inventoryErr := inventory(ctx, plan.SourceCheckpoints, filepath.Join(plan.DestinationState, "audit-checkpoints"), false)
		if inventoryErr != nil {
			return inventoryErr
		}
		current = append(current, checkpoints...)
	}
	configIdentity, err := inspectRegular(plan.SourceConfig)
	if err != nil {
		return err
	}
	current = append(current, Artifact{Source: plan.SourceConfig, Destination: plan.DestinationConfig, Kind: "configuration", identity: configIdentity})
	expected := make(map[string]Artifact, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		expected[artifact.Source] = artifact
	}
	if len(current) != len(expected) {
		return errors.New("artifact count changed")
	}
	for _, artifact := range current {
		planned, ok := expected[artifact.Source]
		if !ok || planned.Destination != artifact.Destination || planned.Kind != artifact.Kind || planned.identity != artifact.identity {
			return fmt.Errorf("artifact changed: %s", artifact.Source)
		}
	}
	return nil
}

func copyTree(source, destination string, expected Identity) error {
	sfd, err := unix.Open(source, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(sfd)
	var sourceStat unix.Stat_t
	if err = unix.Fstat(sfd, &sourceStat); err != nil || uint64(sourceStat.Dev) != expected.Device || sourceStat.Ino != expected.Inode {
		return errors.New("source root identity changed before copy")
	}
	dfd, err := unix.Open(destination, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dfd)
	return copyDir(sfd, dfd)
}
func copyDir(source, destination int) error {
	dup, err := unix.Dup(source)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(dup), "source")
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		var st unix.Stat_t
		if err = unix.Fstatat(source, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			if err = unix.Mkdirat(destination, name, 0700); err != nil {
				return err
			}
			s, er := unix.Openat(source, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if er != nil {
				return er
			}
			d, er := unix.Openat(destination, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if er == nil {
				er = copyDir(s, d)
			}
			_ = unix.Close(s)
			_ = unix.Close(d)
			if er != nil {
				return er
			}
		case unix.S_IFREG:
			if st.Nlink != 1 {
				return errors.New("hard-linked source appeared during copy")
			}
			s, er := unix.Openat(source, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if er != nil {
				return er
			}
			d, er := unix.Openat(destination, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
			if er == nil {
				sf := os.NewFile(uintptr(s), name)
				df := os.NewFile(uintptr(d), name)
				_, er = io.Copy(df, sf)
				if er == nil {
					er = df.Sync()
				}
				_ = sf.Close()
				_ = df.Close()
			} else {
				_ = unix.Close(s)
			}
			if er != nil {
				return er
			}
		default:
			return errors.New("non-regular source appeared during copy")
		}
	}
	return unix.Fsync(destination)
}
func syncTree(path string) error { return syncDir(path) }
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func verifyAuthority(ctx context.Context, cfg config.Config, database, hostKEK string) error {
	authority := cfg.Credentials.Authority
	if authority.Database == "" && authority.DeploymentID == "" && authority.Custody == "" {
		return nil
	}
	if database == "" || authority.DeploymentID == "" {
		return errors.New("credential authority configuration is incomplete")
	}
	credentialPath := hostKEK
	switch authority.Custody {
	case "host-file":
		if credentialPath == "" {
			return errors.New("host-file credential authority lacks a KEK path")
		}
	case "systemd":
		directory := os.Getenv("CREDENTIALS_DIRECTORY")
		if directory == "" {
			return errors.New("systemd credential directory is unavailable; migration cannot prove authority linkage")
		}
		credentialPath = filepath.Join(directory, authority.KEKCredential)
	default:
		return errors.New("unsupported credential custody")
	}
	custodian, err := credentials.LoadFileCustodian(credentialPath)
	if err != nil {
		return fmt.Errorf("load credential custody without exposing key material: %w", err)
	}
	defer custodian.Close()
	return credentialbolt.Inspect(ctx, database, authority.DeploymentID, custodian)
}

func (s *Service) authenticate() (*user.User, error) {
	u, e := s.Current()
	if e != nil {
		return nil, e
	}
	if _, e = strconv.ParseUint(u.Uid, 10, 32); e != nil {
		return nil, errors.New("non-numeric principal UID")
	}
	v, e := s.LookupID(u.Uid)
	if e != nil || v.Uid != u.Uid || v.Username != u.Username {
		return nil, errors.New("ambiguous OS principal")
	}
	return u, nil
}
func inspectRegular(path string) (Identity, error) {
	i, e := os.Lstat(path)
	if e != nil {
		return Identity{}, e
	}
	return inspectRegularInfo(i)
}
func inspectRegularInfo(i os.FileInfo) (Identity, error) {
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.Mode().IsRegular() || i.Mode()&os.ModeSymlink != 0 || s.Nlink != 1 || int(s.Uid) != os.Geteuid() || i.Mode().Perm()&0077 != 0 {
		return Identity{}, errors.New("regular file must be unique, operator-owned, and mode 0600")
	}
	return Identity{uint64(s.Dev), s.Ino, uint32(i.Mode()), s.Uid, s.Gid, uint64(s.Nlink), i.Size(), i.ModTime().UnixNano()}, nil
}
func inspectDirectoryInfo(i os.FileInfo) (Identity, error) {
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 || int(s.Uid) != os.Geteuid() || i.Mode().Perm()&0077 != 0 {
		return Identity{}, errors.New("directory must be operator-owned and mode 0700")
	}
	return Identity{Device: uint64(s.Dev), Inode: s.Ino, Mode: uint32(i.Mode()), UID: s.Uid, GID: s.Gid}, nil
}
func within(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func exists(path string) bool { _, e := os.Lstat(path); return e == nil }
func Digest(p Plan) string {
	h := sha256.New()
	documentDigest := sha256.Sum256(p.document)
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%x", p.PrincipalUID, p.PrincipalUser, p.SourceConfig, p.SourceState, p.SourceCheckpoints, p.DestinationRoot, p.DestinationState, documentDigest)
	for _, a := range p.Artifacts {
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d", a.Source, a.Destination, a.Kind, a.identity.Device, a.identity.Inode, a.identity.Mode, a.identity.UID, a.identity.GID, a.identity.Links, a.identity.Size, a.identity.ModTime)
	}
	for _, preserved := range p.Preserved {
		fmt.Fprintf(h, "\x00preserve\x00%s", preserved)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
