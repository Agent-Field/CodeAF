package session

// WIRING THE PRINCIPAL INTO ONE SESSION.
//
// principal.go says what a principal IS and what the two of them answer. This
// file is the seam: which one a session gets, what it is told, and the three
// small readings the roads that consult it are built out of.
//
// THE FIELD IS WRITTEN ONCE AND NEVER AGAIN ([newPrincipalFor], called from
// [newAgent]), which is what lets every road read `a.principal` without the
// session lock. A principal that could be swapped mid-session would be a
// session whose acceptance and budget mean different things at different
// moments, and there is no door in this build that wants one.

import (
	"fmt"
	"strings"
)

// principalEar is the half of a principal that is TOLD things rather than
// asked. It is unexported and off the interface on purpose: [Principal] is what
// the roads consult, and being informed of the person's own words is this
// package's business with the two implementations it built.
type principalEar interface{ hear(ask string) }

// newPrincipalFor picks this session's principal, and THE BUDGET IS WHAT PICKS
// IT.
//
// Three readings, in order, and each of the first two is a reason a session
// stays exactly as it was:
//
//   - A SESSION SOMEBODY IS SITTING IN FRONT OF GETS A [Person]. `--yolo` is
//     what says otherwise, and it reaches this package as [Config.Unattended].
//   - AN UNATTENDED SESSION WITH NO CEILING ALSO GETS A [Person], and the door
//     says one line about it at launch. This is the rule the whole feature turns
//     on: carrying a conversation on by itself is spending, and spending
//     unasked-for money on an unstated ceiling is not something a flag about
//     TOOL APPROVALS may be read as permission for.
//   - AND AN UNATTENDED SESSION WITH ONE GETS A [Steward].
//
// The spend closure reads the session's own journaled figure under its own
// lock, which is the figure rail.go bounds against — one ledger, two rails.
func newPrincipalFor(a *Agent) Principal {
	// AND ONLY A CONVERSATION MAY HAVE ONE, WHICH IS THE GUARD AND NOT A
	// PREFERENCE.
	//
	// A goal owner holds the WHOLE ask, spends a budget against it, and sweeps
	// what the session left behind. None of those is a thing a worker owns: a
	// node has a brief and an auditor of its own, a fork's hand has a scope, an
	// errand is forty cells that close with home. Every one of them is built
	// from a fresh Config literal today and would inherit none of this — but two
	// roads COPY the conversation's config wholesale (standing_run.go), and a
	// third written next year will too. The guard belongs here, once, where the
	// answer is decided, rather than as a line every copier has to remember.
	if a.config.InTask || a.config.Errand || a.config.inHand {
		return NewPerson()
	}
	if !a.config.Unattended || !a.config.Budget.Set() {
		return NewPerson()
	}
	steward := NewSteward("", a.config.Budget, func() float64 {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.usage.CostUSD
	})
	// AND IT COUNTS ITS HOURS FROM THE SESSION'S CLOCK, not from a second one it
	// started for itself a microsecond later. There is one wall clock in this
	// package ([Agent.startedAt]) and this is where it is handed over; a
	// [Steward] built by a test starts its own, which is what a type nobody
	// wired should do.
	steward.started = a.startedAt
	return steward
}

// who is this session's principal, and it is NEVER NIL — an agent built before
// this field existed, or by a test that assembled one by hand, answers a
// [Person], which is the posture every such caller already had.
func (a *Agent) who() Principal {
	if a.principal == nil {
		return NewPerson()
	}
	return a.principal
}

// steward answers the [Steward] this session works for, or nil.
//
// IT IS THE ONE TYPE ASSERTION IN THIS PACKAGE and it is deliberate rather than
// a shortcut round the interface. Four things belong to an unattended session
// alone and to no principal that could be written later: writing a session
// acceptance, re-running the checks from clean before saying done, deleting
// what the session left lying about, and letting a woken turn start work. Each
// of them is a decision somebody made about THIS implementation, and spelling
// them as interface methods would put four answers on [Person] whose only
// honest value is "never".
func (a *Agent) steward() *Steward {
	steward, _ := a.who().(*Steward)
	return steward
}

