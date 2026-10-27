package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

// Only continuity readback is controlled. Sessions and authority projections use
// the real lightweight canonical authority store from testService. This is not
// native Wait/stop qualification or a production contribution success fixture.
type roleCloseContinuityReader struct {
	fleet.Repository
	fleet.ContributionContinuityRepository
	state contribution.Continuity
	err   error
}

func (r *roleCloseContinuityReader) GetContributionContinuity(context.Context, string) (contribution.Continuity, []contribution.ContinuityEvent, error) {
	return r.state, nil, r.err
}
func (r *roleCloseContinuityReader) AppendContributionContinuity(context.Context, contribution.ContinuityEvent) (contribution.Continuity, error) {
	panic("cleanup reconciliation must not mutate continuity")
}

func TestContributionRoleCloseJoined(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cc, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "ready", core.Receipt{ID: "ready", CharterDigest: cc.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	mandate, _, err := s.PreviewSession(ctx, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.StartSession(ctx, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Hermes.Terminate(context.Background(), original.RuntimeSessionID, false) })
	authority, err := s.authorityContextForSession(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := contribution.Contract{QueueItemID: "controlled-queue", Digest: "controlled-contract"}
	role := contribution.RoleAuthority{Role: "builder", Epoch: 1, SessionID: original.ID, MandateID: original.Mandate.ID, RuntimeSessionID: original.RuntimeSessionID, Authority: reference.DigestRef{ID: authority.ID, Digest: authority.Digest}}
	binding := contribution.NativeBinding{IntentID: "controlled-native", Role: "builder", Epoch: 1, Authority: role.Ref()}
	base := contribution.Continuity{ContractDigest: c.Digest,
		RoleAuthorities:  map[string]contribution.RoleAuthority{"builder": role},
		RoleStops:        map[string]contribution.RoleStop{"builder": {Authority: role, Epoch: 1, Acknowledged: true}},
		NativeBoundaries: map[string]contribution.NativeBoundary{"builder": {Role: "builder", Epoch: 1, Closed: true, Bindings: []contribution.NativeBinding{binding}}},
		NativeEffects:    map[string]contribution.NativeEffect{binding.IntentID: {Binding: binding, State: "completed"}}}
	reader := &roleCloseContinuityReader{Repository: s.FleetRepository, state: base}
	s.FleetRepository = reader
	if s.contributionRoleCloseJoined(ctx, c, original, "builder") {
		t.Fatal("running session accepted")
	}
	// Controlled terminal session with still-active canonical authority must deny.
	terminal := original
	now := s.Now()
	terminal.Status = "stopped"
	terminal.EndedAt = &now
	if err = s.Store.Save("sessions", terminal.ID, terminal); err != nil {
		t.Fatal(err)
	}
	if s.contributionRoleCloseJoined(ctx, c, original, "builder") {
		t.Fatal("active authority accepted")
	}
	// Restore before the actual canonical terminal transition.
	if err = s.Store.Save("sessions", original.ID, original); err != nil {
		t.Fatal(err)
	}
	if err = s.endSession(ctx, original.ID, "stopped", "contribution_role_finished", true); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*contribution.Continuity, *core.Session)
		want   bool
	}{
		{name: "exact stopped cleanup", want: true},
		{name: "wrong contract", change: func(st *contribution.Continuity, _ *core.Session) { st.ContractDigest = "other" }},
		{name: "missing role", change: func(st *contribution.Continuity, _ *core.Session) { delete(st.RoleAuthorities, "builder") }},
		{name: "wrong session", change: func(_ *contribution.Continuity, o *core.Session) { o.ID = "other" }},
		{name: "wrong mandate", change: func(_ *contribution.Continuity, o *core.Session) { o.Mandate.ID = "other" }},
		{name: "wrong runtime", change: func(_ *contribution.Continuity, o *core.Session) { o.RuntimeSessionID = "other" }},
		{name: "wrong pid", change: func(_ *contribution.Continuity, o *core.Session) { o.RuntimePID++ }},
		{name: "wrong process start", change: func(_ *contribution.Continuity, o *core.Session) { o.ProcessStart = "other" }},
		{name: "missing stop", change: func(st *contribution.Continuity, _ *core.Session) { delete(st.RoleStops, "builder") }},
		{name: "unacknowledged stop", change: func(st *contribution.Continuity, _ *core.Session) {
			v := st.RoleStops["builder"]
			v.Acknowledged = false
			st.RoleStops["builder"] = v
		}},
		{name: "open boundary", change: func(st *contribution.Continuity, _ *core.Session) {
			v := st.NativeBoundaries["builder"]
			v.Closed = false
			st.NativeBoundaries["builder"] = v
		}},
		{name: "missing native", change: func(st *contribution.Continuity, _ *core.Session) { delete(st.NativeEffects, binding.IntentID) }},
		{name: "unknown native", change: func(st *contribution.Continuity, _ *core.Session) {
			v := st.NativeEffects[binding.IntentID]
			v.State = "unknown"
			st.NativeEffects[binding.IntentID] = v
		}},
		{name: "wrong authority digest", change: func(st *contribution.Continuity, _ *core.Session) {
			v := st.RoleAuthorities["builder"]
			v.Authority.Digest = "other"
			st.RoleAuthorities["builder"] = v
			stop := st.RoleStops["builder"]
			stop.Authority = v
			st.RoleStops["builder"] = stop
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			var state contribution.Continuity
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			o := original
			if tc.change != nil {
				tc.change(&state, &o)
			}
			reader.state = state
			if got := s.contributionRoleCloseJoined(ctx, c, o, "builder"); got != tc.want {
				t.Fatalf("joined=%t want=%t", got, tc.want)
			}
		})
	}
	reader.state = base
	if s.contributionRoleCloseJoined(nil, c, original, "builder") {
		t.Fatal("nil context accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if s.contributionRoleCloseJoined(cancelled, c, original, "builder") {
		t.Fatal("cancelled context accepted")
	}
	reader.err = errors.New("controlled read failure")
	if s.contributionRoleCloseJoined(ctx, c, original, "builder") {
		t.Fatal("read failure accepted")
	}
	after, err := s.GetSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if core.Digest(before) != core.Digest(after) || core.Digest(projection) != core.Digest(next) {
		t.Fatal("reconciliation mutated terminal session or authority")
	}
}
