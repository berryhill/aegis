package command

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
	"golang.org/x/term"
)

const (
	authorityPassphraseMinimum = credentials.AuthorityPassphraseMinimumBytes
	authorityPassphraseMaximum = credentials.AuthorityPassphraseMaximumBytes
	passphraseRetryLimit       = 3
	pinentryLineLimit          = 4096
	pinentryTotalLimit         = 16 << 10
	pinentryRecordLimit        = 64
	pinentryStderrLimit        = 4 << 10
	pinentryTimeout            = 2 * time.Minute
)

type AuthorityPassphraseIntent uint8

const (
	AuthorityPassphraseCreate AuthorityPassphraseIntent = iota + 1
	AuthorityPassphraseUnlock
)

type AuthorityPassphraseRequest struct {
	Intent     AuthorityPassphraseIntent
	Input      io.Reader
	Diagnostic io.Writer
}

type AuthorityPassphraseProvider interface {
	Acquire(context.Context, AuthorityPassphraseRequest) ([]byte, error)
}

type authorityPassphraseContextKey struct{}

func passphraseProvider(cmd interface{ Context() context.Context }) (AuthorityPassphraseProvider, error) {
	provider, ok := cmd.Context().Value(authorityPassphraseContextKey{}).(AuthorityPassphraseProvider)
	if !ok || provider == nil {
		return nil, errors.New("authority passphrase provider is unavailable")
	}
	return provider, nil
}

type PassphraseErrorKind string

const (
	PassphraseCancelled   PassphraseErrorKind = "cancelled"
	PassphraseUnavailable PassphraseErrorKind = "unavailable"
	PassphraseLaunch      PassphraseErrorKind = "launch_failure"
	PassphraseProtocol    PassphraseErrorKind = "protocol_failure"
	PassphraseTimeout     PassphraseErrorKind = "timeout"
	PassphrasePolicy      PassphraseErrorKind = "policy_failure"
)

type PassphraseError struct {
	Kind        PassphraseErrorKind
	Interaction bool
	reason      string
}

func (e *PassphraseError) Error() string {
	if e.reason != "" {
		return e.reason
	}
	return "authority passphrase intake failed"
}

func IsPassphraseError(err error, kind PassphraseErrorKind) bool {
	var target *PassphraseError
	return errors.As(err, &target) && target.Kind == kind
}

type commandConstructor func(context.Context, string, ...string) *exec.Cmd

type authorityPassphraseService struct {
	explicit func() string
	getenv   func(string) string
	lookPath func(string) (string, error)
	command  commandConstructor
	timeout  time.Duration
}

func newAuthorityPassphraseService(explicit func() string) *authorityPassphraseService {
	return &authorityPassphraseService{
		explicit: explicit,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		command:  exec.CommandContext,
		timeout:  pinentryTimeout,
	}
}

func (s *authorityPassphraseService) Acquire(ctx context.Context, request AuthorityPassphraseRequest) ([]byte, error) {
	if request.Intent != AuthorityPassphraseCreate && request.Intent != AuthorityPassphraseUnlock {
		return nil, &PassphraseError{Kind: PassphrasePolicy, reason: "invalid authority passphrase intent"}
	}
	for attempt := 0; attempt < passphraseRetryLimit; attempt++ {
		mode := "unlock"
		if request.Intent == AuthorityPassphraseCreate {
			mode = "create"
		}
		first, err := s.acquireOnce(ctx, request, mode)
		if err != nil {
			if IsPassphraseError(err, PassphrasePolicy) && attempt+1 < passphraseRetryLimit {
				metadataFeedback(request.Diagnostic, err)
				continue
			}
			return nil, err
		}
		if request.Intent == AuthorityPassphraseUnlock {
			return first, nil
		}
		second, confirmErr := s.acquireOnce(ctx, request, "confirm")
		if confirmErr != nil {
			wipeSecret(first)
			if IsPassphraseError(confirmErr, PassphrasePolicy) && attempt+1 < passphraseRetryLimit {
				metadataFeedback(request.Diagnostic, confirmErr)
				continue
			}
			return nil, confirmErr
		}
		matched := len(first) == len(second) && subtle.ConstantTimeCompare(first, second) == 1
		wipeSecret(second)
		if matched {
			return first, nil
		}
		wipeSecret(first)
		mismatch := &PassphraseError{Kind: PassphrasePolicy, Interaction: true, reason: "authority passphrase confirmation does not match"}
		if attempt+1 == passphraseRetryLimit {
			return nil, mismatch
		}
		metadataFeedback(request.Diagnostic, mismatch)
	}
	return nil, &PassphraseError{Kind: PassphrasePolicy, reason: "authority passphrase retry limit exhausted"}
}

