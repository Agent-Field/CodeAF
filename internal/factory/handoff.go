package factory

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
)

// EVERY STOP THE MAIN AGENT MAY DECIDE GOES TO THE MAIN AGENT FIRST.
//
// A job that stops used to wait on its person at once, whatever stopped it:
// a step out of rounds, a failed step, a Follow PR that reached its limit.
// Most of those are decisions the job's main agent (its manager) is allowed
// to make (the owner's authority table of 2026-11-17: another round, send
// back, rerun a step, accept a finding with a reason), so the person was
// asked for things nobody needed them for.
//
// Handoff is the record of one such stop on the job. While it stands the job
// reads "main_agent" where it would read "you: <action>" ([WaitOf]), with a
// deadline ([WaitMainAgentFor]); the service gives the main agent ONE turn
// on it (internal/factory/serve's handoff.go), and the main agent carries its
// decision out through the service's manager_decide door, which presses the
// same doors the person's card presses. A turn that leaves the job where it
// was, fails or runs past the deadline ends the handoff ([Handoff.Over]),
// and the job reads "you: <action>" as it always did.
//
// THE RULES ARE HERE, NOT IN A PROMPT. [ManagerFirst] says which stops are
// handed over, [ManagerDecides] which decision fits which stop, and the
// approvals of anything public, the merge, the budget and a take-over are
// never either.

// Handoff is the main agent's hold on one stop of a job.
type Handoff struct {
	// Wait is the stop it is for, by the key of the wait underneath
	// ("you:answer"). A job that reads another wait underneath has moved, and
	// the hold goes with it; "" is a record that only keeps Rounds.
	Wait string `json:"wait,omitempty"`
	// At is when the stop was handed over.
	At time.Time `json:"at,omitzero"`
	// Turns is how many turns the main agent has been given on it: none yet,
	// or its one, and TurnAt is when that turn began.
	Turns  int       `json:"turns,omitempty"`
	TurnAt time.Time `json:"turn_at,omitzero"`
	// Over says the stop was handed back to the person, and Why is the main
	// agent's reason, or why it had no turn.
	Over bool   `json:"over,omitempty"`
	Why  string `json:"why,omitempty"`
	// Rounds counts, by step, how often the main agent has put the job back
	// to work since its person last moved it. A step at [ManagerRoundsMost]
	// goes straight to the person the next time it stops.
	Rounds map[string]int `json:"rounds,omitempty"`
}

// HandoffTurnFor is the least a main agent's turn on a stop is given,
// however near the handoff's deadline it starts. A stop that came while the
// service was down is past its deadline when the service next looks, and its
// main agent has still had no turn.
const HandoffTurnFor = time.Minute

// ManagerRoundsMost is how many times the main agent may put one step back
// to work (another round, a send back, a rerun, a resumed pipeline, a retried
// publish) before that step's next stop is its person's.
const ManagerRoundsMost = 2

// The kinds of stop a main agent may decide ([ManagerStop]).
const (
	// StopRounds is a step out of rounds, or one whose verification is
	// incomplete.
	StopRounds = "rounds"
	// StopQuestion is a step's question the person holds that nobody sent on
	// to them: the main agent's turn on it failed or never came.
	StopQuestion = "question"
	// StopFailed is a step that failed.
	StopFailed = "failed"
	// StopUnsaved is results the service refused, still with their runner.
	StopUnsaved = "unsaved"
	// StopPublish is a publish that failed after it was approved.
	StopPublish = "publish"
	// StopFollow is Follow PR stopped on an open pull request: its limit, a
	// scope question, a conflict, changes requested.
	StopFollow = "follow"
)

// The main agent's decisions on a stop, as the manager_decide door takes them.
const (
	DecideAnotherRound   = "another_round"
	DecideSendBack       = "send_back"
	DecideRerun          = "rerun"
	DecideAccept         = "accept"
	DecideResumePipeline = "resume_pipeline"
	DecideRetryPublish   = "retry_publish"
	DecideHandToPerson   = "hand_to_person"
	DecideWaitGitHub     = "wait_github"
)

