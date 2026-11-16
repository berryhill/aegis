package hermes

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

//go:embed native_codex_login.py
var nativeCodexLoginProtocol string

// ResolveNativeCodexLogin resolves ONLY an explicitly approved native source's local
// pool through the qualified Hermes native store API. No refresh, fallback,
// rotation, import, network, or source mutation is permitted. The private
// child's bounded stdout is a controller pipe, never command output or a log.
func (a *Adapter) ResolveNativeCodexLogin(ctx context.Context, provider, profileHome, accountID string, now time.Time, credentialID ...string) (ProviderAccessToken, error) {
	if a == nil || ctx == nil || ctx.Err() != nil || (provider != "codex" && provider != "openai-codex") || accountID == "" {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	d, err := a.Discover(ctx)
	if err != nil {
		return ProviderAccessToken{}, ErrProviderAuthRuntimeUnqualified
	}
	if err = a.qualifyProviderDescriptor(d); err != nil {
		return ProviderAccessToken{}, err
	}
	return resolveNativeCodexLoginChecked(ctx, d, provider, profileHome, accountID, now, func() error { return a.qualifyProviderDescriptor(d) }, credentialID...)
}

func resolveNativeCodexLogin(ctx context.Context, d core.RuntimeDescriptor, provider, profileHome, accountID string, now time.Time, credentialID ...string) (ProviderAccessToken, error) {
	return resolveNativeCodexLoginChecked(ctx, d, provider, profileHome, accountID, now, nil, credentialID...)
}

func resolveNativeCodexLoginChecked(ctx context.Context, d core.RuntimeDescriptor, provider, profileHome, accountID string, now time.Time, qualify func() error, credentialID ...string) (ProviderAccessToken, error) {
	selector, err := nativeCredentialSelector(credentialID)
	if err != nil {
		return ProviderAccessToken{}, err
	}
	output, err := runNativeCodexProtocolChecked(ctx, d, profileHome, "resolve", selector, qualify)
	if err != nil {
		return ProviderAccessToken{}, err
	}
	defer clear(output)
	if len(output) == 0 {
		return ProviderAccessToken{}, ErrProviderAuthAbsent
	}
	p, err := NewProviderAccessToken(provider, string(output), now)
	if err != nil {
		return ProviderAccessToken{}, err
	}
	if accountID != "" && p.accountID != accountID {
		return ProviderAccessToken{}, ErrProviderAuthUnauthorized
	}
	return p, nil
}

func runNativeCodexProtocol(ctx context.Context, d core.RuntimeDescriptor, profileHome, mode, selector string) ([]byte, error) {
	return runNativeCodexProtocolChecked(ctx, d, profileHome, mode, selector, nil)
}

func runNativeCodexProtocolChecked(ctx context.Context, d core.RuntimeDescriptor, profileHome, mode, selector string, qualify func() error) ([]byte, error) {
	childCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// This private home is credential-free; never query in the approved source.
	scratch, err := os.MkdirTemp("", "aegis-native-command-")
	if err != nil {
		return nil, errGatewayCommand
	}
	removeHome := true
	defer func() {
		if removeHome {
			_ = os.RemoveAll(scratch)
		}
	}()
	launch, err := discoverRuntimeCommand(childCtx, d, scratch, nativeCodexLoginProtocol, []string{d.Installation, mode, selector})
	if err != nil {
		removeHome = !errors.Is(err, ErrAttemptCleanup)
		return nil, err
	}
	if err = childCtx.Err(); err != nil {
		return nil, err
	}
	if qualify != nil {
		if err := qualify(); err != nil {
			return nil, err
		}
	}
	if err := childCtx.Err(); err != nil {
		return nil, err
	}
	f, err := openNativeLogin(profileHome)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limit := 32 << 10
	if mode == "candidates" {
		limit = 2 << 20
	}
	output, status, err := runBoundedRuntime(childCtx, launch, "/nonexistent", scratch, limit, f)
	if err != nil {
		removeHome = !errors.Is(err, ErrAttemptCleanup)
		return nil, err
	}
	if status != 0 {
		clear(output)
		stages := map[int]string{20: "import_failed", 21: "mode_denied", 22: "pool_invalid", 23: "source_suppressed", 24: "source_ambiguous", 25: "pool_unusable", 26: "protocol_failed", 27: "source_approval_required", 28: "credential_selection_invalid"}
		if stage, ok := stages[status]; ok {
			return nil, nativeProviderFailure{stage: stage}
		}
		return nil, ErrProviderAuthInvalid
	}
	return output, nil
}

// PreviewNativeCodexLogin returns only validated non-secret metadata. It uses
// exactly the same qualified, read-only native parser as execution.
func (a *Adapter) PreviewNativeCodexLogin(ctx context.Context, profileHome string, now time.Time, credentialID ...string) (NativeCodexMetadata, error) {
	if a == nil || ctx == nil || ctx.Err() != nil {
		return NativeCodexMetadata{}, ErrProviderAuthUnauthorized
	}
	d, e := a.Discover(ctx)
	if e != nil {
		return NativeCodexMetadata{}, ErrProviderAuthRuntimeUnqualified
	}
	if e = a.qualifyProviderDescriptor(d); e != nil {
		return NativeCodexMetadata{}, e
	}
	p, e := resolveNativeCodexLoginChecked(ctx, d, "codex", profileHome, "", now, func() error { return a.qualifyProviderDescriptor(d) }, credentialID...)
	if e != nil {
		return NativeCodexMetadata{}, e
	}
	return NativeCodexMetadata{AccountID: p.accountID}, nil
}

func (a *Adapter) InspectNativeCodexCandidates(ctx context.Context, profileHome string, now time.Time) (NativeCodexCandidates, error) {
	if a == nil || ctx == nil || ctx.Err() != nil {
		return NativeCodexCandidates{}, ErrProviderAuthUnauthorized
	}
	d, e := a.Discover(ctx)
	if e != nil {
		return NativeCodexCandidates{}, ErrProviderAuthRuntimeUnqualified
	}
	if e = a.qualifyProviderDescriptor(d); e != nil {
		return NativeCodexCandidates{}, e
	}
	return inspectNativeCodexCandidatesChecked(ctx, d, profileHome, now, func() error { return a.qualifyProviderDescriptor(d) })
}

func inspectNativeCodexCandidates(ctx context.Context, d core.RuntimeDescriptor, profileHome string, now time.Time) (NativeCodexCandidates, error) {
	return inspectNativeCodexCandidatesChecked(ctx, d, profileHome, now, nil)
}

func inspectNativeCodexCandidatesChecked(ctx context.Context, d core.RuntimeDescriptor, profileHome string, now time.Time, qualify func() error) (NativeCodexCandidates, error) {
	output, e := runNativeCodexProtocolChecked(ctx, d, profileHome, "candidates", "", qualify)
	if e != nil {
		return NativeCodexCandidates{}, e
	}
	defer clear(output)
	// The private protocol's token field is never part of a public DTO or error.
	var rows []struct {
		CredentialID string `json:"credential_id"`
		Reason       string `json:"reason"`
		Token        string `json:"token"`
	}
	if len(output) != 0 && json.Unmarshal(output, &rows) != nil || len(rows) > 64 {
		return NativeCodexCandidates{}, ErrProviderAuthInvalid
	}
	out := NativeCodexCandidates{ProfileHome: profileHome, SelectionPolicy: "explicit-or-unique-token", Candidates: []NativeCodexCandidate{}}
	grants := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		if row.CredentialID != "" && !ValidNativeCredentialID(row.CredentialID) {
			return NativeCodexCandidates{}, ErrProviderAuthInvalid
		}
		c := NativeCodexCandidate{CredentialID: row.CredentialID, Reason: row.Reason}
		switch row.Reason {
		case "eligible":
			p, err := NewProviderAccessToken("codex", row.Token, now)
			if err == nil {
				c.Eligible = true
				c.AccountFingerprint = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("aegis-codex-account-v1\x00"+p.accountID)))
				grants[p.value] = true
			} else if errors.Is(err, ErrProviderAuthExpired) {
				c.Reason = "token_expired"
			} else {
				c.Reason = "token_invalid"
			}
		case "id_missing_or_invalid", "id_duplicate", "source_approval_required", "unsupported_auth_type", "unusable_status", "token_absent", "token_invalid":
		default:
			return NativeCodexCandidates{}, ErrProviderAuthInvalid
		}
		row.Token = ""
		out.Candidates = append(out.Candidates, c)
	}
	out.DistinctEligibleGrants = len(grants)
	return out, nil
}

