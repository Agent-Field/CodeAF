package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// ManagerDecideVerb is the job door a job's main agent carries its decision
// on a stop out through (factory's handoff.go). The agent's tool presses it,
// in process, as the job's person; nothing else does.
const ManagerDecideVerb = "manager_decide"

// managerDecide carries out one decision of the job's main agent on the stop
// the job is on.
//
// IT PRESSES THE DOORS THE PERSON'S CARD PRESSES, so a decision moves the job
// by exactly the rules a person's answer does and nothing is decided twice:
// another round, a send back and an accept are the answer door's
// (lease.Doors.Answer), a rerun is the step's rerun, Resume pipeline and Retry
// publishing are their own doors. What it adds is who may press, and when:
// [factory.ManagerDecides] refuses every job that is not on a stop the main
// agent may decide (an approval of anything public, the merge, the budget and
// a take-over are never one) and every step it has already put back to work
// [factory.ManagerRoundsMost] times. A refusal is a sentence the agent reads,
// and changes nothing.
//
// THE PERSON'S CARD STAYS OPEN while the main agent holds the stop. Whoever
// presses second finds the job moved on and is told so: here by
// [factory.ErrNoManagerStop], at the person's door by that door's own words.
func (h *Handler) managerDecide(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Decision string `json:"decision"`
		Step     string `json:"step,omitempty"`
		Words    string `json:"words,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.owns(w, r, it) {
		return
	}
	decision := strings.ToLower(strings.TrimSpace(body.Decision))
	step, words := strings.TrimSpace(body.Step), strings.TrimSpace(body.Words)
	if !slices.Contains(factory.ManagerDecisions, decision) {
		h.fail(w, http.StatusUnprocessableEntity, "decision must be one of: "+strings.Join(factory.ManagerDecisions, ", "))
		return
	}
	if words == "" && (decision == factory.DecideAccept || decision == factory.DecideSendBack || decision == factory.DecideHandToPerson) {
		h.fail(w, http.StatusUnprocessableEntity, decision+" needs its reason in words")
		return
	}
	// One decision at a time on a job, and never beside a person's answer to
	// the same question: the lock is the one their answer takes.
	unlock, err := h.lockConsent()
	if h.failed(w, err) {
		return
	}
	defer unlock()
	if it, err = h.config.Store.Get(it.ID); h.failed(w, err) {
		return
	}
	if factory.RecoveryPaused(it) {
		h.finish(w, run.StepControlRefusal(factory.DecisionPauseDetail(it)))
		return
	}
	if err := factory.ManagerDecides(it, decision); err != nil {
		h.undecided(w, it, err)
		return
	}
	kind, stopped := factory.ManagerStop(it), factory.ManagerStopStep(it)
	// A decision that names a step is for a stop at that step. One that
	// arrives after the job moved on to another step's stop is refused, not
	// carried out on a stop its agent never read.
	if step != "" && decision != factory.DecideRerun && kind != factory.StopFollow && kind != factory.StopPublish && step != stopped {
		h.undecided(w, it, fmt.Errorf("This job is stopped at %s, not at %s. Read its status again before deciding", stopped, step))
		return
	}
	if decision == factory.DecideRerun && step == "" {
		step = stopped
	}
	if (decision == factory.DecideRerun || decision == factory.DecideRetryPublish) && h.refuseOff(w, factory.RepoOf(it), factory.JobState(it)) {
		return
	}
	held := it.Handoff
	if !h.carryOut(w, r, it, decision, step, words) {
		return
	}
	now := h.clock()
	detail := strings.TrimPrefix(factory.ManagerDecisionLine(decision, words), "Main agent: ")
	if h.config.Record != nil {
		_, _ = h.config.Record.Append(r.Context(), record.Event{Kind: record.Answered, Job: it.ID,
			Actor: record.Actor{Kind: "agent", Name: "main agent"}, Cost: jobCost(it), Detail: detail})
	}
	if decision != factory.DecideHandToPerson {
		_ = h.config.Store.Annotate(it.ID, func(cur *factory.Item) error {
			// The door's write may have read as the person's move and wiped
			// the count; the main agent's own decision keeps it.
			factory.KeepManagerRounds(cur, held)
			factory.NoteManagerDecision(cur, kind, stopped, decision, words, now)
			// AN ANSWER GIVEN FOR THE PERSON IS NEVER INVISIBLE: the question
			// the decision answered is kept as one the main agent answered.
			if a := it.Asking; a != nil && it.QKind != factory.QKindStep {
				cur.KeepManagerAnswer(*a, detail, now)
			}
			// A DECISION THE DOOR TOOK THAT LEAVES THE JOB ON THE SAME STOP
			// (results sent again, still on their way) has had its turn: the
			// stop is the person's until the job moves.
			if held != nil && sameHandoff(cur, *held) {
				next := cur.Handoff.Ended(detail)
				cur.Handoff = &next
			}
			return nil
		})
	}
	h.scheduleJobTimer()
	h.finish(w, nil)
}

// undecided refuses a decision in a sentence, as a conflict with the job as
// it stands.
func (h *Handler) undecided(w http.ResponseWriter, it factory.Item, err error) {
	status, reason := classify(err)
	if status == http.StatusInternalServerError {
		status, reason = http.StatusConflict, "conflict"
	}
	h.refuse(w, status, err.Error(), reason, "", nil, factory.JobState(it))
}

// carryOut presses the door of one decision. It answers false, having
// written the door's refusal, when the door did not take it.
func (h *Handler) carryOut(w http.ResponseWriter, r *http.Request, it factory.Item, decision, step, words string) bool {
	door := func(err error) bool {
		if err != nil {
			h.undecided(w, it, err)
		}
		return err == nil
	}
	switch decision {
	case factory.DecideAnotherRound, factory.DecideSendBack, factory.DecideAccept:
		if !h.requireVerb(w, h.seam.Answer != nil) {
			return false
		}
		yes, answer := true, factory.ChoiceTryOnceMore
		switch {
		case decision == factory.DecideSendBack:
			yes, answer = false, factory.ChoiceSendBack+": "+words
		case decision == factory.DecideAccept:
			answer = factory.ChoiceAccept
		case words != "":
			answer += ": " + words
		}
		return door(h.seam.Answer(it.ID, yes, answer))
	case factory.DecideRerun:
		if !h.workerReady(w) {
			return false
		}
		return door(lease.NewDoors(h.workers.desk).StepRerunExpected(it.ID, step, words, "", nil))
	case factory.DecideResumePipeline:
		return h.pressed(w, r, map[string]any{"mode": "resume_pipeline", "words": words, "by": factory.OfferByMainAgent}, func(w http.ResponseWriter, r *http.Request) { h.controlJob(w, r, it) })
	case factory.DecideRetryPublish:
		if !h.requireVerb(w, h.seam.SignOff != nil) {
			return false
		}
		return h.pressed(w, r, nil, func(w http.ResponseWriter, r *http.Request) { h.retryPublish(w, r, it) })
	case factory.DecideWaitGitHub:
		// A round still parked on the stop is let go first, as Resume
		// pipeline lets it go (control.go).
		if h.workers != nil && h.workers.desk != nil {
			if !door(lease.NewDoors(h.workers.desk).Withdraw(it.ID)) {
				return false
			}
		}
		return door(h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if err := factory.ManagerDecides(*cur, decision); err != nil {
				return err
			}
			return factory.WaitOnGitHub(cur, h.clock())
		}))
	case factory.DecideHandToPerson:
		return door(h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if !factory.EndHandoff(cur, words, h.clock()) {
				return factory.ErrStopWithPerson
			}
			return nil
		}))
	}
	return door(factory.ErrNoManagerStop)
}

// pressed presses another door of this service with body, as this request's
// caller. A door that refuses has its own answer written to w, whole, and
// pressed answers false; one that takes it writes nothing here.
func (h *Handler) pressed(w http.ResponseWriter, r *http.Request, body any, door http.HandlerFunc) bool {
	data := []byte("{}")
	if body != nil {
		data, _ = json.Marshal(body)
	}
	inner := r.Clone(r.Context())
	inner.Body = io.NopCloser(bytes.NewReader(data))
	rec := &doorAnswer{header: http.Header{}}
	door(rec, inner)
	if rec.status == 0 || (rec.status >= 200 && rec.status < 300) {
		return true
	}
	for key, values := range rec.header {
		w.Header()[key] = values
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.body.Bytes())
	return false
}

// doorAnswer keeps what a door pressed from inside the service answered.
type doorAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (d *doorAnswer) Header() http.Header { return d.header }

func (d *doorAnswer) WriteHeader(status int) {
	if d.status == 0 {
		d.status = status
	}
}

func (d *doorAnswer) Write(b []byte) (int, error) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	return d.body.Write(b)
}