// ManagerDecisions is every decision, in the order a status lists them.
var ManagerDecisions = []string{DecideAnotherRound, DecideSendBack, DecideRerun, DecideAccept, DecideResumePipeline, DecideRetryPublish, DecideWaitGitHub, DecideHandToPerson}

// The words a person reads about a handoff.
const (
	// MainAgentDeciding is the job's one line while its main agent holds a
	// stop.
	MainAgentDeciding = "The main agent is deciding what to do."
	// HandoffUnsettled leads the log line of a stop handed back to the
	// person; the main agent's reason follows it.
	HandoffUnsettled = "The main agent could not settle this."
	// HandoffNotAsked, HandoffFailed and HandoffLate are the reasons the
	// service gives when the main agent gave none.
	HandoffNotAsked = "It was not asked."
	HandoffFailed   = "It could not be asked."
	HandoffLate     = "It did not answer in time."
)

// underHandoff is the wait the job's fields give with the watchdog's record
// and the handoff set aside: the stop a handoff is for.
func underHandoff(it Item) Wait {
	it.Stall, it.Handoff = nil, nil
	return WaitOf(it)
}

// handed says the main agent holds the stop w, which the job's fields read:
// a handoff for it stands, and the stop is still one the main agent may
// decide.
func handed(it Item, w Wait) bool {
	h := it.Handoff
	return h != nil && !h.Over && h.Wait != "" && h.Wait == w.key() && ManagerFirst(it, w)
}

// HandedOver says the job's main agent holds the stop the job is on, and
// answers the wait underneath: what the person would be asked for.
func HandedOver(it Item) (Wait, bool) {
	if st := it.Stall; st != nil && st.GaveUp {
		return Wait{}, false
	}
	w := underHandoff(it)
	if !handed(it, w) {
		return Wait{}, false
	}
	return w, true
}

// ManagerFirst says the stop w, which the job would wait on its person for,
// goes to its main agent first. It is the same in Manual and in Auto.
//
// NEVER HANDED OVER: an approval of anything public, the merge, the budget, a
// decision put away, a take-over, "bring a computer online", a stall the
// watchdog already handed to the person, a question only a person can answer
// or one the main agent already sent on, a job whose budget is spent (a turn
// costs money) and a step the main agent has already put back to work
// [ManagerRoundsMost] times.
func ManagerFirst(it Item, w Wait) bool {
	kind := stopKind(it, w)
	return kind != "" && budgetLeft(it) && !managerRoundsUsed(it, kind)
}

// ManagerStop names the kind of stop the job is on that its main agent may
// decide, "" for none. It reads the stop underneath a handoff, so it answers
// the same once the stop is back with the person: a main agent the person
// then talks to may still decide it.
func ManagerStop(it Item) string {
	return stopKind(it, underHandoff(it))
}

// stopKind is [ManagerStop] for the wait w.
func stopKind(it Item, w Wait) string {
	if w.Who != WaitYou || w.PutAway || it.Manual || StoppedByPerson(it) {
		return ""
	}
	if st := it.Stall; st != nil && st.GaveUp {
		return ""
	}
	open := it.PullRequest != nil && it.PullRequest.State == PROpen
	switch w.Action {
	case ActAnswer:
		switch {
		case it.QKind == QKindOutOfAttempts:
			return StopRounds
		case it.QKind == QKindStep:
			// A QUESTION SENT ON TO THE PERSON HAS HAD ITS TURN. The step said
			// only a person can answer it, or the main agent read it and said
			// why it is theirs ([Asked.Why]); neither comes back.
			if a := it.Asking; a != nil && a.With == AskedYou && !a.Person && strings.TrimSpace(a.Why) == "" {
				return StopQuestion
			}
		case it.QKind == qkindScope && open:
			return StopFollow
		}
	case ActRetry:
		switch {
		case publishOutrun(it):
			return StopFollow
		case it.Failure != nil:
			return StopFailed
		case resultsNotSaved(it):
			return StopUnsaved
		case open:
			return StopFollow
		}
	case ActRetryPublish:
		return StopPublish
	case ActDecide:
		if open {
			return StopFollow
		}
	}
	return ""
}

