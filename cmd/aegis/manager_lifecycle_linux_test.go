//go:build linux

package main

import (
	"bufio"
	"bytes"
	"fmt"
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
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
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
		{name: "slash_quit", input: "/quit\n", wantReason: "user_exit"},
		{name: "slash_exit", input: "/exit\n", wantReason: "user_exit"},
		{name: "plain_quit", input: "quit\n", wantReason: "user_exit"},
		{name: "plain_exit", input: "exit\n", wantReason: "user_exit"},
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
				_, _ = master.Write([]byte(test.phrase + "\n"))
				capture = readPTYUntil(t, master, capture, "The local Aegis management model is unavailable (", 3*time.Second)
				if process.ProcessState != nil {
					t.Fatal("phrase containing exit alias terminated manager")
				}
				_, _ = master.Write([]byte("exit\n"))
			} else if test.signal != 0 {
				if err := process.Process.Signal(test.signal); err != nil {
					t.Fatal(err)
				}
			} else {
				_, _ = master.Write([]byte(test.input))
			}
			wait := make(chan error, 1)
			go func() { wait <- process.Wait() }()
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
				case err := <-wait:
					if err != nil {
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

func startManagerPTY(t *testing.T, binary, configPath string) (*exec.Cmd, *os.File, *os.File, *unix.Termios) {
	t.Helper()
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "ptmx")
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
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--config", configPath, "manager")
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	command.Env = append(os.Environ(), "HOME="+filepath.Join(filepath.Dir(configPath), "home"), "XDG_CONFIG_HOME="+filepath.Join(filepath.Dir(configPath), "xdg-config"), "XDG_STATE_HOME="+filepath.Join(filepath.Dir(configPath), "xdg-state"))
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	return command, master, slave, initial
}

func readPTYUntil(t *testing.T, master *os.File, initial []byte, marker string, timeout time.Duration) []byte {
	t.Helper()
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
	data := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: %s\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, ttl, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