func metadataFeedback(output io.Writer, err error) {
	if output != nil {
		_, _ = fmt.Fprintln(output, "Aegis:", err)
	}
}

func (s *authorityPassphraseService) acquireOnce(ctx context.Context, request AuthorityPassphraseRequest, mode string) ([]byte, error) {
	executable, discoveryErr := s.resolve()
	var pinentryErr *PassphraseError
	if discoveryErr == nil {
		value, err := s.pinentry(ctx, executable, mode)
		if err == nil {
			return value, nil
		}
		var protected *PassphraseError
		if !errors.As(err, &protected) || protected.Kind == PassphraseCancelled || protected.Interaction || protected.Kind == PassphraseTimeout {
			return nil, err
		}
		pinentryErr = protected
		// Only initialization failures before GETPIN may use the second surface.
		if protected.Kind != PassphraseUnavailable && protected.Kind != PassphraseLaunch && protected.Kind != PassphraseProtocol {
			return nil, err
		}
	} else if !IsPassphraseError(discoveryErr, PassphraseUnavailable) {
		return nil, discoveryErr
	}
	value, fallbackErr := s.terminalFallback(ctx, request, mode)
	if fallbackErr != nil && pinentryErr != nil && IsPassphraseError(fallbackErr, PassphraseUnavailable) {
		return nil, &PassphraseError{Kind: pinentryErr.Kind, reason: pinentryErr.reason + "; no terminal fallback is available"}
	}
	return value, fallbackErr
}

func (s *authorityPassphraseService) resolve() (string, error) {
	explicit := ""
	if s.explicit != nil {
		explicit = s.explicit()
	}
	if explicit != "" {
		if strings.TrimSpace(explicit) != explicit || strings.IndexByte(explicit, 0) >= 0 || !filepath.IsAbs(explicit) {
			return "", &PassphraseError{Kind: PassphrasePolicy, reason: "configured pinentry executable must be one absolute path"}
		}
		info, err := os.Stat(explicit)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", &PassphraseError{Kind: PassphrasePolicy, reason: "configured pinentry executable must be an executable regular file"}
		}
		return explicit, nil
	}
	path, err := s.lookPath("pinentry")
	if err != nil {
		return "", &PassphraseError{Kind: PassphraseUnavailable, reason: "protected pinentry is unavailable"}
	}
	return path, nil
}

