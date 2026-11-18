package factory

import "strings"

// ── A JOB'S WAIT AS THE FACTORY MANAGER READS IT ───────────────────────────
//
// The Factory manager (the factory's own conversation, `factory_floor`) reads
// every job at once, so it reads each job's stored wait and offers
// ([SettleWait]) in a compact form: one part of a row, and one line of a
// refusal. [WaitLine] says the same wait to the job's own manager, in words
// addressed to it ("you, the manager"); these are addressed to somebody
// looking at the job from outside.
//
// NOTHING HERE READS THE JOB AGAIN. The wait and the offers are the ones the
// store wrote; a job no write has touched since waits were stored is read from
// its fields, by the same reading the store would have made.

// floorWait is the job's stored wait and offers.
func floorWait(it Item) (Wait, []Offer) {
	if it.Wait != nil {
		return *it.Wait, it.Offers
	}
	return WaitOf(it), OffersOf(it)
}

// WaitsOnPerson answers the job's wait when it is the person's, put away or
// not.
func WaitsOnPerson(it Item) (Wait, bool) {
	w, _ := floorWait(it)
	return w, w.Who == WaitYou
}

// WaitsOnMainAgent says a stop of the job is with its main agent now.
func WaitsOnMainAgent(it Item) bool {
	w, _ := floorWait(it)
	return w.Who == WaitMainAgent
}

// WaitsOnRunner says the job waits for a computer or a slot.
func WaitsOnRunner(it Item) bool {
	w, _ := floorWait(it)
	return w.Who == WaitRunner
}

// PersonOffers lists the actions the job accepts from its person now, in the
// order the store wrote them.
func PersonOffers(it Item) []string { return offersBy(it, OfferByYou) }

// MainAgentOffers lists the actions the job accepts from its main agent now.
func MainAgentOffers(it Item) []string { return offersBy(it, OfferByMainAgent) }

func offersBy(it Item, by string) []string {
	_, offers := floorWait(it)
	var out []string
	for _, o := range offers {
		if o.By == by {
			out = append(out, o.Action)
		}
	}
	return out
}

// OffersPerson says the job accepts action from its person now.
func OffersPerson(it Item, action string) bool {
	for _, offered := range PersonOffers(it) {
		if offered == action {
			return true
		}
	}
	return false
}

// floorWorking says what is under way on a job that waits on nobody: the
// service's own act by its word (publishing, merging, approving), and
// otherwise work. A STEP IS NOT NAMED HERE: the wait keeps it by its key
// (`followpr`), and the row already says the step in the person's words.
func floorWorking(w Wait) string {
	switch w.What {
	case "publishing", "merging", "approving":
		return w.What
	}
	return "work"
}

// floorGitHub says what a job waits on at GitHub.
func floorGitHub(w Wait) string {
	switch w.What {
	case "checks":
		return "its checks"
	case "review":
		return "a review"
	case "mergeable":
		return "whether it can merge"
	}
	return "the pull request"
}

// floorStallMost is how much of a stall's words a row carries.
const floorStallMost = 160

// FloorWaitWords says what the job waits on as one part of a row: `waits on
// the person to approve the plan`, `handed to its main agent, which is
// deciding what to do`, `waits on GitHub (checks)`. A stall the watchdog saw
// is said beside it. It is "" for a job that waits on nothing (not started,
// finished).
func FloorWaitWords(it Item) string {
	w, _ := floorWait(it)
	var line string
	switch w.Who {
	case WaitWorking:
		line = "waits on nobody (" + floorWorking(w) + " under way)"
	case WaitGitHub:
		line = "waits on GitHub (" + floorGitHub(w) + ")"
	case WaitRunner:
		line = "waits on a computer to run it"
	case WaitMainAgent:
		line = "handed to its main agent, which is deciding what to do"
	case WaitYou:
		line = "waits on the person to " + firstNonEmpty(actionWords[w.Action], w.Action)
		if w.PutAway {
			line += ", put away by them"
		}
	case WaitBacklog:
		if w.What == "stopped" {
			line = "stopped by the person"
		}
	}
	if st := it.Stall; st != nil {
		stalled := "stalled"
		if words := cutLine(firstLine(st.Words), floorStallMost); words != "" {
			stalled += ": " + words
		}
		if !st.GaveUp {
			stalled += " (reported to its main agent)"
		}
		if line != "" {
			line += " · "
		}
		line += stalled
	}
	return line
}

// FloorWaitLine says the job's wait and what it accepts from its person in
// whole sentences, for a refusal and for one job read in full: "It waits on
// GitHub (checks). It accepts from the person now: task."
func FloorWaitLine(it Item) string {
	w, _ := floorWait(it)
	var line string
	switch w.Who {
	case WaitWorking:
		line = "It waits on nobody: " + floorWorking(w) + " is under way."
	case WaitGitHub:
		line = "It waits on GitHub (" + floorGitHub(w) + ")."
	case WaitRunner:
		line = "It waits on a computer to run it."
	case WaitMainAgent:
		line = "It is with its main agent, which is deciding what to do."
	case WaitYou:
		line = "It waits on the person to " + firstNonEmpty(actionWords[w.Action], w.Action) + "."
		if w.PutAway {
			line += " They put it away."
		}
	case WaitBacklog:
		line = "It has not started."
		if w.What == "stopped" {
			line = "The person stopped it."
		}
	case WaitDone:
		return "It is finished."
	}
	if offers := PersonOffers(it); len(offers) > 0 {
		line += " It accepts from the person now: " + strings.Join(offers, ", ") + "."
	} else {
		line += " It accepts nothing from the person now."
	}
	return strings.TrimSpace(line)
}
