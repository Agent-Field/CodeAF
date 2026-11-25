package serve

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

var errBotRun = errors.New(botRunRefused)

// wakeRequestedTaskDoer keeps a bot's scope from being dropped by a new run
// while allowing it to start work that has a person as its doer.
func (h *Handler) wakeRequestedTaskDoer(r *http.Request, ws factory.OrgWorkstream, t factory.WorkstreamTask, starter string) error {
	if isBotCall(r) && factory.AgentDoes(t) {
		return errBotRun
	}
	return h.wakeTaskDoer(ws, t, starter)
}

// RUNNING MEANS A DOER IS AT WORK, ON THE SERVER TOO. A person who starts an
// Afi task here does not mark it running and hope: the server starts Afi on
// it in the task's own seat (runTaskTurn) inside the same write that marks it
// running, so the task says running only once Afi has taken it, and a start
// Afi cannot take leaves the task exactly where it was with the reason
// (live use, L1 F5 / L4 B6: tasks sat Running forever with nothing behind
// them). The agent's own writes through its door never wake anything: the
// run is already going.

// taskWords is what Afi is asked when it is started on t.
func taskWords(t factory.WorkstreamTask) string {
	words := strings.TrimSpace(t.Title)
	if req := strings.TrimSpace(t.Request); req != "" {
		words += "\n\n" + req
	}
	return words
}

// wakeTaskDoer starts t's doer as starter. It is called with the task's write
// still open, after the change is ready to keep, so a refusal keeps nothing.
//
// EVERY AGENT DOER STARTS HERE (E2 F7): Afi, a named agent or crew, and a
// chat's agent each run in the task's own seat on this server, from the
// task's brief. A chat's task used to be left Up next, because the chat lived
// on its person's computer; its task's run is the task's, never the chat's
// private conversation (org-space DESIGN §3.1).
func (h *Handler) wakeTaskDoer(ws factory.OrgWorkstream, t factory.WorkstreamTask, starter string) error {
	switch t.DoneBy.Kind {
	case factory.TaskByAfi:
		return h.runTaskTurn(ws, t, starter, taskWords(t))
	case factory.TaskByAgent:
		a, ok := h.workstreamAgent(ws, t.DoneBy.AgentID)
		if !ok {
			return errAgentNotHere
		}
		return h.runTaskTurn(ws, t, starter, agentTaskWords(a.Name, a.Brief, t))
	case factory.TaskByChat:
		return h.runTaskTurn(ws, t, starter, agentTaskWords(firstNonEmpty(t.DoneBy.Name, "the chat's agent"), "", t))
	}
	return nil
}

// wakeRefused reports whether err is a start the doer could not take.
func wakeRefused(err error) bool {
	return errors.Is(err, errTaskBusy) || errors.Is(err, errTaskNoRunner) || errors.Is(err, errAgentNotHere) || errors.Is(err, errBotRun)
}
