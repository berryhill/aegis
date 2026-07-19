package command

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/spf13/cobra"
)

func TestFakePinentryProcess(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		return
	}
	scenario, payload := os.Args[separator+1], os.Args[separator+2]
	capture := ""
	if separator+3 < len(os.Args) {
		capture = os.Args[separator+3]
	}
	if scenario == "early-eof" {
		return
	}
	if scenario == "malformed-greeting" {
		fmt.Fprintln(os.Stdout, "WHAT")
		return
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintln(os.Stdout, "OK fake ready")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if capture != "" {
			file, openErr := os.OpenFile(capture, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if openErr == nil {
				_, _ = file.WriteString(line)
				_ = file.Close()
			}
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "SET"):
			fmt.Fprintln(os.Stdout, "OK")
		case line == "GETPIN":
			switch scenario {
			case "success":
				fmt.Fprintln(os.Stdout, "D "+assuanEncode([]byte(payload)))
				fmt.Fprintln(os.Stdout, "OK")
			case "cancel":
				fmt.Fprintln(os.Stdout, "ERR 83886179")
			case "malformed-escape":
				fmt.Fprintln(os.Stdout, "D broken%Q1")
				fmt.Fprintln(os.Stdout, "OK")
			case "duplicate":
				fmt.Fprintln(os.Stdout, "D "+payload)
				fmt.Fprintln(os.Stdout, "D "+payload)
				fmt.Fprintln(os.Stdout, "OK")
			case "post-failure":
				fmt.Fprintln(os.Stdout, "WHAT")
			case "oversized-line":
				fmt.Fprintln(os.Stdout, strings.Repeat("x", pinentryLineLimit+1))
			case "oversized-stderr":
				fmt.Fprint(os.Stderr, strings.Repeat("diagnostic", pinentryStderrLimit))
				fmt.Fprintln(os.Stdout, "D "+assuanEncode([]byte(payload)))
				fmt.Fprintln(os.Stdout, "OK")
			case "nonzero":
				fmt.Fprintln(os.Stdout, "D "+assuanEncode([]byte(payload)))
				fmt.Fprintln(os.Stdout, "OK")
				os.Exit(2)
			case "hang":
				time.Sleep(10 * time.Second)
			}
		case line == "BYE":
			fmt.Fprintln(os.Stdout, "OK")
			return
		}
	}
}

func fakePinentryCommand(scenario func() string, payload func() string, capture string) commandConstructor {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakePinentryProcess$", "--", scenario(), payload(), capture)
	}
}

func testPassphraseService(t *testing.T, scenario func() string, payload func() string, capture string) *authorityPassphraseService {
	t.Helper()
	return &authorityPassphraseService{
		explicit: func() string { return "" },
		getenv:   os.Getenv,
		lookPath: func(string) (string, error) { return "/fake/pinentry", nil },
		command:  fakePinentryCommand(scenario, payload, capture),
		timeout:  2 * time.Second,
	}
}

func TestPinentryProtocolSuccessEscapingAndStaticChrome(t *testing.T) {
	canary := "twelve%bytes-value"
	capture := filepath.Join(t.TempDir(), "protocol")
	service := testPassphraseService(t, func() string { return "success" }, func() string { return canary }, capture)
	value, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Input: bytes.NewReader(nil), Diagnostic: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer wipeSecret(value)
	if string(value) != canary {
		t.Fatal("decoded value mismatch")
	}
	transcript, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"SETTITLE Aegis credential authority", "SETDESC Unlock the Aegis credential authority.", "SETPROMPT Authority passphrase:", "SETOK Unlock", "GETPIN", "BYE"} {
		if !bytes.Contains(transcript, []byte(command)) {
			t.Fatalf("missing command %q in %s", command, transcript)
		}
	}
	if bytes.Contains(transcript, []byte(canary)) {
		t.Fatal("passphrase leaked into client command transcript")
	}
}

