package remote

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── PREPARING THE MACHINE THE TOOLS RUN ON ──────────────────────────────────
//
// /setup reads what a conversation has used against the device it runs on and,
// on /setup now, starts a turn in which the agent installs what is missing
// (internal/session's setup.go). Over a connection the device is the ENGINE's —
// the machine that runs the tools — so the plan is read there and the setup
// turn runs there; the viewer's own machine is not the one being prepared.
//
// The plan is a call and the turn is a stream opener, on the doors beside them
// ([MethodSkillShelf] and [MethodSubmitBash]); the turn's output arrives like
// any turn's. The capability is the welcome's to answer ([Welcome.Setup]).

// setupSaid is the line the room records for a setup turn.
const setupSaid = "set up this machine"

// setupDoor is the slice of *session.Agent this file speaks to, asserted on
// [effortDoor]'s terms.
type setupDoor interface {
	SetupPlan() (preflight.Report, bool)
	SubmitSetup(ctx context.Context) (<-chan session.Event, error)
}

func setupKnown(agent any) bool { _, ok := agent.(setupDoor); return ok }

// errNoFarSetup is what the engine answers when its agent has no setup doors.
var errNoFarSetup = errors.New("engine: this session cannot set up its machine; update the engine and reconnect")

// SetupPlanReply is the engine machine's report, and whether it has a machine
// to look at (a session with no cell has none).
type SetupPlanReply struct {
	Report preflight.Report `json:"report"`
	Ok     bool             `json:"ok"`
}

// SetupSupported answers for THE MACHINE AT THE OTHER END, off what it said at
// the door.
func (a *Agent) SetupSupported() bool { return a.c.Welcome().Setup }

// SetupPlan is the preflight of the engine's machine, and false where the far
// conversation has none or cannot be asked.
func (a *Agent) SetupPlan() (preflight.Report, bool) {
	if !a.SetupSupported() {
		return preflight.Report{}, false
	}
	payload, err := a.c.call(nil, MethodSetupPlan, nil)
	if err != nil {
		return preflight.Report{}, false
	}
	var reply SetupPlanReply
	if json.Unmarshal(payload, &reply) != nil {
		return preflight.Report{}, false
	}
	return reply.Report, reply.Ok
}

// SubmitSetup runs the setup turn on the engine's machine and streams it.
func (a *Agent) SubmitSetup(ctx context.Context) (<-chan session.Event, error) {
	if !a.SetupSupported() {
		return nil, errNoFarSetup
	}
	return a.open(ctx, MethodSubmitSetup, nil)
}

func serveSetupPlan(agent any) (json.RawMessage, error) {
	door, ok := agent.(setupDoor)
	if !ok {
		return nil, errNoFarSetup
	}
	report, has := door.SetupPlan()
	return json.Marshal(SetupPlanReply{Report: report, Ok: has})
}

func submitSetup(agent any) (<-chan session.Event, error) {
	door, ok := agent.(setupDoor)
	if !ok {
		return nil, errNoFarSetup
	}
	return door.SubmitSetup(context.Background())
}
