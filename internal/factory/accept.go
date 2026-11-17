package factory

import "strings"

// ChoiceAccept is the person's fourth answer to a step that stopped short:
// the step counts as finished as it stands, and the run goes on.
const ChoiceAccept = "accept"

// AcceptedPrefix marks, in a step's saved notes, a shortfall the person
// accepted. THE NOTE IS THE WHOLE RECORD: a step's notes cross between the
// factory and a worker both ways and reach every later step's brief, so an
// acceptance kept anywhere else would be lost on one of those trips.
const AcceptedPrefix = "Accepted by the owner:"

// unresolvedCheckPrefix is verification.FailurePrefix, which this package
// cannot import. run's tests hold the two together.
const unresolvedCheckPrefix = "Unresolved verification command failed:"

// OffersAccept says whether the person may accept the step the job waits on.
// Only a step that judges the work and reported what it found can be
// accepted: Test, Review and Proof, before anything is published. A step
// that makes the work (plan, write), one that never reported, a check the
// machine could not finish, and everything after publish cannot.
func OffersAccept(it Item) bool {
	if StoppedByPerson(it) || it.State != StateNeedsYou || it.QKind != QKindOutOfAttempts || it.Stream == nil ||
		it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) || it.Stream.Phases[it.Stream.Cur].State != PhaseWaiting {
		return false
	}
	return AcceptableStep(it, it.Stream.Cur)
}

// AcceptableStep is [OffersAccept] for step i without the question: what the
// runner reads before it asks, to know whether accept is among the answers.
func AcceptableStep(it Item, i int) bool {
	if it.Stream == nil || i < 0 || i >= len(it.Stream.Phases) {
		return false
	}
	if it.PullRequest != nil || it.FollowUp != nil || it.Publishing || it.PublishStatus != nil {
		return false
	}
	ph := it.Stream.Phases[i]
	if ph.Result == nil || (ph.Verification != nil && ph.Verification.State == "incomplete") {
		return false
	}
	if ph.Kind != "" && ph.Kind != StageChat {
		return false
	}
	until := ""
	if j := StageIndex(it.Stages, ph.Name); j >= 0 {
		st := it.Stages[j]
		if (st.Kind != "" && st.Kind != StageChat) || IsCIRepairStage(st) {
			return false
		}
		until = strings.TrimSpace(st.Until)
	}
	switch until {
	case UntilGreen, UntilNoNewFailures, UntilClean, UntilProven:
	default:
		return false
	}
	r := ph.Result
	return r.Done || r.Findings > 0 || len(r.Claims) > 0 || len(unresolvedChecks(r.Notes)) > 0
}

// OutOfAttemptChoices are the answers this job's stuck step takes, with
// accept only where [OffersAccept] holds.
func OutOfAttemptChoices(it Item) []string {
	out := append([]string(nil), OutOfAttemptOptions...)
	if OffersAccept(it) {
		out = append(out, ChoiceAccept)
	}
	return out
}

// AcceptStep records the person's acceptance on the step's saved result and
// answers what was accepted. Each unresolved check becomes an accepted one,
// word for word, so a later step's guard can tell the same failure from a new
// one; a shortfall with no failing check (open findings, a claim not shown)
// is kept as why. The result's own done, exit and findings stay as reported.
func AcceptStep(ph *Phase, why string) []string {
	if ph.Result == nil {
		ph.Result = &StageResult{}
	}
	var accepted []string
	notes := make([]string, 0, len(ph.Result.Notes)+1)
	for _, n := range ph.Result.Notes {
		if rest, ok := strings.CutPrefix(n, unresolvedCheckPrefix); ok {
			rest = strings.TrimSpace(rest)
			accepted = append(accepted, rest)
			n = AcceptedPrefix + " " + rest
		}
		notes = append(notes, n)
	}
	if len(accepted) == 0 {
		if why = strings.TrimSpace(why); why == "" {
			why = ph.Name + " as it stands"
		}
		accepted = append(accepted, why)
		notes = append(notes, AcceptedPrefix+" "+why)
	}
	ph.Result.Notes = notes
	return accepted
}

// StuckReason is a stuck step's question without the choices it ends on:
// `test is not green after 2 rounds: … · try again, or stop the job?` is
// `test is not green after 2 rounds: …`.
func StuckReason(question string) string {
	q := strings.TrimSpace(question)
	if strings.HasSuffix(q, "?") {
		if cut := strings.LastIndex(q, " · "); cut > 0 {
			return strings.TrimSpace(q[:cut])
		}
	}
	return q
}

// AcceptedStep is one finished step the person accepted short of its rule.
type AcceptedStep struct {
	Step string
	What []string
}

// AcceptedSteps lists every accepted shortfall on the job's finished steps,
// in step order. A step sent back to run again has a new result and no
// acceptance.
func AcceptedSteps(it Item) []AcceptedStep {
	if it.Stream == nil {
		return nil
	}
	var out []AcceptedStep
	for _, ph := range it.Stream.Phases {
		if ph.State != PhaseDone || ph.Result == nil {
			continue
		}
		var what []string
		for _, n := range ph.Result.Notes {
			if rest, ok := strings.CutPrefix(n, AcceptedPrefix); ok {
				what = append(what, strings.TrimSpace(rest))
			}
		}
		if len(what) > 0 {
			out = append(out, AcceptedStep{Step: ph.Name, What: what})
		}
	}
	return out
}

// AcceptedFailures is every accepted shortfall as one list: what a later
// step's verification guard does not count against it.
func AcceptedFailures(it Item) []string {
	var out []string
	for _, s := range AcceptedSteps(it) {
		out = append(out, s.What...)
	}
	return out
}

func unresolvedChecks(notes []string) []string {
	var out []string
	for _, n := range notes {
		if rest, ok := strings.CutPrefix(n, unresolvedCheckPrefix); ok {
			out = append(out, strings.TrimSpace(rest))
		}
	}
	return out
}
