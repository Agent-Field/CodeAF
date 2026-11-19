package run

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── A CHECK THAT COULD NOT FINISH SAYS NOTHING ABOUT THE WORK ───────────────
//
// A review check reads a leaf that has already landed done and answers "holds"
// or "does not hold". Two kinds of ending give neither answer and are facts
// about the CHECKER, never about the work:
//
//   - its seat met its own spend ceiling mid-read (run19, #2841);
//   - its model could not be served: the provider refused the request, the
//     router had nowhere to send it, the stream stalled, the connection was
//     down (run21: the rota's checker carried encrypted working its own
//     provider filter excluded, and every rung was refused).
//
// Both settle the run as done-but-unchecked, which the session asks the person
// about (your call) and never draws as failed. What is NOT this: a task or day
// limit the person set, and the run's own wall or a person's stop — those end
// the run, and the run says so in its own words.

// checkCouldNotFinish is whether a review check came home without an answer
// for a reason that is the checker's and not the work's. The run's own ending
// is read off the worker's context ([workerReturn.ended]) before anything in
// the error is, because a provider's timeout and the run's wall can wrap the
// very same context error.
func checkCouldNotFinish(ret workerReturn) bool {
	if ret.err == nil || ret.ended {
		return false
	}
	return checkerOutOfBudget(ret.err) || checkUnserved(ret.err)
}

// checkerOutOfBudget is whether err is a checker's own spend ceiling — the
// one stop that says nothing about the work and nothing about the person's
// money ([session.ErrSpendStopped.SeatCeiling]). A task or day limit is not
// this: those end the run on a bound its person set.
func checkerOutOfBudget(err error) bool {
	var stopped session.ErrSpendStopped
	return errors.As(err, &stopped) && stopped.SeatCeiling && !stopped.TaskLimit
}

// checkUnserved is whether the check's turn ended because no model answered
// it. EVERY CLAUSE IS A TYPE, never a sentence: the provider's refusal
// ([provider.APIError]), a spent ladder ([provider.RefusalError]), a stream the
// guard cut ([provider.StreamCut]), a connection that stayed down, and a
// network timeout. A spend stop of any kind is not this — the ceiling is
// [checkerOutOfBudget]'s and a person's limit is nobody's to soften.
func checkUnserved(err error) bool {
	var stopped session.ErrSpendStopped
	if errors.As(err, &stopped) {
		return false
	}
	var api *provider.APIError
	var spent *provider.RefusalError
	if errors.As(err, &api) || errors.As(err, &spent) || provider.IsConnectionUnavailable(err) {
		return true
	}
	if _, cut := provider.CutFrom(err); cut {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && network.Timeout()
}

// checkStopSentence is the plain account a check that could not finish ends
// on: the run carries it out as [Summary.Unchecked], the session leads the
// task's report with it, and the conversation's model reads it as the reason
// — so it says what really stopped the check and never what it might mean
// about the work.
//
// The ceiling keeps the guard's own sentence, which the session already
// recognises by its opening words. Every other stop opens "the check could not
// finish:" and names the model and the provider's own words when there are
// any, because "nobody could check it" with no cause is what let run21's
// manager invent one.
func checkStopSentence(err error) string {
	if checkerOutOfBudget(err) {
		return err.Error()
	}
	return checkStopLead + checkStopCause(err)
}

// checkStopLead opens every stop that is not the ceiling's.
const checkStopLead = "the check could not finish: "

func checkStopCause(err error) string {
	model := ""
	var spent *provider.RefusalError
	if errors.As(err, &spent) {
		model = strings.TrimSpace(spent.Model)
	}
	its := "its model"
	if model != "" {
		its = "its model " + model
	}
	var api *provider.APIError
	if errors.As(err, &api) {
		said := strings.TrimSpace(api.Message)
		if said == "" {
			said = strings.TrimSpace(api.Raw)
		}
		if said != "" {
			return fmt.Sprintf("the provider would not serve %s and said %q", its, clipStop(said))
		}
		return fmt.Sprintf("the provider would not serve %s", its)
	}
	if _, cut := provider.CutFrom(err); cut {
		return its + " stopped answering partway through a reply"
	}
	if provider.IsConnectionUnavailable(err) {
		return "the connection to " + its + " was down"
	}
	return its + " did not answer: " + clipStop(err.Error())
}

// clipStop keeps a provider's words to a length a row and a reply can carry.
func clipStop(said string) string {
	const most = 280
	said = strings.Join(strings.Fields(said), " ")
	if len([]rune(said)) <= most {
		return said
	}
	return string([]rune(said)[:most-1]) + "…"
}
