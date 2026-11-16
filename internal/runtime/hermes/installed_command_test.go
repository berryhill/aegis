//go:build linux

package hermes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestControllerCodeLiteralMatchesPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"pass", "print('one')", `print("two")`, nativeCodexLoginProtocol} {
		cmd := exec.Command(python, "-I", "-c", "import sys; print(repr(sys.argv[1]),end='')", code)
		out, err := cmd.Output()
		if err != nil || string(out) != controllerCodeLiteral(code) {
			t.Fatal("controller program literal differs from supported public command", err)
		}
	}
}

func TestInstalledCodeCommandCompleteAndFresh(t *testing.T) {
	d, home, python := commandFixture(t, nil)
	// Launch protocol fixture only. Real PM generation acceptance is opt-in.
	script := "import json,sys; assert sys.argv[1:3] == ['--print-runtime-command','--installed-dependencies']; assert sys.argv[3] == '--code'; assert sys.argv[5] == '--'; print(json.dumps([" + controllerCodeLiteral(python) + ",'-I','-c'," + controllerCodeLiteral(installedBootstrap(d.Installation, "")) + "+'exec('+repr(sys.argv[4])+')',*sys.argv[6:]]))"
	launcher := "#!/bin/sh\nexec " + python + " -I -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' \"$@\"\n"
	if err := os.WriteFile(d.Executable, []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	args := []string{d.Installation, "resolve", "nc-" + strings.Repeat("a", 64)}
	g, err := discoverRuntimeCommand(context.Background(), d, home, nativeCodexLoginProtocol, args)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{python, "-I", "-c", installedBootstrap(d.Installation, "exec("+controllerCodeLiteral(nativeCodexLoginProtocol)+")")}, args...)
	if !g.modern || !reflect.DeepEqual(g.argv, want) {
		t.Fatal("code/bootstrap/selector argv changed")
	}
	if err := os.WriteFile(filepath.Join(d.Installation, "pm/installed_activation.py"), []byte("# drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(g.fresh(), errGatewayCommand) {
		t.Fatal("activation drift accepted")
	}
}

func TestNativeCommandValidationBeforeSourceAccess(t *testing.T) {
	d, _, _ := commandFixture(t, nil)
	if err := os.Remove(d.Executable); err != nil {
		t.Fatal(err)
	}
	_, err := runNativeCodexProtocol(context.Background(), d, "/nonexistent-source", "resolve", "")
	if !errors.Is(err, errGatewayCommand) {
		t.Fatal("source access preceded command validation", err)
	}
}
