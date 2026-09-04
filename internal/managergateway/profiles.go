package managergateway

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"golang.org/x/sys/unix"
)

const (
	TurnOriginModel         = "model_untrusted"
	TurnOriginAuthoritative = "aegis_authoritative"
	maxHermesNamedProfiles  = 256
)

type TurnResult struct {
	Kind      string         `json:"kind"`
	Origin    string         `json:"origin"`
	Message   string         `json:"message"`
	Sensitive bool           `json:"sensitive,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type HermesProfileDescriptor struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func localHermesHome(principalUser, principalUID string) (string, error) {
	account, err := user.Lookup(principalUser)
	if err != nil {
		return "", fmt.Errorf("resolve configured principal account: %w", err)
	}
	if account.Uid != principalUID || strconv.Itoa(os.Geteuid()) != principalUID {
		return "", errors.New("configured principal does not match the gateway process owner")
	}
	if !filepath.IsAbs(account.HomeDir) {
		return "", errors.New("configured principal home is not absolute")
	}
	return filepath.Join(account.HomeDir, ".hermes"), nil
}

func verifyOpenProfileDirectory(directory *os.File, expectedUID uint32) error {
	info, err := directory.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not an owner-verified directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID {
		return errors.New("directory owner does not match the authenticated principal")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("directory is group- or world-writable")
	}
	return nil
}

func openProfileDirectory(path string, expectedUID uint32) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), path)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open owner-verified directory")
	}
	if err = verifyOpenProfileDirectory(directory, expectedUID); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func openProfileDirectoryAt(parent *os.File, name string, expectedUID uint32) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(fd), name)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open owner-verified directory")
	}
	if err = verifyOpenProfileDirectory(directory, expectedUID); err != nil {
		_ = directory.Close()
		return nil, err
	}
	return directory, nil
}

func discoverHermesProfiles(root, principalUID string) ([]HermesProfileDescriptor, error) {
	uid, err := strconv.ParseUint(principalUID, 10, 32)
	if err != nil {
		return nil, errors.New("configured principal UID is invalid")
	}
	rootDirectory, err := openProfileDirectory(root, uint32(uid))
	if err != nil {
		return nil, fmt.Errorf("default Hermes home unavailable: %w", err)
	}
	defer rootDirectory.Close()

	profiles := []HermesProfileDescriptor{{ID: "default", Kind: "default", Target: "profile/default", Status: "discovered"}}
	namedDirectory, err := openProfileDirectoryAt(rootDirectory, "profiles", uint32(uid))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return profiles, nil
		}
		return nil, fmt.Errorf("named Hermes profile root unavailable: %w", err)
	}
	defer namedDirectory.Close()

	entries, err := namedDirectory.ReadDir(maxHermesNamedProfiles + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read named Hermes profiles: %w", err)
	}
	if len(entries) > maxHermesNamedProfiles {
		return nil, fmt.Errorf("read named Hermes profiles: inventory exceeds limit of %d", maxHermesNamedProfiles)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !safeProfileID(name) {
			continue
		}
		descriptor := HermesProfileDescriptor{ID: name, Kind: "named", Target: "profile/" + name, Status: "discovered"}
		profileDirectory, openErr := openProfileDirectoryAt(namedDirectory, name, uint32(uid))
		if openErr != nil {
			descriptor.Status = "rejected"
			descriptor.Reason = openErr.Error()
		} else {
			_ = profileDirectory.Close()
		}
		profiles = append(profiles, descriptor)
	}
	sort.Slice(profiles[1:], func(i, j int) bool { return profiles[i+1].ID < profiles[j+1].ID })
	return profiles, nil
}

func safeProfileID(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for index, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || index > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return false
	}
	return true
}

func normalizedIntent(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	return strings.Join(strings.FieldsFunc(input, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}), " ")
}

func containsQuotedIntentSyntax(input string) bool {
	runes := []rune(input)
	for index, value := range runes {
		if value == '\'' || value == '’' {
			// Preserve ordinary contractions such as "I'd", "I’d", and
			// "let's" while rejecting single-quoted examples anywhere in
			// the request, including inside punctuation or delimiters.
			if index > 0 && index+1 < len(runes) && unicode.IsLetter(runes[index-1]) && unicode.IsLetter(runes[index+1]) {
				continue
			}
			return true
		}
		if value == '`' || unicode.Is(unicode.Quotation_Mark, value) || unicode.Is(unicode.Ps, value) || unicode.Is(unicode.Pe, value) {
			return true
		}
	}
	return false
}

