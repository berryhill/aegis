//go:build linux

package manager

import (
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestProcessCustodyBindsExactLoopbackSocketOwner(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	custody, err := AcquireProcessCustody(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer custody.Close()
	bootID, err := currentBootID()
	if err != nil || custody.bootID != bootID {
		t.Fatalf("custody boot identity=%q current=%q err=%v", custody.bootID, bootID, err)
	}
	custody.bootID = "stale-boot-identity"
	if custody.Alive() || custody.Signal(0) == nil {
		t.Fatal("custody from another host boot remained authoritative")
	}
	custody.bootID = bootID

	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	if !custody.OwnsTCPConnection(accepted.RemoteAddr(), accepted.LocalAddr()) {
		t.Fatal("custody rejected a socket owned by the exact process")
	}
}

func TestProcessCustodyLivenessAndPidfdSignal(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exec sleep 30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	custody, err := AcquireProcessCustody(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	defer custody.Close()
	if !custody.Alive() {
		t.Fatal("new pidfd custody is not live")
	}
	if err = custody.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	deadline := time.Now().Add(time.Second)
	for custody.Alive() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if custody.Alive() {
		t.Fatal("pidfd custody remained live after process exit")
	}
}
