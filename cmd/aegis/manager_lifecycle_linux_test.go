//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/testprocess"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestManagerPTYLifecycleSignalsEOFAndExitAliases(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	build := exec.Command("go", "build", "-ldflags=-X=github.com/berryhill/aegis/internal/buildinfo.Version=test", "-o", binary, ".")
	if output, err := testprocess.CombinedOutput(build, 2*time.Minute); err != nil {
		t.Fatalf("build lifecycle fixture: %v\n%s", err, output)
	}
	for _, test := range []struct {
		name       string
		input      string
		signal     syscall.Signal
		wantReason string
		phrase     string
	}{
		{name: "sigint", signal: syscall.SIGINT, wantReason: "interrupt"},
		{name: "sigterm", signal: syscall.SIGTERM, wantReason: "termination"},
		{name: "eof", input: "\x04", wantReason: "terminal_eof"},
		{name: "slash_quit", input: "/quit\r", wantReason: "user_exit"},
		{name: "slash_exit", input: "/exit\r", wantReason: "user_exit"},
		{name: "plain_quit", input: "quit\r", wantReason: "user_exit"},
		{name: "plain_exit", input: "exit\r", wantReason: "user_exit"},
		{name: "containing_exit", phrase: "please explain exit behavior", wantReason: "user_exit"},
		{name: "containing_quit", phrase: "do not quit this sentence", wantReason: "user_exit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configPath := lifecycleConfig(t, filepath.Join(root, test.name))
			process, master, slave, initial := startManagerPTY(t, binary, configPath)
			defer master.Close()
			defer slave.Close()
			capture := readPTYUntil(t, master, nil, "[composer] > ", 5*time.Second)
			if test.phrase != "" {
				_, _ = master.Write([]byte(test.phrase + "\r"))
				capture = readPTYUntil(t, master, capture, "The local Aegis management model is unavailable (", 3*time.Second)
				select {
				case <-process.done:
					t.Fatal("phrase containing exit alias terminated manager")
				default:
				}
				// The response marker precedes the next Composer.Read call. Wait until
				// that call has entered raw mode before sending carriage return;
				// canonical input would translate it to the multiline Ctrl+J byte.
				capture = readPTYUntilCount(t, master, capture, "Enter submit; Ctrl+J newline", 2, 3*time.Second)
				_, _ = master.Write([]byte("exit\r"))
			} else if test.signal != 0 {
				if err := process.Process.Signal(test.signal); err != nil {
					t.Fatal(err)
				}
			} else {
				_, _ = master.Write([]byte(test.input))
			}
			wait := process.done
			deadline := time.Now().Add(5 * time.Second)
			buffer := make([]byte, 1024)
			poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
			for {
				ready, _ := unix.Poll(poll, 50)
				if ready > 0 {
					n, _ := master.Read(buffer)
					if n > 0 {
						capture = append(capture, buffer[:n]...)
					}
				}
				select {
				case <-wait:
					if err := process.Wait(); err != nil {
						t.Fatalf("manager exit: %v output=%q", err, capture)
					}
					goto exited
				default:
				}
				if time.Now().After(deadline) {
					_ = process.Process.Kill()
					t.Fatalf("manager did not exit: %q", capture)
				}
			}
		exited:
			output := string(capture)
			shutdown := "Shutting down Aegis manager (" + test.wantReason + ")."
			if strings.Count(output, shutdown) != 1 {
				t.Fatalf("shutdown count/output mismatch for %q: %q", shutdown, output)
			}
			if strings.Contains(output, "manager_scanner_failed") {
				t.Fatalf("cancellation became scanner failure: %q", output)
			}
			final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if final.Lflag&unix.ECHO != initial.Lflag&unix.ECHO {
				t.Fatal("terminal echo state changed")
			}
		})
	}
	t.Run("session_expiry", func(t *testing.T) {
		configPath := lifecycleConfigTTL(t, filepath.Join(root, "session-expiry"), "1s")
		process, master, slave, initial := startManagerPTY(t, binary, configPath)
		defer master.Close()
		defer slave.Close()
		capture := readPTYUntil(t, master, nil, "[composer] > ", 5*time.Second)
		capture = readPTYUntil(t, master, capture, "Shutting down Aegis manager (session_expired).", 3*time.Second)
		if err := process.Wait(); err != nil {
			t.Fatalf("expired manager exit=%v output=%q", err, capture)
		}
		if strings.Contains(string(capture), "manager_scanner_failed") {
			t.Fatalf("expiry became scanner failure: %q", capture)
		}
		final, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil || final.Lflag&unix.ECHO != initial.Lflag&unix.ECHO {
			t.Fatalf("terminal not restored on expiry: state=%+v err=%v", final, err)
		}
	})
}