// qkindScope is the question kind of Follow PR's own stop (run's parkCI).
const qkindScope = "scope"

// stopStep is the step a stop of this kind is counted against.
func stopStep(it Item, kind string) string {
	switch kind {
	case StopPublish:
		return "publish"
	case StopFollow:
		return FollowPRStep
	case StopFailed:
		if f := it.Failure; f != nil && f.Step != "" {
			return f.Step
		}
	}
	return waitStep(it)
}

// managerRoundsUsed says the main agent has put this stop's step back to work
// as often as it may.
func managerRoundsUsed(it Item, kind string) bool {
	h := it.Handoff
	return h != nil && h.Rounds[stopStep(it, kind)] >= ManagerRoundsMost
}

// movesWork says a decision puts the job back to work, and so counts against
// [ManagerRoundsMost].
func movesWork(decision string) bool {
	switch decision {
	case DecideAnotherRound, DecideSendBack, DecideRerun, DecideResumePipeline, DecideRetryPublish, DecideWaitGitHub:
		return true
	}
	return false
}

// decisionsFor lists the decisions that fit a stop of this kind on the job
// as it stands, whatever the count.
func decisionsFor(it Item, kind string) []string {
	open := it.PullRequest != nil && it.PullRequest.State == PROpen
	var out []string
	switch kind {
	case StopRounds:
		out = append(out, DecideAnotherRound)
		if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) && PreviousWork(s.Phases, s.Cur) >= 0 {
			out = append(out, DecideSendBack)
		}
		out = append(out, DecideRerun)
		if OffersAccept(it) {
			out = append(out, DecideAccept)
		}
	case StopFailed, StopUnsaved:
		out = append(out, DecideRerun)
		if open || kind == StopUnsaved {
			out = append(out, DecideResumePipeline)
		}
	case StopPublish:
		out = append(out, DecideRetryPublish)
	case StopFollow:
		out = append(out, DecideResumePipeline)
		if followStopped(it) {
			out = append(out, DecideWaitGitHub)
		}
	}
	// A stop already back with the person has nothing left to hand over.
	if h := it.Handoff; kind != "" && h != nil && !h.Over && h.Wait != "" {
		out = append(out, DecideHandToPerson)
	}
	return out
}

// The refusals of [ManagerDecides], each a sentence the main agent reads.
var (
	// ErrNoManagerStop is a job that is not on a stop the main agent may
	// decide: it moved on, its person settled it, or the stop was never the
	// main agent's.
	ErrNoManagerStop = errors.New("Nothing to decide: this job is not on a stop the main agent may decide. It may already be settled, or it is the person's (an approval, the merge, the budget, a take-over)")
	// ErrStopWithPerson is a stop the main agent asks to hand to the person
	// that is theirs already.
	ErrStopWithPerson = errors.New("This stop is already with the person")
	// ErrManagerRoundsUsed is a step the main agent has put back to work as
	// often as it may.
	ErrManagerRoundsUsed = fmt.Errorf("The main agent has already put this step back to work %d times since the person last moved the job. The next move is the person's", ManagerRoundsMost)
)

