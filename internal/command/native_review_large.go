package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"unicode"
	"unicode/utf8"
)

// Large exact reviews are not truncated to fit a pinentry protocol line. A
// trusted native text dialog requires an explicit checkbox and approval button.
func nativeReviewDataSafe(data string) bool {
	if data == "" || len(data) > companionJSONLimit+512 || !utf8.ValidString(data) {
		return false
	}
	for _, r := range data {
		if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func trustedNativeReviewHelper() string {
	p, err := filepath.EvalSymlinks("/usr/bin/zenity")
	if err != nil || !filepath.IsAbs(p) {
		return ""
	}
	for name := p; ; name = filepath.Dir(name) {
		info, e := os.Lstat(name)
		if e != nil {
			return ""
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 {
			return ""
		}
		if name == p && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
			return ""
		}
		if name == "/" {
			return p
		}
	}
}

func (s *nativeConfirmation) largeReview(parent context.Context, helper, description string) string {
	if !nativeReviewDataSafe(description) {
		return "policy_rejected"
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	cmd := s.command(ctx, helper, "--text-info", "--title=Aegis exact local Doer approval", "--checkbox=I reviewed and approve only this exact scope", "--ok-label=Approve exact scope", "--cancel-label=Cancel", "--extra-button=Deny", "--width=900", "--height=700")
	env := newAuthorityPassphraseService(nil)
	env.getenv = s.getenv
	cmd.Env = env.environment()
	configureProtectedProcess(cmd)
	cmd.Stdin = strings.NewReader(description)
	out := &boundedCapture{maximum: 128}
	errout := &boundedCapture{maximum: 0}
	cmd.Stdout = out
	cmd.Stderr = errout
	if cmd.Start() != nil {
		return "unavailable"
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			terminateProtectedProcess(cmd)
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	if ctx.Err() != nil {
		if parent.Err() == context.Canceled {
			return "cancelled"
		}
		return "timeout"
	}
	if out.total > 128 || errout.total > pinentryStderrLimit {
		return "malformed_protocol"
	}
	if strings.TrimSpace(out.buffer.String()) == "Deny" {
		return "denied"
	}
	if err != nil {
		return "cancelled"
	}
	if out.buffer.String() != "" {
		return "malformed_protocol"
	}
	return "confirmed"
}