func TestSecondSIGINTForcesTerminationDuringBlockedCleanup(t *testing.T) {
	if os.Getenv("AEGIS_SIGNAL_ESCALATION_HELPER") == "1" {
		ctx, stop := managerSignalContext()
		defer stop()
		fmt.Println("READY")
		<-ctx.Done()
		fmt.Println("CLEANING")
		time.Sleep(30 * time.Second)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestSecondSIGINTForcesTerminationDuringBlockedCleanup$")
	command.Env = append(os.Environ(), "AEGIS_SIGNAL_ESCALATION_HELPER=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = command.Stdout
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitLine := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("signal helper line=%q want=%q", got, want)
			}
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			t.Fatalf("timed out waiting for %s", want)
		}
	}
	waitLine("READY")
	if err = command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	waitLine("CLEANING")
	if err = command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("second SIGINT did not force signal termination: %v", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		t.Fatalf("second SIGINT status=%v", status)
	}
}

// managerProcess has exactly one exec.Cmd.Wait owner. Closing done publishes
// the result to every observer, including cleanup after a test timeout/FailNow.
type managerProcess struct {
	*exec.Cmd
	done chan struct{}
	err  error
}

func watchManager(command *exec.Cmd) *managerProcess {
	process := &managerProcess{Cmd: command, done: make(chan struct{})}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	return process
}

func (process *managerProcess) Wait() error {
	<-process.done
	return process.err
}

func (process *managerProcess) stop() {
	_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
	_ = process.Wait()
}

func TestManagerExclusiveWaiterCleanup(t *testing.T) {
	command := exec.Command("sh", "-c", "sleep 30 & wait")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := watchManager(command)
	t.Cleanup(process.stop)
	waiters := make(chan error, 8)
	for i := 0; i < cap(waiters); i++ {
		go func() { waiters <- process.Wait() }()
	}
	process.stop() // Simulate timeout cleanup while other callers wait.
	for i := 0; i < cap(waiters); i++ {
		select {
		case err := <-waiters:
			if err == nil || err != process.Wait() {
				t.Fatalf("inconsistent wait result: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("waiter did not join cleanup")
		}
	}
}

func openManagerPTY(t *testing.T) (*os.File, *os.File, *unix.Termios) {
	t.Helper()
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "ptmx")
	t.Cleanup(func() { _ = master.Close() })
	if err = unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return master, slave, initial
}

func startManagerPTY(t *testing.T, binary, configPath string) (*managerProcess, *os.File, *os.File, *unix.Termios) {
	t.Helper()
	master, slave, initial := openManagerPTY(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, binary, "--config", configPath, "manager")
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	command.WaitDelay = 2 * time.Second
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	command.Env = append(os.Environ(), "HOME="+filepath.Join(filepath.Dir(configPath), "home"), "XDG_CONFIG_HOME="+filepath.Join(filepath.Dir(configPath), "xdg-config"), "XDG_STATE_HOME="+filepath.Join(filepath.Dir(configPath), "xdg-state"))
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := watchManager(command)
	t.Cleanup(process.stop)
	return process, master, slave, initial
}

func readPTYUntil(t *testing.T, master *os.File, initial []byte, marker string, timeout time.Duration) []byte {
	t.Helper()
	if len(initial) > 1<<20 {
		t.Fatal("PTY output limit exceeded")
	}
	capture := bytes.NewBuffer(append([]byte(nil), initial...))
	deadline := time.Now().Add(timeout)
	poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for !strings.Contains(capture.String(), marker) {
		if time.Now().After(deadline) {
			t.Fatalf("PTY timeout waiting for %q output=%q", marker, capture.String())
		}
		ready, err := unix.Poll(poll, 50)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			t.Fatal(err)
		}
		if ready == 0 {
			continue
		}
		buffer := make([]byte, 1024)
		n, err := master.Read(buffer)
		if n > 0 {
			if capture.Len()+n > 1<<20 {
				t.Fatal("PTY output limit exceeded")
			}
			capture.Write(buffer[:n])
		}
		if err != nil {
			t.Fatalf("PTY read waiting for %q: %v output=%q", marker, err, capture.String())
		}
	}
	return capture.Bytes()
}

func lifecycleConfig(t *testing.T, root string) string {
	return lifecycleConfigTTL(t, root, "5m")
}

func lifecycleConfigTTL(t *testing.T, root, ttl string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "aegis.yaml")
	statePath := filepath.Join(root, "state")
	data := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: %s\naudit:\n  checkpoint_dir: %q\n", statePath, current.Uid, current.Username, ttl, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	initializeOperationalAuthority(t, statePath)
	return path
}