// Descriptor-relative O_NOFOLLOW traversal pins every component and the exact
// source inode. Root/current-user owned, non-writable ancestry; owner-only named
// profile; regular owner-only single-link bounded store. Never read with tools.
func nativeLoginStatAllowed(st syscall.Stat_t, uid uint32) bool {
	return st.Mode&syscall.S_IFMT == syscall.S_IFREG && st.Uid == uid && st.Mode&0077 == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 1<<20
}

func openNativeLogin(home string) (*os.File, error) {
	nativeSource := filepath.Base(home) == ".hermes" || (filepath.Base(filepath.Dir(home)) == "profiles" && filepath.Base(filepath.Dir(filepath.Dir(home))) == ".hermes")
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || !nativeSource {
		return nil, ErrProviderAuthUnauthorized
	}
	fd, err := syscall.Open("/", syscall.O_DIRECTORY|syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrProviderAuthUnauthorized
	}
	defer func() { _ = syscall.Close(fd) }()
	for _, part := range strings.Split(strings.TrimPrefix(home, "/"), "/") {
		next, e := syscall.Openat(fd, part, syscall.O_DIRECTORY|syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return nil, ErrProviderAuthUnauthorized
		}
		_ = syscall.Close(fd)
		fd = next
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) != nil || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || st.Mode&0022 != 0 {
			return nil, ErrProviderAuthUnauthorized
		}
	}
	var dir syscall.Stat_t
	if syscall.Fstat(fd, &dir) != nil || dir.Uid != uint32(os.Geteuid()) || dir.Mode&0077 != 0 {
		return nil, ErrProviderAuthUnauthorized
	}
	tokenFD, err := syscall.Openat(fd, "auth.json", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if err == syscall.ENOENT {
			return nil, ErrProviderAuthAbsent
		}
		return nil, ErrProviderAuthUnauthorized
	}
	file := os.NewFile(uintptr(tokenFD), "controller-native-auth")
	var st syscall.Stat_t
	if syscall.Fstat(tokenFD, &st) != nil || !nativeLoginStatAllowed(st, uint32(os.Geteuid())) {
		file.Close()
		return nil, ErrProviderAuthUnauthorized
	}
	return file, nil
}
