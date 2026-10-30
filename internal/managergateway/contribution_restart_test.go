package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/initialize"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// This is the real owning Service/Badger/Queue/controller compiled into a test
// process, with deterministic Hermes protocol only. Not an installed daemon or
// live-provider qualification. No HTTP listener, ambient credentials or grants.
func TestContributionManagerHardRestartFrozenCandidateResume(t *testing.T) {
	scheduleContributionNativeJourney(t)
	if runtime.GOOS != "linux" {
		t.Skip("PIDFD recovery requires Linux")
	}
	for _, negative := range []bool{false, true} {
		name := "durable-frozen"
		if negative {
			name = "reserved-before-start-holds"
		}
		t.Run(name, func(t *testing.T) { contributionHardRestart(t, negative) })
	}
}

type restartSnapshot struct {
	Config     config.Config
	Key        string
	Contract   contribution.Contract
	Continuity contribution.Continuity
	Record     implementation.ContributionRecord
	Claim      queue.Claim
	Attempt    execution.Attempt
	Turns      string
	Workspace  os.FileInfo `json:"-"`
}

func configureRestartPrincipal(t *testing.T, cfg *config.Config, initialize bool) {
	current, e := user.Current()
	restartMust(t, e)
	cfg.Principal.UID = current.Uid
	cfg.Principal.Name = "Synthetic restart operator"
	cfg.Principal.User = current.Username
	if initialize {
		verifier, e := principalauth.Enroll(cfg.Principal.ID, []byte("synthetic-hard-restart-password"))
		restartMust(t, e)
		restartMust(t, principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier))
	}
}

func restartMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func restartWrite(t *testing.T, path string, value any) {
	t.Helper()
	b, e := json.Marshal(value)
	restartMust(t, e)
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	restartMust(t, e)
	_, e = f.Write(b)
	restartMust(t, e)
	restartMust(t, f.Sync())
	restartMust(t, f.Close())
}
func restartRead(t *testing.T, path string, value any) {
	t.Helper()
	b, e := os.ReadFile(path)
	restartMust(t, e)
	restartMust(t, json.Unmarshal(b, value))
}

