//go:build linux

package command

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestProtectedManagerIntakeDoesNotEchoOnPTY(t *testing.T) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "ptmx")
	defer master.Close()
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
	defer slave.Close()
	initialState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	random := make([]byte, 24)
	_, _ = rand.Read(random)
	canary := "pty-canary-" + hex.EncodeToString(random)
	result := make(chan []byte, 1)
	failures := make(chan error, 1)
	go func() {
		value, readErr := readSecret(cmd, false, "Secret value: ", "Confirm secret value: ")
		if readErr != nil {
			failures <- readErr
			return
		}
		result <- value
	}()
	var capture bytes.Buffer
	readUntil := func(marker string) {
		deadline := time.Now().Add(3 * time.Second)
		for !strings.Contains(capture.String(), marker) {
			_ = master.SetReadDeadline(deadline)
			buffer := make([]byte, 256)
			n, readErr := master.Read(buffer)
			if n > 0 {
				capture.Write(buffer[:n])
			}
			if readErr != nil {
				t.Fatalf("PTY read before %q: %v capture=%q", marker, readErr, capture.String())
			}
		}
	}
	readUntil("Secret value: ")
	_, _ = master.Write([]byte(canary + "\n"))
	readUntil("Confirm secret value: ")
	if strings.Contains(capture.String(), canary) {
		t.Fatal("first protected value was echoed")
	}
	_, _ = master.Write([]byte(canary + "\n"))
	select {
	case err = <-failures:
		t.Fatal(err)
	case value := <-result:
		if string(value) != canary {
			t.Fatal("protected intake changed the value")
		}
		for i := range value {
			value[i] = 0
		}
	case <-time.After(3 * time.Second):
		t.Fatal("protected intake timed out")
	}
	_ = master.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buffer := make([]byte, 256)
	n, _ := master.Read(buffer)
	capture.Write(buffer[:n])
	if strings.Contains(capture.String(), canary) {
		t.Fatal("protected value appeared in PTY capture")
	}
	finalState, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if finalState.Lflag&unix.ECHO != initialState.Lflag&unix.ECHO {
		t.Fatal("protected intake did not restore terminal echo state")
	}
}
