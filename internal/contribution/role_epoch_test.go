package contribution

import "testing"

func TestContributionRoleStopUsesRetainedEpoch(t *testing.T) {
	r := RoleAuthority{Epoch: 7, Role: "builder", SessionID: "clean-session"}
	prior := Continuity{RoleEpochs: map[string]uint64{"builder": 7}, RoleAuthorities: map[string]RoleAuthority{"builder": r}}
	stop := RoleStop{Authority: r, Epoch: 7, PID: 42, ProcessStart: "exact"}
	event := ContinuityEvent{SessionID: r.SessionID, Kind: "role-stop-requested", RoleStop: &stop}
	next := prior
	if err := applyRoleStop(prior, event, &next); err != nil {
		t.Fatal(err)
	}
	stop.Epoch = 1
	if err := applyRoleStop(prior, event, &next); err == nil {
		t.Fatal("hardcoded epoch admitted")
	}
	if prior.RoleEpochs["builder"] != 7 {
		t.Fatal("stop reset epoch")
	}
}