func installRestartCheckpoint(t *testing.T, s *app.Service, repo fleet.Repository, worker *orchestration.QueueWorker, root string, ref reference.RevisionRef) {
	worker.SetContributionCheckpointObserver(func(ctx context.Context, cp implementation.ContributionCheckpoint) error {
		if cp.Stage != "freeze" || cp.Pass != 1 {
			return nil
		}
		sub, e := s.Authenticate(ctx)
		restartMust(t, e)
		key := app.ContributionIntentKey(ref, "https://github.com/fixture/controlled/issues/1", "")
		c, state, _, e := s.ObserveContributionContinuityAs(ctx, sub, key)
		restartMust(t, e)
		rec, e := s.ReadContributionExecutionAs(ctx, sub, key)
		restartMust(t, e)
		projection, e := repo.GetQueueProjection(ctx, c.QueueItemID)
		restartMust(t, e)
		claim, e := repo.GetClaim(ctx, projection.ActiveClaimID)
		restartMust(t, e)
		attempt, e := repo.GetAttempt(ctx, claim.AttemptID)
		restartMust(t, e)
		if rec.State != "phase_boundary" || rec.Frozen == nil || rec.Expected == nil || rec.Consumed != 1 || state.Stage != "freeze" || state.Used.Candidates != 1 || state.Used.QueueAttempts != 1 {
			t.Fatalf("not durable frozen: %+v %+v", rec, state)
		}
		if len(state.RoleAuthorities) != 2 {
			t.Fatalf("expected live controller and builder: %+v", state.RoleAuthorities)
		}
		for role, r := range state.RoleAuthorities {
			if r.BootID == "" || r.PID <= 0 || r.ProcessStart == "" || r.UID != uint32(os.Getuid()) || !restartExecuting(r) || len(state.NativeBoundaries[role].Bindings) == 0 || state.NativeBoundaries[role].Closed {
				t.Fatalf("missing live admission/coverage: %+v", r)
			}
		}
		if os.Getenv("AEGIS_RESTART_NEGATIVE") == "1" {
			// Typed host fault injection, no command Start or fabricated completion.
			// Extend the real producer inventory and leave its exact new intent RESERVED.
			r := state.RoleAuthorities["builder"]
			binding := contribution.NativeBinding{IntentID: "reserved-before-start", RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: claim.ClaimID, AttemptID: claim.AttemptID, Role: "builder", Epoch: r.Epoch, Authority: r.Ref(), Candidate: state.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: contribution.Digest([]byte("test-only-no-start"))}
			boundary := state.NativeBoundaries["builder"]
			boundary.Bindings = append(boundary.Bindings, binding)
			cr := repo.(fleet.ContributionContinuityRepository)
			for _, event := range []contribution.ContinuityEvent{{Kind: "native-boundary-extend", NativeBoundary: &boundary}, {Kind: "native-reserved", NativeEffect: &contribution.NativeEffect{Binding: binding, State: "reserved"}}} {
				event.SchemaVersion = contribution.ContinuitySchema
				event.QueueItemID = c.QueueItemID
				event.ContractDigest = c.Digest
				event.OwnerID = c.OwnerID
				event.SessionID = r.SessionID
				event.Sequence = state.Sequence + 1
				event.PreviousDigest = state.Digest
				event.At = s.Now()
				sealed, e := contribution.SealContinuityEvent(event)
				restartMust(t, e)
				state, e = cr.AppendContributionContinuity(ctx, sealed)
				restartMust(t, e)
			}
		}
		turns, e := os.ReadFile(filepath.Join(root, "turns"))
		restartMust(t, e)
		restartWrite(t, filepath.Join(root, "phase-ready.json"), restartSnapshot{Config: s.Config, Key: key, Contract: c, Continuity: state, Record: rec, Claim: claim, Attempt: attempt, Turns: string(turns)})
		// Deliberately never return: parent SIGKILLs this owning service while its
		// real workspace flock and stores are open. No deferred cleanup runs.
		for {
			time.Sleep(time.Hour)
		}
	})
}

// Helper is deliberately inert in ordinary default-test inventory.
func TestContributionManagerRestartChild(t *testing.T) {
	phase := os.Getenv("AEGIS_RESTART_PHASE")
	if phase == "" {
		return
	}
	root := os.Getenv("AEGIS_RESTART_ROOT")
	if !filepath.IsAbs(root) || filepath.Base(root) != "owned-restart" {
		t.Fatal("invalid owned fixture root")
	}
	if phase == "initial" {
		contributionManagerNativeRepair(t)
		t.Fatal("initial child returned without kill")
	}
	if phase != "resume" {
		t.Fatal("invalid child phase")
	}
	restartResume(t, root)
}