func localProfileIntent(input string) string {
	if strings.ContainsAny(input, "\r\n") || containsQuotedIntentSyntax(input) {
		return ""
	}
	normalized := normalizedIntent(input)
	for _, prefix := range []string{"do not ", "dont ", "never ", "say ", "quote ", "repeat "} {
		if strings.HasPrefix(normalized, prefix) {
			return ""
		}
	}
	for {
		stripped := normalized
		for _, prefix := range []string{"hey ", "okay ", "ok ", "please "} {
			stripped = strings.TrimPrefix(stripped, prefix)
		}
		if stripped == normalized {
			break
		}
		normalized = stripped
	}
	normalized = strings.TrimPrefix(normalized, "lets ")
	normalized = strings.TrimPrefix(normalized, "let s ")
	for _, phrase := range []string{
		"register the default hermes profile on this computer",
		"import the local hermes default profile as an agent",
	} {
		if normalized == phrase {
			return "register_default"
		}
	}
	if !strings.Contains(normalized, "hermes") || !strings.Contains(normalized, "profile") {
		return ""
	}
	for _, phrase := range []string{"list hermes profiles", "show hermes profiles", "see all the hermes profiles", "see the hermes profiles", "find hermes profiles"} {
		if strings.Contains(normalized, phrase) {
			return "inventory"
		}
	}
	return ""
}

var (
	platformSkillsPattern         = regexp.MustCompile(`^(?:how do i |how can i |can you (?:tell me )?how to |please )?(?:install|add|get|use) (?:the )?aegis skills? (?:in|into) hermes$`)
	platformAgentExpertisePattern = regexp.MustCompile(`^(?:does|do|can) (?:our|the|a|this) (?:register(?:ed|s) )?(?:hermes )?agent (?:know how to use|know|use|have) aegis(?: skills)?$`)
	platformAgentRenamePattern    = regexp.MustCompile(`^(?:and )?(?:can|could|would) you (?:change the name of|change name of|rename) (?:the )?(?:default )?(?:hermes )?agent(?: we registered)? (?:to|as) [a-z0-9][a-z0-9._-]{0,63}$`)
)

func platformGuidanceIntent(input string) string {
	if strings.ContainsAny(input, "\r\n") || containsQuotedIntentSyntax(input) {
		return ""
	}
	normalized := normalizedIntent(input)
	switch {
	case platformSkillsPattern.MatchString(normalized):
		return "skills_install"
	case platformAgentExpertisePattern.MatchString(normalized):
		return "registered_agent_expertise"
	case platformAgentRenamePattern.MatchString(normalized):
		return "agent_rename"
	default:
		return ""
	}
}

type agentRegistryIntent struct {
	kind        string
	agentID     string
	revision    uint64
	hasRevision bool
}

var showAgentPattern = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:show|show me|show details for)\s+(?:registered\s+)?agent\s+([a-z0-9][a-z0-9._-]{0,127})(?:\s+revision\s+([1-9][0-9]*))?\s*[?.!]?\s*$`)

func parseAgentRegistryIntent(input string) agentRegistryIntent {
	if strings.ContainsAny(input, "\r\n") || containsQuotedIntentSyntax(input) {
		return agentRegistryIntent{}
	}
	normalized := normalizedIntent(input)
	for _, phrase := range []string{
		"how many agents have we registered",
		"how many agents are registered",
		"how many agents are resistered",
		"what is the number of registered agents",
		"tell me how many agents are registered",
	} {
		if normalized == phrase {
			return agentRegistryIntent{kind: "count"}
		}
	}
	for _, phrase := range []string{
		"list registered agents",
		"list agents",
		"show me all registered agents",
		"what agents have we registered",
		"which agents are registered",
	} {
		if normalized == phrase {
			return agentRegistryIntent{kind: "list"}
		}
	}
	matches := showAgentPattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return agentRegistryIntent{}
	}
	intent := agentRegistryIntent{kind: "show", agentID: strings.ToLower(matches[1])}
	if matches[2] != "" {
		revision, err := strconv.ParseUint(matches[2], 10, 64)
		if err != nil || revision == 0 {
			return agentRegistryIntent{}
		}
		intent.revision = revision
		intent.hasRevision = true
	}
	return intent
}

func IsAgentRegistryRequest(input string) bool {
	return parseAgentRegistryIntent(input).kind != ""
}

func IsLocalProfileRequest(input string) bool {
	return localProfileIntent(input) != ""
}

func IsPlatformGuidanceRequest(input string) bool {
	return platformGuidanceIntent(input) != ""
}

func renderProfileInventory(profiles []HermesProfileDescriptor) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Inspected %d local Hermes profile(s) for the authenticated operating-system principal:\n", len(profiles))
	for _, profile := range profiles {
		fmt.Fprintf(&builder, "- %s (%s): %s", profile.ID, profile.Kind, profile.Status)
		if profile.Reason != "" {
			fmt.Fprintf(&builder, " — %s", profile.Reason)
		}
		builder.WriteByte('\n')
	}
	builder.WriteString("Discovery was read-only. No profile contents, credentials, memories, sessions, skills, plugins, or MCP configuration were sent to the model.")
	return builder.String()
}