func TestPinentryCreateUsesTwoFreshRequestsAndRetriesMismatch(t *testing.T) {
	values := []string{"first-passphrase-value", "different-passphrase", "matching-passphrase", "matching-passphrase"}
	var calls atomic.Int32
	service := testPassphraseService(t, func() string { return "success" }, func() string {
		index := int(calls.Add(1)) - 1
		return values[index]
	}, "")
	var diagnostic bytes.Buffer
	value, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseCreate, Diagnostic: &diagnostic})
	if err != nil {
		t.Fatal(err)
	}
	defer wipeSecret(value)
	if string(value) != "matching-passphrase" || calls.Load() != 4 {
		t.Fatalf("value length=%d calls=%d", len(value), calls.Load())
	}
	if !strings.Contains(diagnostic.String(), "confirmation does not match") {
		t.Fatalf("diagnostic=%q", diagnostic.String())
	}
	for _, secret := range values {
		if strings.Contains(diagnostic.String(), secret) {
			t.Fatal("diagnostic leaked a passphrase")
		}
	}
}

func TestPinentryPolicyBoundsUseBytes(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		ok    bool
	}{
		{"empty", "", false}, {"eleven", "12345678901", false}, {"twelve", "123456789012", true},
		{"multibyte-eleven-bytes", "éééééx", false}, {"multibyte-twelve-bytes", "éééééé", true},
		{"nul", "123456789012\x00", false}, {"oversized", strings.Repeat("x", authorityPassphraseMaximum+1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAuthorityPassphrase([]byte(test.value))
			if (err == nil) != test.ok {
				t.Fatalf("valid=%t err=%v", err == nil, err)
			}
		})
	}
}

func TestPinentryCancellationAndPostInteractionFailureNeverFallback(t *testing.T) {
	for _, scenario := range []string{"cancel", "post-failure", "malformed-escape", "duplicate", "oversized-line", "oversized-stderr", "nonzero"} {
		t.Run(scenario, func(t *testing.T) {
			service := testPassphraseService(t, func() string { return scenario }, func() string { return "long-enough-value" }, "")
			_, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Input: bytes.NewBufferString("terminal-canary\n"), Diagnostic: io.Discard})
			if err == nil {
				t.Fatal("failure accepted")
			}
			if scenario == "cancel" && !IsPassphraseError(err, PassphraseCancelled) {
				t.Fatalf("cancel kind=%v", err)
			}
			if strings.Contains(err.Error(), "long-enough-value") || strings.Contains(err.Error(), "terminal-canary") {
				t.Fatal("error leaked sensitive input")
			}
		})
	}
}

func TestPinentryPreInteractionProtocolFailuresRemainMetadataSafe(t *testing.T) {
	for _, scenario := range []string{"malformed-greeting", "early-eof"} {
		t.Run(scenario, func(t *testing.T) {
			service := testPassphraseService(t, func() string { return scenario }, func() string { return "protocol-canary-value" }, "")
			_, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Input: bytes.NewReader(nil), Diagnostic: io.Discard})
			if err == nil || !IsPassphraseError(err, PassphraseProtocol) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), "protocol-canary-value") {
				t.Fatal("error leaked payload")
			}
		})
	}
}

func TestPinentryUnavailableWithoutTTYIsActionable(t *testing.T) {
	service := newAuthorityPassphraseService(func() string { return "" })
	service.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	_, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Input: bytes.NewReader(nil), Diagnostic: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "protected pinentry or terminal-backed no-echo") {
		t.Fatalf("error=%v", err)
	}
}

func TestPinentryLaunchFailureIsTypedWhenNoFallbackExists(t *testing.T) {
	service := testPassphraseService(t, func() string { return "success" }, func() string { return "unused-canary" }, "")
	missing := filepath.Join(t.TempDir(), "missing-pinentry")
	service.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, missing)
	}
	_, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Input: bytes.NewReader(nil), Diagnostic: io.Discard})
	if err == nil || !IsPassphraseError(err, PassphraseLaunch) {
		t.Fatalf("error=%v", err)
	}
}

func TestPinentryContextTimeoutReapsHelper(t *testing.T) {
	service := testPassphraseService(t, func() string { return "hang" }, func() string { return "unused-passphrase" }, "")
	service.timeout = 100 * time.Millisecond
	started := time.Now()
	_, err := service.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: AuthorityPassphraseUnlock, Diagnostic: io.Discard})
	if err == nil || !IsPassphraseError(err, PassphraseTimeout) {
		t.Fatalf("error=%v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("timeout did not terminate promptly")
	}
}

