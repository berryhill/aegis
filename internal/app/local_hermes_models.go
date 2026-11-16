package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/core"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const localHermesModelImportPrefix = "Imported local Hermes model snapshot ("

// LocalHermesModelSnapshot contains only reviewed configuration, not a connection,
// account, endpoint, credential source, or transport qualification assertion.
type LocalHermesModelSnapshot struct {
	DefaultProvider string            `json:"default_provider"`
	DefaultModel    string            `json:"default_model"`
	Lanes           []core.HermesLane `json:"lanes"`
	Authentication  string            `json:"authentication"`
	Qualification   string            `json:"qualification"`
}

var errLocalHermesModels = errors.New("local Hermes model snapshot unavailable: unsafe, ambiguous or unsupported configuration")
var localHermesModelID = regexp.MustCompile(`^(gpt-[0-9]|o[134]([.-]|$)|codex-|gemini-[0-9]|claude-(opus|sonnet|haiku)-)[a-z0-9._-]*$`)

func validLocalHermesModel(provider, model string) bool {
	if provider != "codex" && provider != "openai-codex" && provider != "antigravity-oauth" {
		return false
	}
	if len(model) > 96 || !localHermesModelID.MatchString(model) || strings.Contains(model, "..") {
		return false
	}
	for _, secret := range []string{"secret", "token", "password", "credential", "sk-", "eyj"} {
		if strings.Contains(model, secret) {
			return false
		}
	}
	return true
}

// No YAML decoding into arbitrary Go objects, merges, aliases, interpolation or
// parser diagnostics containing source bytes. Duplicate keys deny even in ignored
// sections. The byte, depth and node budgets bound parsing and subsequent walking.
func validateLocalHermesYAML(n *yaml.Node, depth int, remaining *int) bool {
	*remaining--
	if depth > 32 || *remaining < 0 || n.Alias != nil || n.Anchor != "" || n.Kind == yaml.AliasNode {
		return false
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return false
		}
	case yaml.MappingNode:
		if n.Tag != "!!map" || len(n.Content)%2 != 0 {
			return false
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
				return false
			}
			seen[key.Value] = true
		}
	case yaml.SequenceNode:
		if n.Tag != "!!seq" {
			return false
		}
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str", "!!bool", "!!int", "!!float", "!!null", "!!timestamp":
		default:
			return false
		}
	default:
		return false
	}
	for _, child := range n.Content {
		if !validateLocalHermesYAML(child, depth+1, remaining) {
			return false
		}
	}
	return true
}

func localHermesYAMLField(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func localHermesYAMLString(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return ""
	}
	return n.Value
}

func parseLocalHermesModels(data []byte) (core.HermesConfig, error) {
	var document, extra yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if len(data) > maximumLocalHermesMarkerBytes || decoder.Decode(&document) != nil || decoder.Decode(&extra) != io.EOF {
		return core.HermesConfig{}, errLocalHermesModels
	}
	budget := 8192
	if !validateLocalHermesYAML(&document, 0, &budget) || document.Content[0].Kind != yaml.MappingNode {
		return core.HermesConfig{}, errLocalHermesModels
	}
	root := document.Content[0]
	model := localHermesYAMLField(root, "model")
	provider := localHermesYAMLString(localHermesYAMLField(model, "provider"))
	name := localHermesYAMLString(localHermesYAMLField(model, "default"))
	if !validLocalHermesModel(provider, name) {
		return core.HermesConfig{}, errLocalHermesModels
	}
	h := core.HermesConfig{Provider: provider, Model: name, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}}
	h.AllowedLanes = []core.HermesLane{{Provider: provider, Model: name, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}}}
	fallbacks := localHermesYAMLField(root, "fallback_providers")
	legacy := localHermesYAMLField(root, "fallback_model")
	if fallbacks != nil && legacy != nil {
		return core.HermesConfig{}, errLocalHermesModels
	}
	var entries []*yaml.Node
	if fallbacks != nil {
		if fallbacks.Kind != yaml.SequenceNode || len(fallbacks.Content) > 15 {
			return core.HermesConfig{}, errLocalHermesModels
		}
		entries = fallbacks.Content
	} else if legacy != nil {
		entries = []*yaml.Node{legacy}
	}
	seen := map[string]bool{provider + "\x00" + name: true}
	for _, entry := range entries {
		p := localHermesYAMLString(localHermesYAMLField(entry, "provider"))
		m := localHermesYAMLString(localHermesYAMLField(entry, "model"))
		if !validLocalHermesModel(p, m) || seen[p+"\x00"+m] {
			return core.HermesConfig{}, errLocalHermesModels
		}
		seen[p+"\x00"+m] = true
		h.AllowedLanes = append(h.AllowedLanes, core.HermesLane{Provider: p, Model: m, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}})
	}
	return h, nil
}

