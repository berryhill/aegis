package command

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Only closed public phase names and numeric durations cross the private-output
// boundary. Do not forward arbitrary child log lines or context error strings.
func isolatedFixturePublicPhase(phase string) bool {
	switch phase {
	case "build_old", "build_candidate", "archive", "healthy", "current-cli-stopped", "already-stopped", "interrupted-authority", "legacy-unit", "migration-failure-resume", "cancelled-auth", "unavailable-auth", "startup-failure-resume", "socket-denial", "open-writer-denial", "stale-image", "journal-tamper", "journal-link", "journal-identity":
		return true
	}
	return false
}

func isolatedFixtureChildTimings(output []byte) []string {
	var result []string
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[0] != "AEGIS_FIXTURE_TIMING_V1" || !isolatedFixturePublicPhase(fields[1]) || (fields[2] != "enter" && fields[2] != "finish") {
			continue
		}
		elapsed, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || elapsed < 0 || strconv.FormatInt(elapsed, 10) != fields[3] {
			continue
		}
		remaining, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil || remaining < 0 || strconv.FormatInt(remaining, 10) != fields[4] {
			continue
		}
		result = append(result, fmt.Sprintf("AEGIS_FIXTURE_TIMING_V1 %s %s %d %d", fields[1], fields[2], elapsed, remaining))
	}
	return result
}

func isolatedFixtureWritePhase(w io.Writer, phase, state string, origin, now, deadline time.Time) {
	if !isolatedFixturePublicPhase(phase) || (state != "enter" && state != "finish") {
		return
	}
	elapsed := now.Sub(origin)
	if elapsed < 0 {
		return
	}
	// The child test deadline already excludes the parent's cleanup reserve.
	// Subtracting it again would misreport the actual child budget by three seconds.
	remaining := deadline.Sub(now)
	if deadline.IsZero() || remaining < 0 {
		remaining = 0
	}
	fmt.Fprintf(w, "AEGIS_FIXTURE_TIMING_V1 %s %s %d %d\n", phase, state, elapsed.Nanoseconds(), remaining.Nanoseconds())
}

// Cleanup registration precedes fixture setup, so finish includes scenario
// cleanup, even when an assertion calls Fatal. A killed child may only emit enter.
func isolatedFixtureTrackPhase(t *testing.T, phase string) {
	t.Helper()
	deadline, _ := t.Deadline()
	isolatedFixtureWritePhase(os.Stderr, phase, "enter", isolatedFixtureClockOrigin, time.Now(), deadline)
	t.Cleanup(func() {
		isolatedFixtureWritePhase(os.Stderr, phase, "finish", isolatedFixtureClockOrigin, time.Now(), deadline)
	})
}

// Used for builds/archive, whose timing must finish before the next phase starts.
func isolatedFixtureBeginPhase(t *testing.T, phase string) func() {
	t.Helper()
	deadline, _ := t.Deadline()
	isolatedFixtureWritePhase(os.Stderr, phase, "enter", isolatedFixtureClockOrigin, time.Now(), deadline)
	return func() {
		isolatedFixtureWritePhase(os.Stderr, phase, "finish", isolatedFixtureClockOrigin, time.Now(), deadline)
	}
}