func (s *authorityPassphraseService) pinentry(parent context.Context, executable string, mode string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	cmd := s.command(ctx, executable)
	cmd.Env = s.environment()
	configureProtectedProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, &PassphraseError{Kind: PassphraseLaunch, reason: "pinentry could not initialize"}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, &PassphraseError{Kind: PassphraseLaunch, reason: "pinentry could not initialize"}
	}
	stderr := &boundedCapture{maximum: pinentryStderrLimit}
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, &PassphraseError{Kind: PassphraseLaunch, reason: "pinentry could not start"}
	}
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			terminateProtectedProcess(cmd)
		case <-watchDone:
		}
	}()

	reader := &protocolReader{reader: bufio.NewReaderSize(stdout, pinentryLineLimit+1)}
	interaction := false
	fail := func(kind PassphraseErrorKind, reason string) ([]byte, error) {
		_ = stdin.Close()
		terminateProtectedProcess(cmd)
		_ = cmd.Wait()
		close(watchDone)
		if ctx.Err() != nil {
			kind, reason = passphraseContextFailure(parent)
		}
		if stderr.total > pinentryStderrLimit {
			kind, reason = PassphraseProtocol, "pinentry exceeded the diagnostic output bound"
		}
		return nil, &PassphraseError{Kind: kind, Interaction: interaction, reason: reason}
	}
	line, readErr := reader.line()
	if readErr != nil || !strings.HasPrefix(line, "OK") || (len(line) > 2 && line[2] != ' ') {
		return fail(PassphraseProtocol, "pinentry returned an invalid greeting")
	}

	description := "Unlock the Aegis credential authority. Minimum 12 bytes; the passphrase is not persisted."
	prompt := "Authority passphrase:"
	okText := "Unlock"
	if mode == "create" {
		description = "Create the Aegis credential authority passphrase. Minimum 12 bytes; it is not persisted. Losing it makes the authority unrecoverable without separate recovery."
		prompt = "New authority passphrase:"
		okText = "Continue"
	} else if mode == "confirm" {
		description = "Confirm the new Aegis credential authority passphrase. Minimum 12 bytes; it is not persisted."
		prompt = "Confirm authority passphrase:"
		okText = "Confirm"
	}
	for _, command := range []string{
		"SETTITLE " + assuanEncode([]byte("Aegis credential authority")),
		"SETDESC " + assuanEncode([]byte(description)),
		"SETPROMPT " + assuanEncode([]byte(prompt)),
		"SETOK " + assuanEncode([]byte(okText)),
	} {
		if _, err = io.WriteString(stdin, command+"\n"); err != nil {
			return fail(PassphraseProtocol, "pinentry request failed before protected input")
		}
		line, readErr = reader.line()
		if readErr != nil {
			return fail(PassphraseProtocol, "pinentry failed before protected input")
		}
		if strings.HasPrefix(line, "ERR ") && strings.HasPrefix(command, "SETOK ") {
			continue
		}
		if !validOK(line) {
			return fail(PassphraseProtocol, "pinentry rejected protected prompt setup")
		}
	}
	// From this point the helper may have observed GETPIN even if the pipe write
	// reports a short/failed write, so a second input surface is never safe.
	interaction = true
	if _, err = io.WriteString(stdin, "GETPIN\n"); err != nil {
		return fail(PassphraseProtocol, "pinentry could not request protected input")
	}
	var data []byte
	seenData := false
	for {
		line, readErr = reader.line()
		if readErr != nil {
			wipeSecret(data)
			return fail(PassphraseProtocol, "pinentry did not complete protected input")
		}
		switch {
		case strings.HasPrefix(line, "D "):
			if seenData {
				wipeSecret(data)
				return fail(PassphraseProtocol, "pinentry returned duplicate protected data")
			}
			seenData = true
			data, err = assuanDecode(line[2:])
			if err != nil {
				return fail(PassphraseProtocol, "pinentry returned malformed protected data")
			}
		case validOK(line):
			if !seenData {
				return fail(PassphraseProtocol, "pinentry returned protected success without data")
			}
			if policyErr := validateAuthorityPassphrase(data); policyErr != nil {
				wipeSecret(data)
				_, _ = io.WriteString(stdin, "BYE\n")
				_ = stdin.Close()
				waitErr := cmd.Wait()
				close(watchDone)
				if stderr.total > pinentryStderrLimit {
					return nil, &PassphraseError{Kind: PassphraseProtocol, Interaction: true, reason: "pinentry exceeded the diagnostic output bound"}
				}
				if waitErr != nil && ctx.Err() != nil {
					kind, reason := passphraseContextFailure(parent)
					return nil, &PassphraseError{Kind: kind, Interaction: true, reason: reason}
				}
				return nil, policyErr
			}
			_, _ = io.WriteString(stdin, "BYE\n")
			_ = stdin.Close()
			waitErr := cmd.Wait()
			close(watchDone)
			if stderr.total > pinentryStderrLimit {
				wipeSecret(data)
				return nil, &PassphraseError{Kind: PassphraseProtocol, Interaction: true, reason: "pinentry exceeded the diagnostic output bound"}
			}
			if waitErr != nil {
				wipeSecret(data)
				return nil, &PassphraseError{Kind: PassphraseProtocol, Interaction: true, reason: "pinentry exited unsuccessfully after protected input"}
			}
			return data, nil
		case strings.HasPrefix(line, "ERR "):
			wipeSecret(data)
			code, ok := assuanErrorCode(line)
			if ok && code&0xffff == 99 {
				return fail(PassphraseCancelled, "authority passphrase entry cancelled")
			}
			return fail(PassphraseProtocol, "pinentry rejected protected input")
		default:
			wipeSecret(data)
			return fail(PassphraseProtocol, "pinentry returned an unexpected protocol record")
		}
	}
}

func (s *authorityPassphraseService) terminalFallback(ctx context.Context, request AuthorityPassphraseRequest, mode string) ([]byte, error) {
	input, inputOK := request.Input.(*os.File)
	output, outputOK := request.Diagnostic.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(int(input.Fd())) || !term.IsTerminal(int(output.Fd())) {
		return nil, &PassphraseError{Kind: PassphraseUnavailable, reason: "authority passphrase intake requires protected pinentry or terminal-backed no-echo input and diagnostic output"}
	}
	prompt := "Authority passphrase (minimum 12 bytes): "
	if mode == "create" {
		prompt = "New authority passphrase (minimum 12 bytes): "
	} else if mode == "confirm" {
		prompt = "Confirm authority passphrase: "
	}
	value, err := readTerminalSecretBounded(ctx, input, output, prompt, authorityPassphraseMaximum)
	if err != nil {
		kind := PassphraseProtocol
		reason := "protected terminal authority passphrase intake failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			kind, reason = PassphraseCancelled, "authority passphrase entry cancelled"
		} else if errors.Is(err, context.DeadlineExceeded) {
			kind, reason = PassphraseTimeout, "authority passphrase request timed out"
		}
		return nil, &PassphraseError{Kind: kind, Interaction: true, reason: reason}
	}
	if err = validateAuthorityPassphrase(value); err != nil {
		wipeSecret(value)
		return nil, err
	}
	return value, nil
}

