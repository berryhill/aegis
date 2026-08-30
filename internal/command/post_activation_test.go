package command

import (
	"errors"
	"strings"
	"testing"
)

func TestEnterAegisAgentCallsOnlyAuthenticatedManager(t *testing.T) {
	calls := 0
	if err := enterAegisAgent(func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("manager calls=%d want=1", calls)
	}
}

func TestEnterAegisAgentPropagatesManagerFailure(t *testing.T) {
	want := errors.New("manager unavailable")
	if got := enterAegisAgent(func() error { return want }); !errors.Is(got, want) {
		t.Fatalf("error=%v want=%v", got, want)
	}
}

func TestActivationCompletesBeforeDirectAgentEntry(t *testing.T) {
	order := []string{}
	if err := activateAndEnterAegisAgent(func() error {
		order = append(order, "activate")
		return nil
	}, func() error {
		order = append(order, "manager")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "activate,manager" {
		t.Fatalf("order=%s want=activate,manager", got)
	}
}

func TestActivationFailurePreventsAgentEntry(t *testing.T) {
	want := errors.New("activation failed")
	managerCalled := false
	got := activateAndEnterAegisAgent(func() error { return want }, func() error {
		managerCalled = true
		return nil
	})
	if !errors.Is(got, want) || managerCalled {
		t.Fatalf("error=%v manager_called=%t", got, managerCalled)
	}
}
