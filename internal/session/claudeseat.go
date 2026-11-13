package session

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/claudemodel"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// claudeProgram is the program a task seated on Claude Code runs as
// (internal/claudecode).
const claudeProgram = "claude"

// seatOnClaudeCode sends a task to the `claude` program when the person seated
// the worker on Claude Code and the proposal named no program of its own.
//
// THE SEAT DECIDES, NOT THE MODEL THAT PROPOSED. A conversation on any model
// may propose the task; who does it is the worker seat in Settings, and a
// worker seat on Claude Code means Claude Code doing the work in its own
// session, in its own folder on a branch, with the person's words reaching it
// while it works. Its model word rides along as the task's model so the program
// starts the Claude the person picked.
//
// It holds only where a program may be handed work at all: inside a task, and
// where no run road is linked, the task stays codeaf's own.
func (a *Agent) seatOnClaudeCode(spec taskSpec) taskSpec {
	if spec.via != "" || !a.mayHandToProgram() {
		return spec
	}
	if _, err := a.delegateFor(claudeProgram); err != nil {
		return spec
	}
	pin, ok := config.CrewPinAt(a.config.ProfileDir, crewroute.Worker)
	if !ok || !strings.EqualFold(pin.Provider, claudemodel.ID) && !strings.HasPrefix(pin.Model, claudemodel.ID+"/") {
		return spec
	}
	spec.via = claudeProgram
	return spec
}
