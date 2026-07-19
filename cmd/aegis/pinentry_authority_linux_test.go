//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAuthorityPinentryCreateAndNonTTYUnlockCLI(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	pinentry := filepath.Join(root, "fake-pinentry")
	countFile := filepath.Join(root, "pinentry-count")
	canary := "generated-authority-canary-7f4e2d91"
	buildTestBinary(t, binary)
	buildFakePinentry(t, pinentry, countFile, canary)
	configPath := filepath.Join(root, "installation", "aegis.yaml")
	statePath := filepath.Join(root, "installation", "state")

	process, master, slave := startAuthorityPTY(t, binary, configPath, statePath, pinentry)
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Create this configuration? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Select [1]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Create and verify this encrypted authority? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Passphrase-encrypted authority initialized, unlocked, and verified.", 15*time.Second)
	// Stop at the next ordinary onboarding stage without selecting, downloading,
	// certifying, or activating any runtime/model artifact.
	_, _ = master.Write([]byte{4})
	if err := process.Wait(); err != nil {
		t.Fatalf("create exit=%v output=%q", err, capture)
	}
	if bytes.Contains(capture, []byte(canary)) {
		t.Fatal("creation output contained passphrase canary")
	}
	for _, path := range []string{configPath, filepath.Join(statePath, "credentials", "authority.kek.enc"), filepath.Join(statePath, "credentials", "authority.db")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing artifact %s: %v", path, err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("artifact %s mode=%#o", path, info.Mode().Perm())
		}
	}
	assertCanaryAbsentBelow(t, filepath.Join(root, "installation"), canary)

	unlock := exec.Command(binary, "--config", configPath, "--pinentry-executable", pinentry, "secret", "list")
	unlock.Env = append(os.Environ(), "E2E_PROVIDER_CANARY=must-not-reach-pinentry")
	var stdout, stderr bytes.Buffer
	unlock.Stdout, unlock.Stderr = &stdout, &stderr
	if err := unlock.Run(); err != nil {
		t.Fatalf("unlock exit=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"count": 0`) {
		t.Fatalf("unlock output=%q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), canary) {
		t.Fatal("unlock output contained passphrase canary")
	}
	assertCanaryAbsentBelow(t, filepath.Join(root, "installation"), canary)
	count, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.Fields(count)) != 3 {
		t.Fatalf("pinentry requests=%d log=%q", len(bytes.Fields(count)), count)
	}
}

func TestAuthorityNoEchoPTYFallbackCLI(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	buildTestBinary(t, binary)
	configPath := filepath.Join(root, "fallback", "aegis.yaml")
	statePath := filepath.Join(root, "fallback", "state")
	process, master, slave := startAuthorityPTY(t, binary, configPath, statePath, "")
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Create this configuration? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Select [1]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Create and verify this encrypted authority? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	canary := "fallback-authority-canary-18d74c"
	capture = readPTYUntil(t, master, capture, "New authority passphrase", 5*time.Second)
	_, _ = master.Write([]byte(canary + "\r"))
	capture = readPTYUntil(t, master, capture, "Confirm authority passphrase", 5*time.Second)
	_, _ = master.Write([]byte(canary + "\r"))
	capture = readPTYUntil(t, master, capture, "Passphrase-encrypted authority initialized, unlocked, and verified.", 15*time.Second)
	_, _ = master.Write([]byte{4})
	if err := process.Wait(); err != nil {
		t.Fatalf("fallback create exit=%v output=%q", err, capture)
	}
	if bytes.Contains(capture, []byte(canary)) {
		t.Fatal("PTY fallback echoed passphrase canary")
	}
	assertCanaryAbsentBelow(t, filepath.Join(root, "fallback"), canary)
}