func passphraseContextFailure(parent context.Context) (PassphraseErrorKind, string) {
	if errors.Is(parent.Err(), context.Canceled) {
		return PassphraseCancelled, "authority passphrase entry cancelled"
	}
	return PassphraseTimeout, "authority passphrase request timed out"
}

func validateAuthorityPassphrase(value []byte) error {
	if len(value) < authorityPassphraseMinimum || len(value) > authorityPassphraseMaximum {
		return &PassphraseError{Kind: PassphrasePolicy, Interaction: true, reason: "authority passphrase must be between 12 and 1024 bytes"}
	}
	for _, b := range value {
		if b == 0 || b == '\r' || b == '\n' {
			return &PassphraseError{Kind: PassphrasePolicy, Interaction: true, reason: "authority passphrase contains unsupported control bytes"}
		}
	}
	return nil
}

func (s *authorityPassphraseService) environment() []string {
	names := []string{"PATH", "HOME", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "TERM", "GPG_TTY"}
	environment := make([]string, 0, len(names))
	total := 0
	for _, name := range names {
		if value := s.getenv(name); value != "" && len(value) <= 4096 && !strings.ContainsAny(value, "\x00\r\n") {
			entry := name + "=" + value
			if total+len(entry) > 32<<10 {
				continue
			}
			environment = append(environment, entry)
			total += len(entry)
		}
	}
	return environment
}

type protocolReader struct {
	reader  *bufio.Reader
	total   int
	records int
}

func (r *protocolReader) line() (string, error) {
	if r.records >= pinentryRecordLimit || r.total >= pinentryTotalLimit {
		return "", errors.New("protocol bounds exceeded")
	}
	line, err := r.reader.ReadString('\n')
	r.records++
	r.total += len(line)
	if len(line) > pinentryLineLimit || r.total > pinentryTotalLimit {
		return "", errors.New("protocol bounds exceeded")
	}
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if strings.IndexByte(line, 0) >= 0 {
		return "", errors.New("protocol contains NUL")
	}
	return line, nil
}

func validOK(line string) bool { return line == "OK" || strings.HasPrefix(line, "OK ") }

func assuanErrorCode(line string) (uint64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "ERR" {
		return 0, false
	}
	value, err := strconv.ParseUint(fields[1], 10, 32)
	return value, err == nil
}

func assuanEncode(value []byte) string {
	const hex = "0123456789ABCDEF"
	var encoded strings.Builder
	for _, b := range value {
		if b == '%' || b == '\r' || b == '\n' || b < 0x20 || b == 0x7f {
			encoded.WriteByte('%')
			encoded.WriteByte(hex[b>>4])
			encoded.WriteByte(hex[b&0xf])
		} else {
			encoded.WriteByte(b)
		}
	}
	return encoded.String()
}

func assuanDecode(value string) ([]byte, error) {
	decoded := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			decoded = append(decoded, value[i])
			continue
		}
		if i+2 >= len(value) {
			wipeSecret(decoded)
			return nil, errors.New("malformed escape")
		}
		part, err := strconv.ParseUint(value[i+1:i+3], 16, 8)
		if err != nil {
			wipeSecret(decoded)
			return nil, errors.New("malformed escape")
		}
		decoded = append(decoded, byte(part))
		i += 2
	}
	if len(decoded) > authorityPassphraseMaximum {
		wipeSecret(decoded)
		return nil, errors.New("protected data exceeds bound")
	}
	return decoded, nil
}

type boundedCapture struct {
	buffer  bytes.Buffer
	maximum int
	total   int
}

func (w *boundedCapture) Write(value []byte) (int, error) {
	w.total += len(value)
	remaining := max(w.maximum-w.buffer.Len(), 0)
	if remaining > 0 {
		_, _ = w.buffer.Write(value[:min(remaining, len(value))])
	}
	return len(value), nil
}