// hearAsk tells the principal what was asked, in the person's own words. It is
// called from the one place a person's message is recorded, and a note the
// session wrote itself is never mistaken for one.
func (a *Agent) hearAsk(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	ear, ok := a.who().(principalEar)
	if !ok {
		return
	}
	ear.hear(text)
}

// ── THE NOTICE ──────────────────────────────────────────────────────────────

// unattendedWithoutBudget is the line a door prints when somebody started an
// unattended session and named no ceiling.
//
// IT SAYS WHAT WOULD HAVE HAPPENED AND HOW TO GET IT, which is the only kind of
// notice worth a line: "this is what you have" plus "this is the word for the
// other thing". It never appears for an attended session, and it never appears
// twice.
const unattendedWithoutBudget = "no budget was named, so this session stops when the model stops — " +
	"give it --max-hours or --max-cost and it carries its own work on until the ask is met or the budget is out"

// UnattendedNotice is the one line a door shows for an unattended session, or
// "" when there is nothing worth saying.
//
// It is a function of the config rather than a field on it so that a door
// cannot show the wrong one: the same two readings that pick the principal pick
// the sentence.
func UnattendedNotice(config Config) string {
	if !config.Unattended {
		return ""
	}
	if !config.Budget.Set() {
		return unattendedWithoutBudget
	}
	return "carrying its own work on: " + budgetWords(config.Budget)
}

// budgetWords writes a ceiling the way the person stated it, and says nothing
// about a ceiling they did not state — the emptiness law, applied to a pair of
// numbers where a zero would read as "no money at all".
func budgetWords(budget Budget) string {
	var parts []string
	if budget.Wall > 0 {
		parts = append(parts, spellDuration(budget.Wall))
	}
	if budget.USD > 0 {
		parts = append(parts, dollarsWord(budget.USD))
	}
	return strings.Join(parts, " · ")
}

// dollarsWord writes a budget in dollars the way somebody typed it: a round
// figure keeps no cents it does not have.
func dollarsWord(usd float64) string {
	if usd == float64(int64(usd)) {
		return fmt.Sprintf("$%d", int64(usd))
	}
	return fmt.Sprintf("$%.2f", usd)
}

// ── THE READINGS A DECISION IS MADE ON ──────────────────────────────────────