func TestAuthorityPinentryCancellationDoesNotMutateCLI(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	pinentry := filepath.Join(root, "cancel-pinentry")
	buildTestBinary(t, binary)
	buildCancelPinentry(t, pinentry)
	configPath := filepath.Join(root, "cancelled", "aegis.yaml")
	statePath := filepath.Join(root, "cancelled", "state")
	process, master, slave := startAuthorityPTY(t, binary, configPath, statePath, pinentry)
	defer master.Close()
	defer slave.Close()
	capture := readPTYUntil(t, master, nil, "Create this configuration? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Select [1]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "Create and verify this encrypted authority? [Y/n]:", 5*time.Second)
	_, _ = master.Write([]byte("\r"))
	capture = readPTYUntil(t, master, capture, "authority passphrase entry cancelled", 5*time.Second)
	if err := process.Wait(); err == nil {
		t.Fatalf("cancel unexpectedly succeeded output=%q", capture)
	}
	for _, path := range []string{filepath.Join(statePath, "credentials", "authority.kek.enc"), filepath.Join(statePath, "credentials", "authority.db")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cancel created %s: %v", path, err)
		}
	}
	configuration, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(configuration, []byte("passphrase-file")) {
		t.Fatal("cancel persisted authority configuration")
	}
}

func buildFakePinentry(t *testing.T, binary, countFile, canary string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "main.go")
	program := fmt.Sprintf(`package main
import("bufio";"fmt";"os";"strings")
func main(){
 if os.Getenv("E2E_PROVIDER_CANARY")!="" { os.Exit(91) }
 f,_:=os.OpenFile(%q,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600); fmt.Fprintln(f,"request"); f.Close()
 r:=bufio.NewReader(os.Stdin); fmt.Fprintln(os.Stdout,"OK fake")
 for { line,err:=r.ReadString('\n'); if err!=nil{return}; line=strings.TrimSpace(line)
  if strings.HasPrefix(line,"SET"){fmt.Fprintln(os.Stdout,"OK");continue}
  if line=="GETPIN"{fmt.Fprintln(os.Stdout,%q);fmt.Fprintln(os.Stdout,"OK");continue}
  if line=="BYE"{fmt.Fprintln(os.Stdout,"OK");return}
 }
}`, countFile, "D "+canary)
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", binary, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake pinentry: %v\n%s", err, output)
	}
}

func buildCancelPinentry(t *testing.T, binary string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "main.go")
	program := `package main
import("bufio";"fmt";"os";"strings")
func main(){r:=bufio.NewReader(os.Stdin);fmt.Fprintln(os.Stdout,"OK fake");for{line,err:=r.ReadString('\n');if err!=nil{return};line=strings.TrimSpace(line);if strings.HasPrefix(line,"SET"){fmt.Fprintln(os.Stdout,"OK")}else if line=="GETPIN"{fmt.Fprintln(os.Stdout,"ERR 83886179")}else if line=="BYE"{return}}}`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", binary, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build cancel pinentry: %v\n%s", err, output)
	}
}

func startAuthorityPTY(t *testing.T, binary, configPath, statePath, pinentry string) (*exec.Cmd, *os.File, *os.File) {
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
	arguments := []string{"--config", configPath, "--state-dir", statePath}
	if pinentry != "" {
		arguments = append(arguments, "--pinentry-executable", pinentry)
	}
	arguments = append(arguments, "init")
	command := exec.Command(binary, arguments...)
	command.Stdin, command.Stdout, command.Stderr = slave, slave, slave
	home := filepath.Join(filepath.Dir(configPath), "home")
	if pinentry == "" {
		command.Env = []string{"HOME=" + home, "PATH=" + filepath.Join(filepath.Dir(configPath), "empty-path"), "TERM=xterm-256color", "E2E_PROVIDER_CANARY=must-not-reach-pinentry"}
	} else {
		command.Env = append(os.Environ(), "HOME="+home, "E2E_PROVIDER_CANARY=must-not-reach-pinentry")
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	return command, master, slave
}

func assertCanaryAbsentBelow(t *testing.T, root, canary string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(value, []byte(canary)) {
			return fmt.Errorf("passphrase canary persisted in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
