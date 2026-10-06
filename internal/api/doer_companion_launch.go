package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Open the running installed binary, not a browser-selected executable. Parent
// directories and the executable must not be writable by another identity.
func trustedDoerExecutable() (*os.File, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, os.ErrPermission
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, os.ErrPermission
	}
	for current := path; ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil {
			return nil, e
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (int(stat.Uid) != os.Geteuid() && stat.Uid != 0) || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid) != 0 {
			return nil, os.ErrPermission
		}
		if current == path && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
			return nil, os.ErrPermission
		}
		if current == "/" {
			break
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, e := f.Stat()
	running, re := os.Stat("/proc/self/exe")
	if e != nil || re != nil || !os.SameFile(opened, running) {
		f.Close()
		return nil, os.ErrPermission
	}
	return f, nil
}

type boundedCompanionOutput struct{ bytes.Buffer }

func (b *boundedCompanionOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, io.ErrShortBuffer
	}
	return b.Buffer.Write(p)
}

func launchDoerCompanion(ctx context.Context, socket, origin, id, bearer string, bind func(int), configFiles ...string) string {
	args := []string{"doer-approval-companion", "--socket", socket, "--origin", origin, "--intent-id", id, "--transport-fd", "3"}
	return launchProtectedDoerCompanion(ctx, args, bearer, bind, configFiles...)
}

func launchProtectedDoerCompanion(ctx context.Context, args []string, bearer string, bind func(int), configFiles ...string) string {
	if bearer == "" || len(bearer) > 4096 || strings.IndexFunc(bearer, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return "transport_unavailable"
	}
	executable, err := trustedDoerExecutable()
	if err != nil {
		return "companion_unavailable"
	}
	defer executable.Close()
	read, write, err := os.Pipe()
	if err != nil {
		return "transport_unavailable"
	}
	defer read.Close()
	defer write.Close()
	deadline, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// Descriptor 4 pins the validated inode across rename/path races. Descriptor 3
	// alone carries the bearer; neither argv, environment, stdout nor stderr does.
	// The caller selects one of the fixed product-owned companion commands.
	if len(configFiles) > 0 && configFiles[0] != "" {
		args = append(args, "--config", configFiles[0])
	}
	cmd := exec.CommandContext(deadline, "/proc/self/fd/4", args...)
	cmd.ExtraFiles = []*os.File{read, executable}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	cmd.Env = []string{}
	for _, key := range []string{"HOME", "PATH", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	out := &boundedCompanionOutput{}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return "companion_unavailable"
	}
	bind(cmd.Process.Pid)
	read.Close()
	if _, err = io.WriteString(write, bearer); err != nil {
		_ = cmd.Cancel()
		_ = cmd.Wait()
		return "transport_unavailable"
	}
	write.Close()
	err = cmd.Wait()
	if deadline.Err() != nil {
		if ctx.Err() == context.Canceled {
			return "cancelled"
		}
		return "no_decision"
	}
	if err != nil {
		return "unknown_response"
	}
	var response struct {
		Status string `json:"status"`
	}
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return "unknown_response"
	}
	switch response.Status {
	case "approved", "cancelled", "no_decision", "timeout", "unavailable", "dialog_unavailable", "denied", "expired", "transport_unavailable", "unknown_response", "policy_rejected", "transport_custody_rejected", "malformed_protocol", "response_drift", "sensitive_body_rejected", "outcome_unknown", "response_unverified":
		return response.Status
	default:
		return "unknown_response"
	}
}