func TestPinentryEnvironmentAllowlist(t *testing.T) {
	values := map[string]string{
		"PATH": "/safe/bin", "HOME": "/safe/home", "DISPLAY": ":77", "WAYLAND_DISPLAY": "wayland-test",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/safe/bus", "OPENAI_API_KEY": "provider-canary", "HTTPS_PROXY": "proxy-canary", "AEGIS_TOKEN": "token-canary",
	}
	service := newAuthorityPassphraseService(func() string { return "" })
	service.getenv = func(name string) string { return values[name] }
	environment := strings.Join(service.environment(), "\n")
	for _, expected := range []string{"PATH=/safe/bin", "HOME=/safe/home", "DISPLAY=:77", "WAYLAND_DISPLAY=wayland-test", "DBUS_SESSION_BUS_ADDRESS=unix:path=/safe/bus"} {
		if !strings.Contains(environment, expected) {
			t.Fatalf("missing %q in %q", expected, environment)
		}
	}
	for _, canary := range []string{"provider-canary", "proxy-canary", "token-canary"} {
		if strings.Contains(environment, canary) {
			t.Fatal("secret-bearing environment value inherited")
		}
	}
}

func TestConfiguredPinentryValidation(t *testing.T) {
	for _, path := range []string{"pinentry", " /absolute/pinentry", "/missing/pinentry"} {
		service := newAuthorityPassphraseService(func() string { return path })
		_, err := service.resolve()
		if err == nil || !IsPassphraseError(err, PassphrasePolicy) {
			t.Fatalf("path=%q error=%v", path, err)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "pinentry")
	if err := os.WriteFile(path, []byte("fake"), 0700); err != nil {
		t.Fatal(err)
	}
	service := newAuthorityPassphraseService(func() string { return path })
	resolved, err := service.resolve()
	if err != nil || resolved != path {
		t.Fatalf("resolved=%q error=%v", resolved, err)
	}
}

func TestAssuanBoundsAndMalformedRecords(t *testing.T) {
	if _, err := assuanDecode("bad%Q0"); err == nil {
		t.Fatal("malformed escape accepted")
	}
	if got := assuanEncode([]byte("a%b\r\n")); got != "a%25b%0D%0A" {
		t.Fatalf("encoded=%q", got)
	}
	reader := &protocolReader{reader: bufio.NewReader(strings.NewReader(strings.Repeat("x", pinentryLineLimit+1) + "\n"))}
	if _, err := reader.line(); err == nil {
		t.Fatal("oversized line accepted")
	}
}

func TestLoadConfiguredCustodianRetriesOnlyAuthentication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.kek.enc")
	correct := []byte("correct-passphrase-value")
	if err := createTestPassphraseKey(path, correct); err != nil {
		t.Fatal(err)
	}
	provider := &sequencePassphrases{values: [][]byte{[]byte("wrong-passphrase-value"), append([]byte(nil), correct...)}}
	cmd := testCommandWithProvider(provider)
	custodian, err := loadConfiguredCustodian(cmd, testPassphraseConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	custodian.Close()
	if provider.calls != 2 {
		t.Fatalf("calls=%d", provider.calls)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	provider = &sequencePassphrases{values: [][]byte{append([]byte(nil), correct...), append([]byte(nil), correct...)}}
	cmd = testCommandWithProvider(provider)
	if _, err = loadConfiguredCustodian(cmd, testPassphraseConfig(path)); err == nil || provider.calls != 1 {
		t.Fatalf("insecure artifact error=%v calls=%d", err, provider.calls)
	}
}

func createTestPassphraseKey(path string, passphrase []byte) error {
	return credentials.CreatePassphraseKey(path, "authority-kek", passphrase)
}

func testPassphraseConfig(path string) config.CredentialAuthority {
	return config.CredentialAuthority{Custody: "passphrase-file", KEKFile: path}
}

func testCommandWithProvider(provider AuthorityPassphraseProvider) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, provider))
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

type sequencePassphrases struct {
	values [][]byte
	calls  int
}

func (s *sequencePassphrases) Acquire(context.Context, AuthorityPassphraseRequest) ([]byte, error) {
	if s.calls >= len(s.values) {
		return nil, errors.New("unexpected passphrase request")
	}
	value := append([]byte(nil), s.values[s.calls]...)
	s.calls++
	return value, nil
}