// Read only the fixed owner-verified config marker after explicit with-models
// consent. Never open auth.json, .env, plugins, another profile or an endpoint.
func inspectLocalHermesModels(root, principalUID string) (localHermesProfileEvidence, core.HermesConfig, error) {
	fail := func() (localHermesProfileEvidence, core.HermesConfig, error) {
		return localHermesProfileEvidence{}, core.HermesConfig{}, errLocalHermesModels
	}
	uid, err := strconv.ParseUint(principalUID, 10, 32)
	if err != nil {
		return fail()
	}
	fd, err := unix.Open(root, localHermesDirectoryOpenFlags(), 0)
	if err != nil {
		return fail()
	}
	dir := os.NewFile(uintptr(fd), "profile/default")
	defer dir.Close()
	if verifyLocalHermesFile(dir, uint32(uid), true) != nil {
		return fail()
	}
	dirInfo, err := dir.Stat()
	if err != nil {
		return fail()
	}
	mfd, err := unix.Openat(fd, "config.yaml", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fail()
	}
	file := os.NewFile(uintptr(mfd), "config.yaml")
	defer file.Close()
	if verifyLocalHermesFile(file, uint32(uid), false) != nil {
		return fail()
	}
	before, err := file.Stat()
	if err != nil || before.Size() <= 0 || before.Size() > maximumLocalHermesMarkerBytes {
		return fail()
	}
	var stat unix.Stat_t
	if unix.Fstat(mfd, &stat) != nil || stat.Nlink != 1 {
		return fail()
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumLocalHermesMarkerBytes+1))
	defer clear(data)
	if err != nil || int64(len(data)) != before.Size() {
		return fail()
	}
	h, err := parseLocalHermesModels(data)
	if err != nil {
		return fail()
	}
	var post unix.Stat_t
	if unix.Fstat(mfd, &post) != nil || stat.Dev != post.Dev || stat.Ino != post.Ino || stat.Size != post.Size || stat.Mode != post.Mode || stat.Uid != post.Uid || stat.Gid != post.Gid || stat.Nlink != post.Nlink || stat.Mtim != post.Mtim || stat.Ctim != post.Ctim {
		return fail()
	}
	var linked unix.Stat_t
	if unix.Fstatat(fd, "config.yaml", &linked, unix.AT_SYMLINK_NOFOLLOW) != nil || uint64(linked.Dev) != uint64(post.Dev) || linked.Ino != post.Ino || linked.Mtim.Sec != post.Mtim.Sec || linked.Mtim.Nsec != post.Mtim.Nsec || linked.Ctim.Sec != post.Ctim.Sec || linked.Ctim.Nsec != post.Ctim.Nsec {
		return fail()
	}
	currentDir, err := os.Lstat(root)
	if err != nil || !os.SameFile(dirInfo, currentDir) || verifyLocalHermesFile(dir, uint32(uid), true) != nil || verifyLocalHermesFile(file, uint32(uid), false) != nil {
		return fail()
	}
	// Hash sanitized selections and stat provenance, never secret-bearing bytes.
	metadata, _ := json.Marshal(h)
	provenance := fmt.Sprintf("aegis.local-hermes-models.v1\x00%d:%d:%d:%d:%d:%d:%d:%d:%d:%d\x00", stat.Dev, stat.Ino, stat.Uid, stat.Gid, stat.Mode, stat.Size, stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec)
	digest := sha256.Sum256(append([]byte(provenance), metadata...))
	return localHermesProfileEvidence{Fingerprint: "sha256:" + hex.EncodeToString(digest[:])}, h, nil
}

func canonicalLocalHermesModelCharter(agentID, principalID, fingerprint string, h core.HermesConfig) (core.CanonicalCharter, error) {
	// Reconstruct the closed metadata shape; reject injected connection/grants on
	// durable replay instead of trusting that a lookalike charter was an import.
	expected := core.HermesConfig{Provider: h.Provider, Model: h.Model, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}}
	if !validLocalHermesModel(h.Provider, h.Model) || len(h.AllowedLanes) == 0 || len(h.AllowedLanes) > 16 {
		return core.CanonicalCharter{}, errLocalHermesModels
	}
	for _, lane := range h.AllowedLanes {
		if !validLocalHermesModel(lane.Provider, lane.Model) {
			return core.CanonicalCharter{}, errLocalHermesModels
		}
		expected.AllowedLanes = append(expected.AllowedLanes, core.HermesLane{Provider: lane.Provider, Model: lane.Model, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ConfiguredProviderV1}})
	}
	if !reflect.DeepEqual(h, expected) {
		return core.CanonicalCharter{}, errLocalHermesModels
	}
	base, err := canonicalLocalHermesImportCharter(agentID, principalID, fingerprint)
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	c := base.Charter
	c.Name = localHermesModelImportPrefix + fingerprint + ")"
	c.Stanzas[0].Hermes = expected
	return core.Canonicalize(c)
}

// PrepareLocalHermesModelImportAs is explicit consent to read bounded config
// model metadata; it does not authorize registration or provider connection.
func (s *Service) PrepareLocalHermesModelImportAs(ctx context.Context, subject core.Subject) (LocalHermesAgentImportProposal, error) {
	proposal, _, _, err := s.localHermesImportArtifacts(ctx, subject, localHermesImportUnixPeer, true)
	return proposal, err
}

func (s *Service) ConfirmLocalHermesModelImportAs(ctx context.Context, subject core.Subject, expectedDigest string) (FleetAgent, bool, error) {
	return s.confirmLocalHermesAgentImportAs(ctx, subject, expectedDigest, localHermesImportUnixPeer, true)
}
