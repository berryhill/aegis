package command

import "strings"

// This is reported only after Verify has proved the current installed image,
// authenticated audit-current readiness, Console reachability and Agent read.
func (j *installedJourney) VerifiedConsoleURL() string {
	return strings.TrimRight(j.plan.Origin, "/") + "/console"
}
