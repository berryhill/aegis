//go:build linux

package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// These are real child/kernel tests of the recovered-stop primitive. They do
// NOT qualify an owning-service crash, Queue reopen or whole Resume journey.
type recoveredCrashIdentity struct {
	Binding ContributionStopBinding
	Boot    string
	UID     uint32
}

// Real OS SIGKILL of the owning ADAPTER process, durable identity reopen in a
// fresh process. This is explicitly NOT the full app/Badger/Queue journey.
func TestContributionRecoveredAdapterCrash(t *testing.T) {
	path := os.Getenv("AEGIS_RECOVER_ADAPTER_FIXTURE")
	mode := os.Getenv("AEGIS_RECOVER_ADAPTER_MODE")
	if path != "" {
		if mode == "owner" {
			a, _, b := contributionStopChild(t, "exec sleep 60")
			boot, uid, err := a.ContributionProcessIdentity(b.RuntimeSessionID, b.PID, b.ProcessStart)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(recoveredCrashIdentity{b, boot, uid})
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			for {
				time.Sleep(time.Hour)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var identity recoveredCrashIdentity
		if err = json.Unmarshal(data, &identity); err != nil {
			t.Fatal(err)
		}
		proof, err := (&Adapter{}).RecoverContributionRole(context.Background(), identity.Binding, identity.Boot, identity.UID)
		if err != nil || proof.Kind() != "recovered-pidfd-exit.v1" {
			t.Fatalf("fresh adapter: %v %s", err, proof.Kind())
		}
		if err = os.WriteFile(path+".proof", []byte(proof.Kind()), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	path = filepath.Join(t.TempDir(), "identity.json")
	child := func(mode string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestContributionRecoveredAdapterCrash$")
		cmd.Env = append(os.Environ(), "AEGIS_RECOVER_ADAPTER_FIXTURE="+path, "AEGIS_RECOVER_ADAPTER_MODE="+mode)
		return cmd
	}
	owner := child("owner")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner identity deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// No graceful shutdown or cleanup runs in the owner process.
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err == nil {
		t.Fatal("expected SIGKILL")
	}
	if out, err := child("recover").CombinedOutput(); err != nil {
		t.Fatal(fmt.Sprintf("fresh process: %s %v", out, err))
	}
	proof, err := os.ReadFile(path + ".proof")
	if err != nil || string(proof) != "recovered-pidfd-exit.v1" {
		t.Fatalf("durable recovered proof %s %v", proof, err)
	}
}

func TestContributionRecoveredPIDFD(t *testing.T) {
	a, _, b := contributionStopChild(t, "exec sleep 60")
	boot, uid, err := a.ContributionProcessIdentity(b.RuntimeSessionID, b.PID, b.ProcessStart)
	if err != nil {
		t.Fatal(err)
	}
	fresh := &Adapter{}
	proof, err := fresh.RecoverContributionRole(context.Background(), b, boot, uid)
	if err != nil || !proof.RoleProcessAcknowledged() || proof.Binding() != b || proof.Kind() != "recovered-pidfd-exit.v1" || proof.BootID() != boot {
		t.Fatalf("%+v %v", proof, err)
	}
}

func TestContributionRecoveredPIDFDDenials(t *testing.T) {
	for _, kind := range []string{"wrong-boot", "reused-pid", "missing-start", "missing-boot", "wrong-uid", "still-owned", "absent-process"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := contributionStopChild(t, "exec sleep 60")
			boot, uid, err := a.ContributionProcessIdentity(b.RuntimeSessionID, b.PID, b.ProcessStart)
			if err != nil {
				t.Fatal(err)
			}
			fresh := &Adapter{}
			switch kind {
			case "wrong-boot":
				boot = "different-boot"
			case "reused-pid":
				b.ProcessStart = "different-start"
			case "missing-start":
				b.ProcessStart = ""
			case "missing-boot":
				boot = ""
			case "wrong-uid":
				uid = uint32(os.Getuid()) + 1
			case "still-owned":
				fresh = a
			case "absent-process":
				_ = p.cmd.Process.Kill()
				<-p.done
			}
			proof, err := fresh.RecoverContributionRole(context.Background(), b, boot, uid)
			if err == nil || proof.RoleProcessAcknowledged() {
				t.Fatalf("false ack %+v %v", proof, err)
			}
			if kind != "absent-process" {
				if err = p.cmd.Process.Signal(syscall.Signal(0)); err != nil {
					t.Fatal("unrelated live child signalled", err)
				}
			}
		})
	}
}