// ManagerDecides says the job's main agent may carry out decision on the stop
// the job is on, or why not. It holds once the stop is back with the person
// too, so a main agent the person then asks may still decide it; what it may
// never decide here (the approvals of anything public, the merge, the budget,
// a take-over) is no stop of its kind at all.
func ManagerDecides(it Item, decision string) error {
	kind := ManagerStop(it)
	if kind == "" {
		return ErrNoManagerStop
	}
	fits := decisionsFor(it, kind)
	found := false
	for _, d := range fits {
		found = found || d == decision
	}
	if !found {
		if len(fits) == 0 {
			return ErrNoManagerStop
		}
		// A STOP ALREADY WITH THE PERSON HAS NOTHING LEFT TO HAND OVER. Handing
		// a stop on is the one decision the main agent is told it may always
		// make, so it is answered with what is true and not with a list of
		// other decisions to try.
		if decision == DecideHandToPerson {
			return ErrStopWithPerson
		}
		return fmt.Errorf("%s does not fit this stop. It takes: %s", decision, strings.Join(fits, ", "))
	}
	if movesWork(decision) && managerRoundsUsed(it, kind) {
		return ErrManagerRoundsUsed
	}
	return nil
}

// managerOffers lists what the job offers its main agent on the stop it is
// on: every decision [ManagerDecides] takes now.
func managerOffers(it Item) []Offer {
	// Only a job that needs someone is on a stop; the rest are spared the read.
	if JobState(it) != "needs_you" {
		return nil
	}
	kind := ManagerStop(it)
	if kind == "" {
		return nil
	}
	var out []Offer
	for _, d := range decisionsFor(it, kind) {
		if movesWork(d) && managerRoundsUsed(it, kind) {
			continue
		}
		out = append(out, Offer{Action: d, By: OfferByMainAgent})
	}
	// A STEP'S QUESTION THE MAIN AGENT HOLDS BY A HANDOFF IS ANSWERED THROUGH
	// THE DOOR IT ALWAYS HAD (lease.Doors.AnswerPending).
	if _, held := HandedOver(it); held && kind == StopQuestion {
		out = append(out, Offer{Action: DoAnswer, By: OfferByMainAgent})
	}
	return out
}

// settleHandoff keeps the job's handoff across one write, before its wait is
// read ([SettleWait]). A handoff belongs to the stop it was made for: once the
// job reads another wait underneath, it is over. A job that newly reads a stop
// the main agent may decide is handed over in the same write, so it never
// reads "you" first.
//
// ONLY A NEW STOP IS HANDED OVER. A job that already waited on its person for
// the same thing before this write (one saved before handoffs, or one whose
// handoff ended) stays with them: nothing here takes a stop back from a
// person.
func settleHandoff(before Item, it *Item, now time.Time) {
	// Nearly every write is of a job on no stop with no record to keep: only
	// a job that needs someone can be on one.
	if it.Handoff == nil && JobState(*it) != "needs_you" {
		return
	}
	under := underHandoff(*it)
	key := under.key()
	h := it.Handoff
	if h != nil {
		cp := *h
		cp.Rounds = maps.Clone(h.Rounds)
		h = &cp
		// THE PERSON'S MOVE WIPES THE COUNT. A job that waited on its person
		// and now reads another wait was moved by them, or by the world; the
		// main agent starts again with the step. THE MAIN AGENT'S OWN MOVE
		// NEVER DOES: a write that counts one of its rounds (a task it sent to
		// a stop already with the person, [NoteManagerTask]) moved the job
		// itself, and a count it wiped would let it go on for ever.
		if prior := before.Wait; prior != nil && prior.Who == WaitYou && prior.key() != key && roundsCounted(it.Handoff) <= roundsCounted(before.Handoff) {
			h.Rounds = nil
		}
		if h.Wait != "" && h.Wait != key {
			h = &Handoff{Rounds: h.Rounds}
		}
		if h.Wait == "" && len(h.Rounds) == 0 {
			h = nil
		}
	}
	it.Handoff = h
	if before.Wait == nil || (h != nil && h.Wait != "") || standingStop(before) == key || !ManagerFirst(*it, under) {
		return
	}
	next := Handoff{Wait: key, At: now}
	if h != nil {
		next.Rounds = h.Rounds
	}
	it.Handoff = &next
}