func contributionHardRestart(t *testing.T, negative bool) {
	root := filepath.Join(t.TempDir(), "owned-restart")
	restartMust(t, os.Mkdir(root, 0700))
	binary, e := os.Executable()
	restartMust(t, e)
	start := func(phase string) (*exec.Cmd, <-chan error) {
		// Match the owning service's private creation mask without mutating the
		// test runner's process-global umask. exec preserves the exact owned PID.
		cmd := exec.Command("/bin/sh", "-c", "umask 077; exec \"$@\"", "owned-service-fixture", binary, "-test.run=^TestContributionManagerRestartChild$", "-test.v", "-test.timeout=7m")
		cmd.Env = append(os.Environ(), "AEGIS_RESTART_ROOT="+root, "AEGIS_RESTART_PHASE="+phase)
		if negative {
			cmd.Env = append(cmd.Env, "AEGIS_RESTART_NEGATIVE=1")
		}
		log, e := os.OpenFile(filepath.Join(root, phase+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		restartMust(t, e)
		cmd.Stdout = log
		cmd.Stderr = log
		restartMust(t, cmd.Start())
		done := make(chan error, 1)
		go func() { e := cmd.Wait(); _ = log.Close(); done <- e }()
		return cmd, done
	}
	first, done := start("initial")
	firstExited := false
	var saved restartSnapshot
	t.Cleanup(func() {
		if !firstExited {
			_ = first.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("initial child Wait timeout")
			}
		}
		// Only exact boot/start/UID-bound old roles can be cleaned up. No process
		// scans or unbound group kills; never reset/rewrite canonical fixture state.
		if saved.Key != "" {
			adapter := hermes.New("unused-fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
			for _, r := range saved.Continuity.RoleAuthorities {
				if restartExecuting(r) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, e := adapter.RecoverContributionRole(ctx, hermes.ContributionStopBinding{SessionID: r.SessionID, RuntimeSessionID: r.RuntimeSessionID, AuthorityID: r.Authority.ID, AuthorityDigest: r.Authority.Digest, Role: r.Role, Epoch: r.Epoch, PID: r.PID, ProcessStart: r.ProcessStart}, r.BootID, r.UID)
					cancel()
					if e != nil {
						t.Errorf("preserve exact owned role %d for diagnosis: %v", r.PID, e)
					}
				}
			}
		}
	})
	timer := time.NewTimer(6 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
ready:
	for {
		select {
		case e := <-done:
			firstExited = true
			b, _ := os.ReadFile(filepath.Join(root, "initial.log"))
			t.Fatalf("service exited before durable freeze: %v\n%s", e, b)
		case <-timer.C:
			t.Fatal("timed out before durable freeze")
		case <-tick.C:
			if _, e := os.Stat(filepath.Join(root, "phase-ready.json")); e == nil {
				break ready
			}
		}
	}
	restartRead(t, filepath.Join(root, "phase-ready.json"), &saved)
	inode, e := os.Stat(saved.Record.Workspace)
	restartMust(t, e)
	restartMust(t, first.Process.Kill()) // Actual SIGKILL of exact owned service PID.
	select {
	case e := <-done:
		firstExited = true
		if e == nil || first.ProcessState.Success() {
			t.Fatal("hard kill not exercised")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGKILL Wait timed out")
	}
	for _, r := range saved.Continuity.RoleAuthorities {
		if !restartExecuting(r) {
			t.Fatalf("old independent role did not survive owning-service kill: %+v", r)
		}
	}
	second, resumed := start("resume")
	select {
	case e := <-resumed:
		b, _ := os.ReadFile(filepath.Join(root, "resume.log"))
		if e != nil {
			t.Fatalf("fresh service resume: %v\n%s", e, b)
		}
		t.Logf("fresh service: %s", b)
	case <-time.After(6 * time.Minute):
		_ = second.Process.Kill()
		select {
		case <-resumed:
		case <-time.After(5 * time.Second):
			t.Error("resume Wait timed out")
		}
		t.Fatal("fresh service timed out")
	}
	after, e := os.Stat(saved.Record.Workspace)
	restartMust(t, e)
	if !os.SameFile(inode, after) {
		t.Fatal("workspace replaced across restart")
	}
	var result string
	restartRead(t, filepath.Join(root, "result.json"), &result)
	expected := "accepted"
	if negative {
		expected = "reserved-held"
	}
	if result != expected {
		t.Fatalf("result %q", result)
	}
}

func restartExecuting(r contribution.RoleAuthority) bool {
	raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", r.PID))
	if e != nil {
		return false
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return false
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return false
	}
	return fields[0] != "Z" && fields[19] == r.ProcessStart
}

func restartResume(t *testing.T, root string) {
	ctx := context.Background()
	var old restartSnapshot
	restartRead(t, filepath.Join(root, "phase-ready.json"), &old)
	if os.Getenv("AEGIS_RESTART_DEBUG") == "1" {
		timer := time.AfterFunc(20*time.Second, func() {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			fmt.Fprintf(os.Stderr, "OWNED RECOVERY CHILD pid=%d\n%s", os.Getpid(), buf[:n])
			locks, _ := os.ReadFile("/proc/locks")
			_ = filepath.WalkDir(old.Config.StateDir, func(path string, d os.DirEntry, err error) error {
				if err == nil && d.Name() == "MAINTENANCE" {
					info, e := os.Stat(path)
					if e == nil {
						inode := info.Sys().(*syscall.Stat_t).Ino
						for _, line := range strings.Split(string(locks), "\n") {
							if strings.Contains(line, fmt.Sprintf(":%d ", inode)) {
								fmt.Fprintf(os.Stderr, "OWNED MAINTENANCE %s inode=%d lock=%s\n", path, inode, line)
							}
						}
					}
				}
				return nil
			})

		})
		defer timer.Stop()
	}
	recovered, e := initialize.RecoverInterruptedInstance(ctx, old.Config, []byte("synthetic-hard-restart-password"))
	restartMust(t, e)
	if !recovered.Recovered {
		t.Fatal("real crash did not require authenticated interrupted-authority recovery")
	}
	s := contributionAppAt(t, old.Config.StateDir, false)
	s.Config = old.Config
	// Fresh adapter starts with no in-memory processes. No source/git/charter or
	// approval receipt initialization and no counter recreation in this process.
	adapter := hermes.NewWithClock(s.Config.HermesExecutable, slog.New(slog.NewTextHandler(io.Discard, nil)), s.Now)
	s.Hermes = adapter
	repo, e := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	restartMust(t, e)
	defer repo.Close()
	routed, e := orchestration.NewRoutedRuntimeAdapter(adapter, s.Config.StateDir)
	restartMust(t, e)
	fs, e := orchestration.NewFleetService(repo, s.Authority, s.AuthorityCommands, func(_ context.Context, _ orchestration.FleetAction, sub core.Subject) error {
		return s.RequirePrincipal(sub)
	}, nil, s.Now)
	restartMust(t, e)
	verifier, e := evidence.NewBlobVerifier(s.Store)
	restartMust(t, e)
	worker, e := orchestration.NewQueueWorker(repo, fs, s.Store, verifier, routed, s.Now)
	restartMust(t, e)
	restartMust(t, s.ConfigureFleet(repo, fs, worker))
	restartMust(t, worker.ConfigureImplementation(s.Config.Implementation, s.Config.StateDir, adapter))
	ctx, sub, e := s.AuthenticateContributionUnixPeerRequest(ctx, contributionDeliveryConfiguredUID(t, s))
	restartMust(t, e)
	c, state, _, e := s.ObserveContributionContinuityAs(ctx, sub, old.Key)
	restartMust(t, e)
	rec, e := s.ReadContributionExecutionAs(ctx, sub, old.Key)
	restartMust(t, e)
	if !reflect.DeepEqual(c, old.Contract) || !reflect.DeepEqual(state, old.Continuity) || !reflect.DeepEqual(rec, old.Record) {
		t.Fatal("durable facts changed across actual kill/reopen")
	}
	gateway, e := New(ctx, s)
	restartMust(t, e)
	id, token := "fresh-hard-restart-manager", "isolated-restart-token"
	gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: sub, issuedAt: s.Now(), expires: sub.ExpiresAt, mode: "degraded"}
	result, runErr := gateway.Execute(ctx, sub, id, token, "/contribute resume "+old.Key)
	current, after, history, e := s.ObserveContributionContinuityAs(ctx, sub, old.Key)
	restartMust(t, e)
	projection, e := repo.GetQueueProjection(ctx, c.QueueItemID)
	restartMust(t, e)
	claim, e := repo.GetClaim(ctx, projection.ActiveClaimID)
	restartMust(t, e)
	attempt, e := repo.GetAttempt(ctx, claim.AttemptID)
	restartMust(t, e)
	items, e := repo.ListQueueItems(ctx)
	restartMust(t, e)
	if !reflect.DeepEqual(current, c) || claim != old.Claim || attempt != old.Attempt || projection.Attempts != 1 || len(items) != 1 {
		t.Fatal("resume replaced immutable contract/Queue/claim/attempt")
	}
	if os.Getenv("AEGIS_RESTART_NEGATIVE") == "1" {
		held, e := s.ReadContributionExecutionAs(ctx, sub, old.Key)
		restartMust(t, e)
		turns, e := os.ReadFile(filepath.Join(root, "turns"))
		restartMust(t, e)
		if runErr == nil || !reflect.DeepEqual(after, state) || !reflect.DeepEqual(held, rec) || string(turns) != old.Turns {
			t.Fatalf("unknown RESERVED mutated/replayed: %+v %v", result, runErr)
		}
		for _, r := range state.RoleAuthorities {
			if !restartExecuting(r) {
				t.Fatal("unknown RESERVED stopped old role or launched replacement")
			}
		}
		restartWrite(t, filepath.Join(root, "result.json"), "reserved-held")
		return
	}
	if runErr != nil {
		t.Fatalf("explicit fresh manager Resume: %+v %v", result, runErr)
	}
	final, e := s.ReadContributionExecutionAs(ctx, sub, old.Key)
	restartMust(t, e)
	if final.State != "candidate_accepted" || final.Consumed != 2 || len(final.Passes) != 2 || final.Passes[0].Candidate.Digest != old.Record.Frozen.Digest || final.Passes[0].Check.Outcome != "failed" || final.Passes[1].Check.Outcome != "passed" || after.Used.Candidates != 2 || after.Used.QueueAttempts != 1 || after.Used.RuntimeTurns != 4 {
		t.Fatalf("same frozen RED/repair/GREEN lost: %+v %+v", final, after)
	}
	// For each old role, the durable recovered PIDFD acknowledgement must precede
	// any successor admission. Parent also independently observed its survival.
	for role, r := range old.Continuity.RoleAuthorities {
		ack := false
		successor := false
		for _, ev := range history {
			if ev.Kind == "role-stop-acknowledged" && ev.RoleStop != nil && ev.RoleStop.Authority == r {
				if ev.RoleStop.ProofKind != "recovered-pidfd-exit.v1" || !ev.RoleStop.Acknowledged {
					t.Fatal("not recovered PIDFD proof")
				}
				ack = true
			}
			if ev.Kind == "role-admitted" && ev.RoleAuthority != nil && ev.RoleAuthority.Role == role && ev.RoleAuthority.Epoch > r.Epoch {
				if !ack {
					t.Fatal("successor before durable recovered stop")
				}
				successor = true
			}
		}
		if !ack || !successor || restartExecuting(r) {
			t.Fatalf("old role not excluded before successor: %s", role)
		}
	}
	candidate, e := s.AcceptedContributionCandidateAs(ctx, sub, old.Key)
	restartMust(t, e)
	controller := after.RoleAuthorities["controller"]
	restartMust(t, s.WithContributionContinuityCandidateAs(ctx, sub, controller.SessionID, old.Key, func(_ context.Context, held orchestration.ContributionCandidate) error {
		if held.Digest != candidate.Digest || held.Digest != final.Passes[1].Candidate.Digest {
			return fmt.Errorf("accepted custody drift")
		}
		return nil
	}))
	// Joined ordinary stop also bounds cleanup of fresh sessions.
	_, e = gateway.Execute(ctx, sub, id, token, "/contribute stop "+old.Key)
	restartMust(t, e)
	n, e := strconv.Atoi(old.Turns)
	restartMust(t, e)
	turns, e := os.ReadFile(filepath.Join(root, "turns"))
	restartMust(t, e)
	now, e := strconv.Atoi(string(turns))
	restartMust(t, e)
	if n != 1 || now != 4 {
		t.Fatalf("fixture turn counter reset/replayed: %d -> %d", n, now)
	}
	restartWrite(t, filepath.Join(root, "result.json"), "accepted")
	t.Log("actual owning Service SIGKILL -> same Badger/Queue/claim/attempt/frozen candidate -> fresh adapter PIDFD recovery and durable stop acknowledgements -> native RED/repair/GREEN and accepted custody; deterministic protocol, not live-provider proof")
}
