# Narrow native-command join (377)

`app.runContributionNativeCommandAs(ctx, subject, session, key, binding, cmd)` is
an internal integration point for 376 after source ownership/command qualification.
It adds no HTTP or shell route. Keep the accepted-source lock throughout it.
`binding.CommandDigest = hermes.NativeCommandDigest(cmd)`; policy digest is the
sealed contract TemplateDigest. Explicitly qualify command path, args, working
directory and environment before constructing it.

Reservation/completion/unknown append to the existing fleet continuity 0x78 CAS
chain. Bindings retain run, Queue, original claim/attempt, role authority/epoch,
candidate, policy, command and intent IDs. Original RuntimeBinding/claim and
Queue counters are not rewritten. A repeated intent cannot launch twice.
Reservation currently requires the original active unexpired claim; this is NOT
a fresh-session continuation lease. A future v6-only continuation-epoch bridge
must explicitly replace that admission check without widening historical leases.

Only the real owned Cmd.Wait plus process-group-empty observation yields private
NativeCompletion.Joined. Completed means quiescent, not successful: nonzero exits
still return the execution error. Missing executable/uncertain join is unknown;
unknown blocks subsequent command admission. No native fact enables successor
role admission or remote effects. Caller flags cannot release custody.

Role-stop safety cleanup detaches from caller cancellation only AFTER a durable
stop request and is bounded to five seconds, including canonical acknowledgement
CAS retries. The original stop adapter's four-second bound remains. Persistence
failure leaves a pending/unknown stop; PID absence is never acknowledged.
