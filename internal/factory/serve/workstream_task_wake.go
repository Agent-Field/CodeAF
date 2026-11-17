package serve

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// RUNNING MEANS A DOER IS AT WORK, ON THE SERVER TOO. A person who starts an
// Afi task here does not mark it running and hope: the server starts Afi on
// it in the task's own seat (runTaskTurn) inside the same write that marks it
// running, so the task says running only once Afi has taken it, and a start
// Afi cannot take leaves the task exactly where it was with the reason
// (live use, L1 F5 / L4 B6: tasks sat Running forever with nothing behind
// them). The agent's own writes through its door never wake anything: the
// run is already going.

// errChatNotHere is the refusal for starting a chat's task on the server: a
// chat lives on a person's machine, and nothing here can wake it.
var errChatNotHere = errors.New("a chat's task starts from that chat; this server cannot wake it")

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
func (h *Handler) wakeTaskDoer(ws factory.OrgWorkstream, t factory.WorkstreamTask, starter string) error {
	switch t.DoneBy.Kind {
	case factory.TaskByAfi:
		return h.runTaskTurn(ws, t, starter, taskWords(t))
	case factory.TaskByChat:
		return errChatNotHere
	}
	return nil
}

// wakeRefused reports whether err is a start the doer could not take.
func wakeRefused(err error) bool {
	return errors.Is(err, errTaskBusy) || errors.Is(err, errTaskNoRunner) || errors.Is(err, errChatNotHere)
}
