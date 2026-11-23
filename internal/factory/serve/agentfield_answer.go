package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// ── A PERSON ANSWERS AN AGENTFIELD RUN'S PAUSE ON THIS FACTORY ──────────────
//
// A job whose AgentField run paused for a person (needs_you.question.kind
// agentfield_approval) is answered with the job verbs every surface already
// has: approve, request_changes with words, reject_plan. THIS FACTORY HOLDS
// THE JOB, SO IT DECIDES WHO MAY ANSWER AND KEEPS WHAT THEY SAID; the runner
// that started the run carries the answer to the control plane, because that
// runner alone holds the control plane's key and is the run's owner there
// (lease.Doors.AnswerAgentField). So a person answers from any desk attached
// to any factory — one on this machine, one on another computer, an
// organization's on a server — and the answer reaches the run the same way.

// The refusals an answer to a run's pause can meet here.
const (
	agentFieldNeedsWords = "say what to change: Ask for changes sends your words to the run"
	agentFieldTooLong    = "keep your words under 4000 characters"
	agentFieldMoved      = "the run is no longer waiting for this answer"
	agentFieldIsApproval = "this is the AgentField run's approval: answer it with Approve, Ask for changes or Reject"
)

// agentFieldWordsMost bounds the words a person sends a run.
const agentFieldWordsMost = 4000

// agentFieldAnswer puts the AgentField branch in front of a job verb: a job
// that waits on a run's pause is answered with decision, and every other job
// goes to next exactly as before (refused as next's route would be when this
// factory does not provide it).
func (h *Handler) agentFieldAnswer(decision string, next jobAction, nextExists bool) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if !factory.AgentFieldPause(it) {
			if h.requireVerb(w, nextExists) {
				next(w, r, it)
			}
			return
		}
		h.answerAgentField(w, r, it, decision)
	}
}

// notRunAnswer refuses a step's answer (POST answer) to a job that waits on
// an AgentField run's pause: that pause is the run's approval, answered with
// the three verbs above, never as a step's question.
func (h *Handler) notRunAnswer(next jobAction) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if factory.AgentFieldPause(it) {
			h.refuse(w, http.StatusUnprocessableEntity, agentFieldIsApproval, "bad_input", "", nil, h.withVersion(api.FromItem(it)).State)
			return
		}
		next(w, r, it)
	}
}

// answerAgentField takes the person's decision on the run's pause.
func (h *Handler) answerAgentField(w http.ResponseWriter, r *http.Request, it factory.Item, decision string) {
	var body struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "could not read JSON body")
		return
	}
	if len(bytes.TrimSpace(data)) > 0 && json.Unmarshal(data, &body) != nil {
		h.fail(w, http.StatusUnprocessableEntity, "body must be JSON")
		return
	}
	words := strings.TrimSpace(body.Words)
	state := h.withVersion(api.FromItem(it)).State
	switch {
	case decision == agentfield.DecisionRequestChanges && words == "":
		h.refuse(w, http.StatusUnprocessableEntity, agentFieldNeedsWords, "bad_input", "", nil, state)
		return
	case len(words) > agentFieldWordsMost:
		h.refuse(w, http.StatusUnprocessableEntity, agentFieldTooLong, "bad_input", "", nil, state)
		return
	}
	if !h.fresh(w, r, it, "answer", body.Expect) || !h.answersRun(w, r, it) || !h.workerReady(w) {
		return
	}
	caller, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	wait := factory.AgentFieldWaiting(it)
	d := factory.AgentFieldDecision{Decision: decision, Words: words, By: factory.NormalizeHandle(caller)}
	if err := lease.NewDoors(h.workers.desk).AnswerAgentField(it.ID, wait.RequestID, d); err != nil {
		if errors.Is(err, lease.ErrAgentFieldMoved) {
			h.refuse(w, http.StatusConflict, agentFieldMoved, "stale", "", nil, state)
			return
		}
		h.failed(w, err)
		return
	}
	// A Reject ends the job as the person's Stop, once the runner has carried
	// it to the control plane (lease.Doors.AnswerAgentField).
	h.finish(w, nil)
}

// answersRun says the caller may answer the job's run pause, and refuses in
// words when not: the job's person answers it ([factory.MayAnswerAgentField]).
func (h *Handler) answersRun(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.mode() == api.FactorySolo {
		return true
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	if factory.MayAnswerAgentField(it, handle) {
		return true
	}
	who := it.Person()
	h.writeRefusal(w, http.StatusForbidden, refusalBody{
		Error: "only @" + who + " can answer this", Reason: factory.ReasonNotJobPerson,
		By: "@" + who, JobPerson: "@" + who, NowState: h.withVersion(api.FromItem(it)).State,
	})
	return false
}
