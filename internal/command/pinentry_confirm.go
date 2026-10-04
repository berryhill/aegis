package command

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// Confirmation is deliberately separate from secret intake: no GETPIN, retry,
// terminal fallback, shell, model, or caller-selected helper is permitted.
type nativeConfirmation struct {
	command commandConstructor
	getenv  func(string) string
	timeout time.Duration
}

func newNativeConfirmation() *nativeConfirmation {
	return &nativeConfirmation{command: exec.CommandContext, getenv: os.Getenv, timeout: pinentryTimeout}
}

func trustedConfirmationHelper() string {
	// Do not trust PATH, browser input, or --pinentry-executable for authority.
	for _, candidate := range []string{"/usr/bin/pinentry-gnome3", "/usr/bin/pinentry-qt", "/usr/bin/pinentry"} {
		path, err := filepath.EvalSymlinks(candidate)
		if err != nil || !filepath.IsAbs(path) {
			continue
		}
		safe := true
		for p := path; ; p = filepath.Dir(p) {
			info, e := os.Lstat(p)
			if e != nil {
				safe = false
				break
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 {
				safe = false
				break
			}
			if p == path && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
				safe = false
				break
			}
			if p == "/" {
				break
			}
		}
		if safe {
			return path
		}
	}
	return ""
}

func confirmationDataSafe(data string) bool {
	if !utf8.ValidString(data) || len(data) == 0 || len(assuanEncode([]byte(data)))+len("SETDESC ")+1 > pinentryLineLimit {
		return false
	}
	for _, r := range data {
		if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func (s *nativeConfirmation) confirm(parent context.Context, description string) string {
	if !nativeReviewDataSafe(description) {
		return "policy_rejected"
	}
	if s.getenv("DISPLAY") == "" && s.getenv("WAYLAND_DISPLAY") == "" {
		return "unavailable"
	}
	if !confirmationDataSafe(description) {
		helper := trustedNativeReviewHelper()
		if helper == "" {
			return "unavailable"
		}
		return s.largeReview(parent, helper, description)
	}
	helper := trustedConfirmationHelper()
	if helper == "" || (s.getenv("DISPLAY") == "" && s.getenv("WAYLAND_DISPLAY") == "") {
		return "unavailable"
	}
	return s.run(parent, helper, description)
}

func (s *nativeConfirmation) run(parent context.Context, helper, description string) string {
	if !confirmationDataSafe(description) {
		return "policy_rejected"
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	cmd := s.command(ctx, helper)
	env := newAuthorityPassphraseService(nil)
	env.getenv = s.getenv
	cmd.Env = env.environment()
	configureProtectedProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "unavailable"
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return "unavailable"
	}
	// Never return helper text (it can contain the untrusted review or secrets).
	diagnostic := &boundedCapture{maximum: 0}
	cmd.Stderr = diagnostic
	if cmd.Start() != nil {
		_ = stdin.Close()
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
	result := "malformed_protocol"
	reader := &protocolReader{reader: bufio.NewReaderSize(stdout, pinentryLineLimit+1)}
	line, e := reader.line()
	if e == nil && validOK(line) {
		setup := true
		for _, request := range []string{"SETTITLE Aegis exact Doer approval", "SETDESC " + assuanEncode([]byte(description)), "SETOK Approve exact scope", "SETCANCEL Cancel", "SETNOTOK Deny"} {
			if _, e = io.WriteString(stdin, request+"\n"); e != nil {
				setup = false
				break
			}
			line, e = reader.line()
			if e != nil || !validOK(line) {
				setup = false
				break
			}
		}
		if setup {
			if _, e = io.WriteString(stdin, "CONFIRM\n"); e == nil {
				line, e = reader.line()
				switch {
				case e == io.EOF:
					result = "no_decision"
				case e != nil:
					result = "malformed_protocol"
				case line == "OK":
					result = "confirmed"
				case strings.HasPrefix(line, "ERR "):
					code, valid := assuanErrorCode(line)
					if valid && code&0xffff == 99 {
						result = "cancelled"
					} else if valid && code&0xffff == 114 {
						result = "denied"
					}
				}
			}
		}
	}
	if result == "confirmed" {
		_, _ = io.WriteString(stdin, "BYE\n")
	} else {
		terminateProtectedProcess(cmd)
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	close(done)
	if ctx.Err() != nil {
		if parent.Err() == context.Canceled {
			return "cancelled"
		}
		return "timeout"
	}
	if diagnostic.total > pinentryStderrLimit {
		return "malformed_protocol"
	}
	if result == "confirmed" && waitErr != nil {
		return "malformed_protocol"
	}
	return result
}
