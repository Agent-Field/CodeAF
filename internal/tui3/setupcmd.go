package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
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
// look at. Over a connection both methods ask the engine's machine, the one the
// tools run on (internal/remote's setup.go).
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
	far, ok := a.agent.(setupDoor)
	if !ok {
		a.note(setupNowhere)
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "":
		return a.withSetupPlan(far, a.showSetup)
	case "now":
		return a.withSetupPlan(far, func(plan machine.Report) tea.Cmd { return a.startSetup(far, plan) })
	}
	a.note(setupUsage)
	return nil
}

// withSetupPlan reads the plan off the update loop (over a connection it is a
// call to the engine's machine) and hands it to what the person asked for.
func (a *app) withSetupPlan(far setupDoor, then func(machine.Report) tea.Cmd) tea.Cmd {
	return a.offLoop(func() func(bool) tea.Cmd {
		plan, ok := far.SetupPlan()
		return func(here bool) tea.Cmd {
			switch {
			case !here:
				return nil
			case !ok:
				a.note(setupNowhere)
				return nil
			}
			return then(plan)
		}
	})
}

// showSetup prints the preflight lines and the one step that follows, and does
// nothing else. An impossible line is shown and is never attempted.
func (a *app) showSetup(plan machine.Report) tea.Cmd {
	for _, line := range plan.Lines() {
		a.note(line)
	}
	for _, line := range chatlist.SetupReasons(chatlist.SetupFacts{Missing: plan.Resume.MissingNames(), Running: plan.Resume.RunningLines()}) {
		a.note(line)
	}
	if plan.Idle() {
		a.note(setupAllHere)
		return nil
	}
	a.note("type /setup now to have the agent install what it can · it opens the network for that turn and asks first unless approvals are open")
	return nil
}

func (a *app) startSetup(door setupDoor, plan machine.Report) tea.Cmd {
	if plan.Idle() {
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
