//go:build linux

package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func TestNativeStatDiagnosticSameSnapshotDenies(t *testing.T) {
	const secret = "SECRET /private/prompt token=credential argv provider_stderr"
	valid := contributionProcStat(42, "S")
	mutate := func(index int, value string) []byte {
		text := string(valid)
		end := strings.LastIndexByte(text, ')')
		fields := strings.Fields(text[end+1:])
		fields[index] = value
		return []byte("100 (" + secret + ") " + strings.Join(fields, " "))
	}
	cases := []struct {
		name, reason, class string
		raw                 []byte
		err                 error
	}{
		{"parenthesis", "parenthesis", "stat_unavailable", []byte(secret), nil},
		{"fields", "fields", "stat_unavailable", []byte("100 (" + secret + ") S 1 42"), nil},
		{"group", "group", "stat_unavailable", mutate(2, secret), nil},
		{"negative_group", "group", "stat_unavailable", mutate(2, "-1"), nil},
		{"state", "state", "stat_unavailable", mutate(0, "?"), nil},
		{"long_state", "state", "stat_unavailable", mutate(0, "SS"), nil},
		{"start", "start", "stat_unavailable", mutate(19, "-1"), nil},
	}
	for _, c := range []struct {
		err   error
		class string
	}{
		{unix.EACCES, "access"}, {unix.EPERM, "permission"}, {unix.EIO, "io"},
		{unix.EMFILE, "process_fd_limit"}, {unix.ENFILE, "system_fd_limit"},
		{unix.ENOMEM, "memory"}, {unix.EINTR, "interrupted"},
		{ErrContributionStopUnavailable, "stat_unavailable"}, {errors.New(secret), "other"},
	} {
		cases = append(cases, struct {
			name, reason, class string
			raw                 []byte
			err                 error
		}{c.class + "_read", "read", c.class, []byte(secret), fmt.Errorf("%s: %w", secret, c.err)})
	}
	for _, tc := range cases {
		for _, recheck := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recheck=%t", tc.name, recheck), func(t *testing.T) {
				prefix, reason := "observe_stat_", tc.reason
				if recheck {
					prefix = "observe_recheck_"
					if reason == "read" {
						reason = "read_errno"
					}
				}
				want := prefix + reason
				calls := 0
				read := func(string) ([]byte, error) { calls++; return tc.raw, tc.err }
				_, _, _, helperErr := nativeProcessStatFrom(100, read, recheck)
				identity := tc.err
				if identity == nil {
					identity = ErrContributionStopUnavailable
				}
				if calls != 1 || !errors.Is(helperErr, identity) || nativeFailureClass(helperErr) != tc.class || !strings.HasPrefix(helperErr.Error(), want+":") || strings.Contains(helperErr.Error(), "SECRET") {
					t.Fatalf("single snapshot/helper identity: calls=%d err=%v", calls, helperErr)
				}
				joinedErr := nativeStage("observe_stat", errors.Join(execution.ErrNativeQuiescenceUnavailable, helperErr))
				if joinedErr.stage != want || !errors.Is(joinedErr, identity) || !errors.Is(joinedErr, execution.ErrNativeQuiescenceUnavailable) {
					t.Fatal("tagged join lost identity/stage")
				}
				reads, opens, closes, signals := 0, 0, 0, 0
				cmd := exec.Command("/bin/true")
				joined := true
				err := runContributionNativeWithBarrier(context.Background(), execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
					func(context.Context) error { return nil }, func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil },
					func(ctx context.Context, group int) (bool, error) {
						return terminateContributionGroupFrom(ctx, 42, func(string) ([]os.DirEntry, error) { return contributionProcEntries(t)[:1], nil },
							func(string) ([]byte, error) {
								reads++
								if recheck && reads == 1 {
									return valid, nil
								}
								return tc.raw, tc.err
							},
							func(int, int) (int, error) { opens++; return 7, nil },
							func(int, unix.Signal, *unix.Siginfo, int) error { signals++; return nil },
							func(int) error { closes++; return nil })
					}, func(context.Context, int) error { return nil })
				var d *nativeFailureDiagnostic
				wantReads, wantFD := 1, 0
				if recheck {
					wantReads, wantFD = 2, 1
				}
				if joined || !errors.As(err, &d) || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || errors.Is(err, identity) || reads != wantReads || opens != wantFD || closes != wantFD || signals != 0 {
					t.Fatalf("denial/ordering: joined=%v reads=%d opens=%d closes=%d signals=%d err=%v", joined, reads, opens, closes, signals, err)
				}
				text := err.Error()
				if !strings.Contains(text, "first="+want+" class="+tc.class+" ") || strings.Contains(text, "SECRET") || strings.Contains(text, "private") || strings.Contains(text, "credential") {
					t.Fatal(text)
				}
			})
		}
	}
}

func TestNativeStatDiagnosticFallbackAndSuccess(t *testing.T) {
	if closedNativeStage("SECRET") != "unknown" || closedNativeStage("observe_stat_unknown") != "observe_stat_unknown" || closedNativeStage("observe_recheck_unknown") != "observe_recheck_unknown" {
		t.Fatal("unknown attributed to stat cause")
	}
	for _, recheck := range []bool{false, true} {
		calls := 0
		raw := contributionProcStat(42, "S")
		start, group, state, err := nativeProcessStatFrom(100, func(string) ([]byte, error) { calls++; return raw, nil }, recheck)
		s, g, st, e := contributionProcessStatFrom(100, func(string) ([]byte, error) { return raw, nil })
		if calls != 1 || err != e || start != s || group != g || state != st {
			t.Fatal("success semantics changed")
		}
	}
}