// landings is how this session's units of work came home, newest last, as the
// principal is shown them ([Landing]).
//
// IT READS THE LIVE GRAPH AND NOT THE INDEX ON DISK. The index carries every
// landing this project has ever had, from every session; the question here is
// what THIS session finished, and the graph in front of us is the only thing
// that answers it without filtering somebody else's rows.
//
// A node still running or still queued is not a landing and is left out — but
// it does make [Remains.Landed] false, because a session with work in flight
// has not finished anything and must never be told it has.
func (a *Agent) landings() ([]Landing, bool) {
	graph := a.tasker()
	if graph == nil {
		return nil, false
	}
	// THE NODES ARE TAKEN UNDER THE GRAPH LOCK AND READ WITHOUT IT. Every
	// accessor below takes that same lock for itself (task_run.go), so holding
	// it across them would be this function deadlocking on its own first read.
	graph.mu.Lock()
	nodes := make([]*TaskNode, 0, len(graph.order))
	for _, id := range graph.order {
		if node := graph.nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	graph.mu.Unlock()

	var out []Landing
	settled := 0
	for _, node := range nodes {
		state := node.stateNow()
		if !state.settled() {
			continue
		}
		settled++
		report, _, _, _ := node.leavings()
		out = append(out, Landing{
			ID:     node.id,
			Title:  node.title(),
			State:  state,
			Report: report,
			// The signature is the failure's own first line, which is what the
			// audit wrote when it said what was missing. IT IS A STAND-IN AND
			// SAYS SO: the classification lane at the provider boundary is where
			// "the same failure" is going to be decided properly, and a Landing
			// carrying a word it wrote drops straight into this field with
			// nothing here to change.
			Signature: landingSignature(state, report),
		})
	}
	return out, settled > 0
}

// landingSignature is what makes two failures the same failure, until something
// better classifies them.
//
// THE FIRST LINE OF THE REPORT, and nothing else. An audit that found the same
// gap twice writes the same first line twice; a node killed for a different
// reason writes a different one. It is a weak reading and it is the honest
// strength of what this session knows on its own — and because
// [Steward.Report] never counts an empty signature, a landing with no report at
// all is left uncounted rather than folded in with every other silent one.
func landingSignature(state TaskState, report string) string {
	if state != TaskFailed && state != TaskUnverified {
		return ""
	}
	return strings.TrimSpace(firstLine(report))
}

// remainsFor assembles what the principal decides on at the end of a turn: the
// reader's line, this session's acceptance, and how the work landed.
//
// THE CHECKS ARE NOT IN IT AND THAT IS THE POINT. Running the session's checks
// costs a process each and takes as long as the checks take, so they are run
// once, at the one moment their answer can change anything: after a principal
// has said the ask is met (principal_audit.go). Everything here is a read of
// what the session already holds.
func (a *Agent) remainsFor(said, reader string) Remains {
	landings, landed := a.landings()
	return Remains{
		Said:       said,
		Reader:     reader,
		Acceptance: a.who().Acceptance(),
		Landings:   landings,
		Landed:     landed,
	}
}

// ── ADDRESSING A LANDING ────────────────────────────────────────────────────

// incompleteClause is the tail of a landing that ran out of repair rounds, and
// WHO IT IS ADDRESSED TO IS THE WHOLE OF WHAT IT SAYS.
//
// ── THE MEASURED DEAD END ───────────────────────────────────────────────────
//
// The old sentence — "what is missing is above and the branch is kept: offer
// them a follow-up in their own words before anything else is spent on it" — is
// exactly right when somebody is reading it. It is a dead end when nobody is:
// the model answers it in words, the turn ends on words alone, and the session
// idles. A measured run reached this line at two and a half hours with seven and
// a half hours of budget left and never did another thing.
//
// So the three endings are said apart:
//
//   - A BRIEF MEANS CARRY IT ON. The goal owner read the landing and answered
//     with what the next attempt should open on, which is the audit's own
//     account of the gap — already the most specific thing anybody in this
//     session knows about it, and it is repeated here rather than pointed at
//     because the note is the whole of what the next turn reads.
//   - A PERSON MEANS OFFER IT TO THEM, unchanged, word for word.
//   - AND NEITHER MEANS SAY SO. A goal owner that has stopped — the same thing
//     three times — is not a person to offer work to and is not asking for more,
//     and the note that says nothing more is being started is the only honest
//     one left.
func incompleteClause(address landingAddress) string {
	if brief := strings.TrimSpace(address.brief); brief != "" {
		return "\nwhat is missing is above and the branch is kept · carry it on yourself from here, in that same working copy:\n" + brief
	}
	if address.person {
		return "\nwhat is missing is above and the branch is kept: offer them a follow-up in their own words before anything else is spent on it"
	}
	return "\nwhat is missing is above and the branch is kept, and nothing more is being started on it"
}

// addressLanding puts one landing to this session's goal owner and returns what
// it said ([Principal.Report]).
//
// IT IS CALLED EXACTLY ONCE PER LANDING, from the one place a node reaching a
// final state is a fact rather than a guess ([Agent.reportTaskNode]). That
// matters because Report is not a pure reading: it is where the loop guard
// counts, and a second caller would count one failure twice and stop a session
// that was making progress.
func (a *Agent) addressLanding(notice TaskNotice) landingAddress {
	principal := a.who()
	return landingAddress{
		brief:  principal.Report(landingFromNotice(notice)),
		person: a.steward() == nil,
	}
}

// quietAddress is for the roads that RE-TELL a landing somebody has already
// been told about — a parent finishing over the top of a piece still waiting to
// be settled, a session recovering its graph from disk, a node handed to the
// model on purpose. None of them is a landing arriving, so none of them puts
// anything to the goal owner: the counting already happened when it landed.
func (a *Agent) quietAddress() landingAddress {
	return landingAddress{person: a.steward() == nil}
}

// landingFromNotice reads one landing off the notice the graph already built,
// so there is no second walk of the node and no second reading of its state.
func landingFromNotice(notice TaskNotice) Landing {
	return Landing{
		ID:        notice.ID,
		Title:     notice.Title,
		State:     notice.State,
		Report:    notice.Report,
		Signature: landingSignature(notice.State, notice.Report),
	}
}