// roundsCounted is how many rounds the handoff counts, over every step.
func roundsCounted(h *Handoff) int {
	n := 0
	if h != nil {
		for _, r := range h.Rounds {
			n += r
		}
	}
	return n
}

// standingStop is the stop the stored job waits on its person or its main
// agent for, by the key of the wait underneath, "" for none.
func standingStop(it Item) string {
	if h := it.Handoff; h != nil && h.Wait != "" {
		return h.Wait
	}
	if w := it.Wait; w != nil && w.Who == WaitYou {
		return w.key()
	}
	return ""
}

// Turned is the handoff with the main agent's one turn counted, begun at now.
func (h Handoff) Turned(now time.Time) Handoff {
	h.Turns++
	h.TurnAt = now
	return h
}

// HandoffDue is when the stop the job's main agent holds is its person's
// whatever the turn is doing: the wait's deadline, or the end of a turn that
// began too late to fit before it ([HandoffTurnFor]). It is zero for a job
// whose main agent holds no stop.
func HandoffDue(it Item) time.Time {
	w, h := it.Wait, it.Handoff
	if w == nil || h == nil || w.Who != WaitMainAgent {
		return time.Time{}
	}
	due := w.Deadline
	if end := h.TurnAt.Add(HandoffTurnFor); h.Turns > 0 && end.After(due) {
		due = end
	}
	return due
}

// Ended is the handoff handed back to the person, with why. One already
// handed back keeps its reason.
func (h Handoff) Ended(why string) Handoff {
	if h.Over {
		return h
	}
	h.Over, h.Why = true, strings.TrimSpace(why)
	return h
}

// HandoffLine is the log line of a stop handed back to the person: the fixed
// lead and the reason.
func HandoffLine(why string) string {
	return strings.TrimSpace(HandoffUnsettled + " " + strings.TrimSpace(why))
}

// EndHandoff hands the stop the job's main agent holds back to its person,
// with why, and says so on the job's log. It answers false, having changed
// nothing, when the main agent holds no stop on the job.
func EndHandoff(it *Item, why string, now time.Time) bool {
	if _, held := HandedOver(*it); !held {
		return false
	}
	next := it.Handoff.Ended(why)
	it.Handoff = &next
	if it.Stream != nil {
		it.Stream.Log = append(it.Stream.Log, LogLine{At: now, Tone: "ask", Text: HandoffLine(next.Why)})
	}
	return true
}

// NoteManagerDecision keeps one decision the main agent carried out on the
// job: a line on its log, and one against the step's count when the decision
// put the job back to work. kind and step are the stop's as it stood before
// the decision ([ManagerStop], [ManagerStopStep]); the job has moved since.
func NoteManagerDecision(it *Item, kind, step, decision, why string, now time.Time) {
	if movesWork(decision) && kind != "" {
		h := Handoff{}
		if it.Handoff != nil {
			h = *it.Handoff
			h.Rounds = maps.Clone(it.Handoff.Rounds)
		}
		if h.Rounds == nil {
			h.Rounds = map[string]int{}
		}
		h.Rounds[step]++
		it.Handoff = &h
	}
	if it.Stream != nil {
		it.Stream.Log = append(it.Stream.Log, LogLine{At: now, Tone: "said", Text: ManagerDecisionLine(decision, why)})
	}
}

// KeepManagerRounds puts back the count held, the job's handoff as it stood
// before the main agent's decision was carried out. The door that carried it
// out moved the job off its stop in a write of its own, which reads as the
// person's move when the stop was already with them ([settleHandoff]); THE
// MAIN AGENT'S OWN DECISION NEVER WIPES ITS COUNT, so the count it had is
// kept, step by step, wherever that write left a smaller one. It is called
// in the write that counts the decision ([NoteManagerDecision]).
func KeepManagerRounds(it *Item, held *Handoff) {
	if held == nil || len(held.Rounds) == 0 {
		return
	}
	h := Handoff{}
	if it.Handoff != nil {
		h = *it.Handoff
	}
	rounds := maps.Clone(h.Rounds)
	if rounds == nil {
		rounds = map[string]int{}
	}
	for step, n := range held.Rounds {
		rounds[step] = max(rounds[step], n)
	}
	h.Rounds = rounds
	it.Handoff = &h
}

