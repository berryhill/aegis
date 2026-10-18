package command

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary is the inert protocol fixture, never a live native prompt.
func TestCompanionPinentryFixture(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--confirm-fixture" {
			index = i
			break
		}
	}
	if index < 0 || index+1 >= len(os.Args) {
		return
	}
	scenario := os.Args[index+1]
	capture := ""
	if index+2 < len(os.Args) {
		capture = os.Args[index+2]
	}
	fmt.Println("OK fixture")
	reader := bufio.NewScanner(os.Stdin)
	for reader.Scan() {
		line := reader.Text()
		if line == "GETPIN" {
			os.Exit(9)
		}
		if line == "BYE" {
			os.Exit(0)
		}
		if strings.HasPrefix(line, "SETDESC ") && strings.Contains(line, "\nCONFIRM") {
			os.Exit(10)
		}
		if line == "CONFIRM" {
			switch scenario {
			case "require-display":
				data, _ := os.ReadFile(capture)
				if strings.Contains(string(data), "OPTION display=:77\n") {
					fmt.Println("OK")
				} else {
					fmt.Println("ERR 83886179 cancelled")
				}
			case "approve":
				fmt.Println("OK")
			case "cancel":
				fmt.Println("ERR 83886179 cancelled")
			case "deny":
				fmt.Println("ERR 114 not confirmed")
			case "none":
				os.Exit(0)
			case "data":
				fmt.Println("D password-canary")
				fmt.Println("OK")
			case "unknown":
				fmt.Println("UNKNOWN secret-canary")
			case "oversize":
				fmt.Println(strings.Repeat("X", pinentryLineLimit+1))
			case "timeout":
				time.Sleep(time.Minute)
			}
		} else {
			if capture != "" {
				file, err := os.OpenFile(capture, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err == nil {
					_, _ = fmt.Fprintln(file, line)
					_ = file.Close()
				}
			}
			fmt.Println("OK")
		}
	}
	os.Exit(0)
}

func TestCompanionNativeConfirmation(t *testing.T) {
	for _, tc := range []struct{ scenario, want string }{{"approve", "confirmed"}, {"cancel", "cancelled"}, {"deny", "denied"}, {"none", "no_decision"}, {"data", "malformed_protocol"}, {"unknown", "malformed_protocol"}, {"oversize", "malformed_protocol"}, {"timeout", "timeout"}} {
		t.Run(tc.scenario, func(t *testing.T) {
			service := newNativeConfirmation()
			service.timeout = 2 * time.Second
			if tc.scenario == "timeout" {
				service.timeout = 50 * time.Millisecond
			}
			service.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompanionPinentryFixture$", "--", "--confirm-fixture", tc.scenario)
			}
			// Percent/newline are encoded as data in one SETDESC record, never commands.
			got := service.run(context.Background(), "/fixture", "UNTRUSTED DATA: task\nCONFIRM\nGETPIN %0A")
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
func TestConfirmationGraphicalContextWithoutTTY(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "protocol")
	service := newNativeConfirmation()
	service.getenv = func(name string) string {
		if name == "DISPLAY" {
			return ":77"
		}
		return ""
	}
	service.command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompanionPinentryFixture$", "--", "--confirm-fixture", "require-display", capture)
	}
	if got := service.run(context.Background(), "/fixture", "Review exact scope"); got != "confirmed" {
		t.Fatal(got)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "OPTION display=:77\n") || strings.Contains(string(data), "GETPIN") {
		t.Fatalf("transcript=%q", data)
	}
}

func TestCompanionConfirmationRejectsUnsafeData(t *testing.T) {
	for _, data := range []string{"", strings.Repeat("x", pinentryLineLimit), "task\x00CONFIRM", "task\rCONFIRM", "task\u202eOK", string([]byte{0xff})} {
		if confirmationDataSafe(data) {
			t.Fatal("unsafe description accepted")
		}
	}
	service := newNativeConfirmation()
	service.getenv = func(string) string { return "" }
	if got := service.confirm(context.Background(), "data"); got != "unavailable" {
		t.Fatal(got)
	}
}
