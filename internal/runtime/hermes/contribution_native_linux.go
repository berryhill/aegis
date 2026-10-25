//go:build linux

package hermes

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"os/exec"
	"syscall"
	"time"
)

// NativeCompletion is minted only by the exact owned Cmd.Wait and group-empty join.
// It proves quiescence, NOT command success, authority, or source custody release.
type NativeCompletion struct {
	binding contribution.NativeBinding
	joined  bool
}

func (p NativeCompletion) Binding() contribution.NativeBinding { return p.binding }
func (p NativeCompletion) Joined() bool                        { return p.joined }
func NativeCommandDigest(cmd *exec.Cmd) string {
	if cmd == nil {
		return ""
	}
	b, _ := json.Marshal(struct {
		Path string
		Args []string
		Dir  string
		Env  []string
	}{cmd.Path, cmd.Args, cmd.Dir, cmd.Env})
	return contribution.Digest(b)
}

// RunContributionNative accepts only an already constructed/qualified internal
// command. Reserve must durably fence it before Start. Finish is persistence-only.
// No shell, command parser, approval, successor or HTTP route is introduced here.
func RunContributionNative(ctx context.Context, b contribution.NativeBinding, cmd *exec.Cmd, reserve func(context.Context) error, finish func(context.Context, NativeCompletion) error) error {
	if cmd == nil || cmd.Process != nil || cmd.SysProcAttr != nil || b.CommandDigest != NativeCommandDigest(cmd) || reserve == nil || finish == nil {
		return contribution.ErrContinuityConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reserve(ctx); err != nil {
		return err
	}
	complete := func(p NativeCompletion, cause error) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if err := finish(cleanup, p); err != nil {
			return err
		}
		return cause
	}
	if err := ctx.Err(); err != nil {
		return complete(NativeCompletion{binding: b}, err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return complete(NativeCompletion{binding: b}, err)
	}
	group := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-group, syscall.SIGKILL)
		waitErr = <-done
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		empty, err := contributionGroupEmpty(group)
		if err != nil {
			return complete(NativeCompletion{binding: b}, err)
		}
		if empty {
			return complete(NativeCompletion{binding: b, joined: true}, waitErr)
		}
		select {
		case <-cleanup.Done():
			return complete(NativeCompletion{binding: b}, contribution.ErrNativeQuiescenceUnavailable)
		case <-tick.C:
		}
	}
}