// ManagerStopStep is the step the stop the job is on is counted against.
func ManagerStopStep(it Item) string {
	return stopStep(it, ManagerStop(it))
}

// decisionWords says each decision as the job's log does.
var decisionWords = map[string]string{
	DecideAnotherRound:   "another round",
	DecideSendBack:       "sent back",
	DecideRerun:          "ran the step again",
	DecideAccept:         "accepted the step as it stands",
	DecideResumePipeline: "resumed the pipeline",
	DecideRetryPublish:   "retried publishing",
	DecideWaitGitHub:     "went back to waiting on GitHub",
	DecideHandToPerson:   "handed it to you",
}

// ManagerDecisionLine is the log line of one decision: "Main agent: another
// round · <why>".
func ManagerDecisionLine(decision, why string) string {
	line := "Main agent: " + firstNonEmpty(decisionWords[decision], decision)
	if why = strings.Join(strings.Fields(why), " "); why != "" {
		line += " · " + why
	}
	return line
}

// followStopped says Follow PR itself is stopped on the job's open pull
// request, and not on the budget or on a repair that waits to be published:
// the one stop [WaitOnGitHub] may lift.
func followStopped(it Item) bool {
	f, pr := it.FollowUp, it.PullRequest
	return f != nil && pr != nil && pr.State == PROpen && f.State == "needs_you" && f.BlockedBy != FollowBlockedBudget &&
		!it.Publishing && !publishFailed(it) && it.Failure == nil
}

// WaitOnGitHubWords is what Follow PR says once the main agent has put it
// back to watching.
const WaitOnGitHubWords = "Watching CI, reviews and merge conflicts. The main agent decided nothing needs doing now."

// WaitOnGitHub puts a stopped Follow PR back to watching its pull request
// with nothing queued: the stop is taken down, and what GitHub shows now is
// marked as seen, so only something new there starts the next round
// ([QueueCIFollowUp]). It is for a stop that needs nothing done, and it is
// refused for every stop that is not Follow PR's own.
func WaitOnGitHub(it *Item, now time.Time) error {
	if !followStopped(*it) {
		return errors.New("Follow PR is not stopped on an open pull request here, so there is nothing to go back to watching")
	}
	f, pr := *it.FollowUp, it.PullRequest
	f.Reviewed = maps.Clone(f.Reviewed)
	if pr.FeedbackTracked {
		if f.Reviewed == nil {
			f.Reviewed = map[string]string{}
		}
		for id, version := range ReviewVersions(pr) {
			f.Reviewed[id] = version
		}
	}
	f.Inspecting = nil
	f.State, f.BlockedBy, f.Request = "watching", "", ""
	f.Reason = WaitOnGitHubWords
	f.EventKey, f.ReviewKey = followEvent(pr, ""), followReviewKey(pr)
	it.FollowUp = &f
	it.State = StateShipped
	it.Question, it.QKind = "", ""
	it.Asking = nil
	if it.Stream != nil {
		i := EnsureFollowPR(it)
		it.Stream.Phases[i].State, it.Stream.Phases[i].Note = PhaseWaiting, f.Reason
	}
	return nil
}

// followReviewKey is the key [QueueCIFollowUp] compares a pull request's
// reviews by: its event with everything but the reviews set aside.
func followReviewKey(pr *PullRequest) string {
	review := *pr
	review.ReadinessKey, review.HeadSHA, review.BaseSHA = "", "", ""
	review.CheckRuns, review.Checks, review.Conflict = nil, PRChecks{}, false
	return followEvent(&review, "")
}
