package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// /setup is "prepare this machine" (docs/ARCHITECTURE.md 8.4): it runs the
// preflight of this device against what the conversation has used, says what it
// found, and, when the person types /setup now, starts the setup turn in which
// the agent installs the installable lines. It follows /cache's guard: the bare
// word shows, the typed second word acts.

// setupDoor is the narrow slice of the session /setup needs, asserted on the
// agent rather than added to [Agent]: a session with no cell has no machine to
// look at, and a far engine has neither method.
type setupDoor interface {
	SetupPlan() (machine.Report, bool)
	SubmitSetup(ctx context.Context) (<-chan session.Event, error)
}

const (
	setupNowhere = "this conversation has no machine of its own to set up"
	setupUsage   = "/setup takes now, or nothing · /setup shows what this machine lacks"
	setupAllHere = "everything this conversation uses is already on this machine — nothing to set up"
)

func (a *app) runSetupCommand(rest string) tea.Cmd {
	door, ok := a.agent.(setupDoor)
	if !ok || a.hosted() {
		a.note(setupNowhere)
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "":
		a.showSetup(door)
		return nil
	case "now":
		return a.startSetup(door)
	}
	a.note(setupUsage)
	return nil
}

// showSetup prints the preflight lines and the one step that follows, and does
// nothing else. An impossible line is shown and is never attempted.
func (a *app) showSetup(door setupDoor) {
	plan, ok := door.SetupPlan()
	if !ok {
		a.note(setupNowhere)
		return
	}
	pending := len(plan.Pending()) > 0
	for _, line := range plan.Lines() {
		a.note(line)
	}
	if !pending {
		a.note(setupAllHere)
		return
	}
	a.note("type /setup now to have the agent install what it can · it opens the network for that turn and asks first unless approvals are open")
}

func (a *app) startSetup(door setupDoor) tea.Cmd {
	if plan, _ := door.SetupPlan(); len(plan.Pending()) == 0 {
		a.note(setupAllHere)
		return nil
	}
	if a.parking() {
		a.note("wait for the answer to finish, or stop it, before setting up this machine")
		return nil
	}
	const said = "set up this machine"
	ctx := a.ctx
	return a.submittingShown(said, said, func() (<-chan session.Event, error) { return door.SubmitSetup(ctx) })
}
