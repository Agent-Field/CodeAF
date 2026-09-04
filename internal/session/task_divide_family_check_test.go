package session

// ONE FAMILY-WIDE CHECK IS THE PARENT'S, AND IT IS RUN ONCE (#569).
//
// A division hands out parts that run at the same time, each in a worktree of
// its own. So a done-condition copied into every part is not one check — it is
// as many runs of that check as there are parts, all of them concurrent, none
// of them able to see what the others wrote, and then one more run by the parent
// after the parts are integrated. The measured shape is three parts each ordered
// to run `go test ./internal/tui3/...`, a suite that takes eight minutes on an
// idle box: four runs of it where the work needed one, three of them judging a
// tree that does not yet hold the other parts' files and so cannot be right
// about the family anyway.
//
// THE LAW THE DIVISION WANTED: A PART'S DONE-CONDITION IS SCOPED TO THE FILES
// THAT PART OWNS, AND A CHECK OVER THE WHOLE FAMILY RUNS ONCE, BY THE PARENT,
// AFTER INTEGRATION. That is the same sentence the scope rule already makes
// about paths (task_divide_scope.go) said about commands: what a part is
// finished against describes the part, not the family it belongs to.
//
// These cases drive the real divide door, exactly as the scope-admission cases
// beside them do (task_divide_test.go's newDivideNest / divideArgsFor), because
// the whole of the question is which child specs the door actually admits.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// familySuite is the check that belongs to the whole family and to no part of
// it. It is written out rather than built so that the string a reader sees in
// the failure message is the string the parts were actually given.
const familySuite = "go test ./internal/tui3/..."

// carriesTheFamilySuite reads one admitted part's done-condition and answers
// whether that part has been ORDERED TO RUN THE FAMILY-WIDE SUITE.
//
// IT IS DELIBERATELY GENEROUS ABOUT SPELLING, and that is the point rather than
// a convenience: a repair that hoists the family check off the children must
// hoist it however the model happened to spell it, so a reading that only knew
// the one literal would call `go test  ./internal/tui3/` a different check and
// pass a division that is still four runs of one suite. Whitespace is collapsed,
// case is dropped, and the trailing `/` and `...` that spell the same package
// tree are cut, which is the same equivalence
// [TestEquivalentSpellingsOfOneCheckAreOneCommand] pins below.
func carriesTheFamilySuite(acceptance string) bool {
	flat := strings.ToLower(strings.Join(strings.Fields(acceptance), " "))
	for _, word := range strings.Fields(flat) {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(word, "..."), "/")
		trimmed = strings.TrimPrefix(trimmed, "./")
		if trimmed != "internal/tui3" {
			continue
		}
		if strings.Contains(flat, "go test") {
			return true
		}
	}
	return false
}

// THE MEASURED WASTE. Three parts, each with a targeted check of its own and
// each carrying the same family-wide suite, is a division that buys the suite
// four times and can only trust the last one.
//
// EITHER ENDING IS RIGHT. The door may REPAIR the division — admit the three
// parts with their own checks and leave the family suite for the parent — or it
// may REFUSE it and send the worker back to rewrite the done-conditions. What it
// may not do is what it does today: admit all three with the same eight-minute
// suite ordered in every one of them.
func TestOneFamilyWideCheckIsNotCopiedIntoEveryParallelPart(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)

	// THE GROUND IS LEFT EMPTY ON PURPOSE. Nothing here is about paths: if the
	// family tree really held `internal/tui3`, the ownership rule in
	// task_divide_scope.go would refuse this division on the shared path and
	// this test would go green without the check ever being read as a check.
	// The parts name a suite in prose, which is exactly how a worker writes one.
	parts := []dividePart{
		{Title: "rank", Summary: "s", Brief: "write rank.go",
			Acceptance: "rank_test.go passes; " + familySuite + " passes"},
		{Title: "sessions", Summary: "s", Brief: "write sessions.go",
			Acceptance: "sessions_test.go passes; " + familySuite + " passes"},
		{Title: "browse", Summary: "s", Brief: "write browse.go",
			Acceptance: "browse_test.go passes; " + familySuite + " passes"},
	}
	answer := nest.divide(t, divideArgsFor(wideEvidence, parts...))

	kids := nest.graph.children(nest.parent.id)

	// THE PLAIN REFUSAL IS AN ACCEPTABLE ANSWER. A worker told that a part
	// cannot be finished against the whole family's suite can rewrite three
	// done-conditions and ask again, which is the same road the ownership
	// refusal already puts it on.
	if len(kids) == 0 {
		if !strings.HasPrefix(answer, "not split:") {
			t.Fatalf("no parts were admitted and the worker was told %q, want either a division "+
				"whose parts carry their own checks or a refusal that says so", answer)
		}
		return
	}

	if len(kids) != len(parts) {
		t.Fatalf("the division bore %d parts, want %d or a plain refusal; the worker was told %q",
			len(kids), len(parts), answer)
	}
	byTitle := make(map[string]*TaskNode, len(kids))
	for _, kid := range kids {
		byTitle[kid.title()] = kid
	}

	// EACH PART KEEPS ITS OWN TARGETED CHECK. Hoisting the family suite off the
	// children may not take their own done-conditions with it: a part with
	// nothing to be finished against is a part nobody can check.
	for _, own := range []struct{ title, check string }{
		{"rank", "rank_test.go"},
		{"sessions", "sessions_test.go"},
		{"browse", "browse_test.go"},
	} {
		kid := byTitle[own.title]
		if kid == nil {
			t.Errorf("no part called %q was admitted; the parts are %v", own.title, titlesOf(kids))
			continue
		}
		if !strings.Contains(kid.acceptance(), own.check) {
			t.Errorf("the part %q is finished against %q, want it to keep its own check %q",
				own.title, kid.acceptance(), own.check)
		}
	}

	// AND AT MOST ONE ORDER TO RUN THE FAMILY SUITE SURVIVES ACROSS THE PARTS —
	// ideally none, because the suite belongs to the parent, after integration.
	var carried []string
	for _, kid := range kids {
		if carriesTheFamilySuite(kid.acceptance()) {
			carried = append(carried, fmt.Sprintf("%q", kid.title()))
		}
	}
	sort.Strings(carried)
	if len(carried) > 1 {
		t.Fatalf("%d of the %d admitted parts are each ordered to run the family-wide check %q — %s — "+
			"so that suite runs %d times where the work needed it once, and every part but the last "+
			"judges a tree that does not hold its siblings' files yet; want it carried by at most one "+
			"part, or the division refused",
			len(carried), len(kids), familySuite, strings.Join(carried, ", "), len(carried)+1)
	}
}

// titlesOf names the admitted parts in a stable order, for the failure messages
// above: which parts exist is the first thing a reader of a red here wants.
func titlesOf(kids []*TaskNode) []string {
	names := make([]string, 0, len(kids))
	for _, kid := range kids {
		names = append(names, kid.title())
	}
	sort.Strings(names)
	return names
}

// ── the rule underneath it: WHEN ARE TWO SPELLINGS ONE COMMAND ────────────────
//
// THE HELPER THIS SECTION ASKS FOR DOES NOT EXIST YET. Nothing in
// task_checks.go answers "are these two spellings the same check": the door
// there dedupes on the exact bytes ([appendChecks]) and reads a span for its
// SHAPE ([commandLike]) or for whether it could run at all ([runnableHere]),
// which are different questions. Hoisting a family-wide check off the parts
// needs the missing one, because a worker writes `go test ./internal/tui3/...`
// into one part and `go test ./internal/tui3` into the next and means the same
// eight minutes both times.
//
// SO THE RULE IS PINNED HERE AND THE READING IS A SEAM. The lane that lands the
// fix writes normalizedCheckCommand (or whatever it ends up called) in
// task_checks.go and points the seam below at it in ONE LINE; until then this
// test fails saying exactly what is missing, rather than failing the whole
// package to compile and taking the case above down with it.

// normalizedCheckCommandUnderTest is that seam, and it now points at the real
// reading: task_checks.go's [normalizedCheckCommand].
var normalizedCheckCommandUnderTest = normalizedCheckCommand

// TWO SPELLINGS OF ONE CHECK ARE ONE COMMAND, AND TWO FILTERS ARE TWO CHECKS.
// The first half is what makes hoisting possible at all; the second is the floor
// under it — `-run A` and `-run B` are different work, and folding them together
// would drop a check somebody asked for.
func TestEquivalentSpellingsOfOneCheckAreOneCommand(t *testing.T) {
	normalize := normalizedCheckCommandUnderTest
	if normalize == nil {
		t.Fatalf("there is no one reading of a check command in this package: task_checks.go has "+
			"nothing that answers whether %q and %q are the same check, and hoisting a family-wide "+
			"check off the parts of a division cannot be done without one",
			"go test ./internal/tui3/...", "go test ./internal/tui3")
	}

	// THE SAME COMMAND, FOUR WAYS. A trailing slash, the whitespace a model
	// happened to type, and the `-count=1` that changes nothing about WHICH suite
	// runs are all one order to run one suite.
	//
	// THE PACKAGE-TREE ELLIPSIS IS NOT ONE OF THEM, and this table used to say it
	// was, which was wrong: `./internal/tui3` measures ONE package and
	// `./internal/tui3/...` measures the whole subtree under it, so they are two
	// different amounts of work and folding them together would let a division
	// hoist away a check nobody else was going to make. It is pinned as a
	// difference below.
	same := []string{
		"go test ./internal/tui3",
		"go test ./internal/tui3/",
		"go test  ./internal/tui3 ",
		"go test ./internal/tui3 -count=1",
	}
	first := normalize(same[0])
	for _, spelling := range same[1:] {
		if got := normalize(spelling); got != first {
			t.Errorf("%q reads as %q and %q reads as %q, want one command: they run the same suite",
				spelling, got, same[0], first)
		}
	}

	// AND A PACKAGE IS NOT ITS SUBTREE.
	if tree := normalize("go test ./internal/tui3/..."); tree == first {
		t.Errorf("the subtree reads as %q, the same as the one package, want them told apart: "+
			"they run different amounts of work", first)
	}

	// AND A FILTER IS NOT A PATH. `-run TestHTTP/` names TestHTTP AND ITS
	// SUBTESTS and `-run TestHTTP` names the test alone, so the trailing
	// character is the whole difference between them — and a normalisation that
	// cut a trailing separator off every word would fold two genuinely different
	// filters into one and refuse a division over it, which is exactly the
	// failure the rule above promises not to cause.
	subtests := normalize("go test ./pkg -run TestHTTP/")
	if alone := normalize("go test ./pkg -run TestHTTP"); subtests == alone {
		t.Errorf("both filters read as %q, want two commands: one runs the subtests too", alone)
	}

	// AND TWO FILTERS STAY TWO CHECKS.
	one := normalize("go test ./internal/tui3/... -run TestAlpha")
	other := normalize("go test ./internal/tui3/... -run TestBeta")
	if one == other {
		t.Errorf("both -run filters read as %q, want two commands: they run different tests", one)
	}
	if one == first {
		t.Errorf("a -run filter reads as %q, the same as the whole suite, want them told apart", first)
	}
}

// ── the road after the refusal ────────────────────────────────────────────────

// THE REFUSAL IS ONLY HALF THE ANSWER. What must happen next is a RE-ASK: the
// worker rewrites the three done-conditions so each part proves its own slice,
// keeps the family-wide run in its OWN done-condition, and calls the verb again —
// and the second ask is admitted. The two roads that must NOT be taken are the
// two a bare "no" would leave open: falling back to one undivided worker, and
// quietly dropping the shared check so nobody makes it at all.
//
// THE PARENT'S OWN DONE-CONDITION IS THE PROOF THAT IT WAS MOVED RATHER THAN
// DROPPED, and reading it is all this test does with it: nothing writes a spec
// after admission ([TestEachPartCarriesItsOwnDoneConditionAndTheParentKeepsTheOriginal]),
// so the family-wide command was already the parent's before the first ask and is
// still the parent's after the second.
func TestARefusedDivisionComesBackAdmittedOnceEachPartProvesItsOwnSlice(t *testing.T) {
	// The parent is armed by its own brief and finished against the family-wide
	// suite, which is where that run belongs.
	parentDone := "the adapters build; " + familySuite + " passes"
	nest := newDivideNestFrom(t, taskSpec{title: "the whole job", request: personSentence,
		brief: wideBrief, acceptance: parentDone, depth: 1}, 0, &scriptedCompleter{}, nil)

	refused := nest.divide(t, divideArgsFor(wideEvidence,
		dividePart{Title: "rank", Summary: "s", Brief: "write rank.go",
			Acceptance: "rank_test.go passes; " + familySuite + " passes"},
		dividePart{Title: "sessions", Summary: "s", Brief: "write sessions.go",
			Acceptance: "sessions_test.go passes; " + familySuite + " passes"},
		dividePart{Title: "browse", Summary: "s", Brief: "write browse.go",
			Acceptance: "browse_test.go passes; " + familySuite + " passes"}))

	if !strings.HasPrefix(refused, "not split:") {
		t.Fatalf("the worker was told %q, want the division refused", refused)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts exist after the refusal, want none admitted", len(kids))
	}
	// IT NAMES THE COMMAND AND IT ASKS AGAIN. Without the first the worker has
	// four done-conditions to re-read; without the second it is free to read the
	// refusal as "this work is not divisible" and carry on alone, which is the
	// floor gates' finding and not this one.
	if !strings.Contains(refused, familySuite) {
		t.Errorf("the refusal never says which check: %q", refused)
	}
	if !strings.Contains(refused, "ask again") {
		t.Errorf("the refusal never asks the worker to come back: %q", refused)
	}
	if !strings.Contains(refused, "its own slice") {
		t.Errorf("the refusal never says what each part should be finished against: %q", refused)
	}

	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 1 {
		t.Fatalf("the record holds %d divisions, want the one that was put", len(divisions))
	}
	if divisions[0].Decision != divisionRefusedShared {
		t.Fatalf("the record says %q, want %q", divisions[0].Decision, divisionRefusedShared)
	}
	// AND THE RECORD KEEPS THE COMMAND, so an autopsy can prove which run was the
	// family's rather than counting shell calls across worktrees that are gone.
	if len(divisions[0].Shared) == 0 {
		t.Errorf("the record names no shared check, want the one every part was ordered to run")
	}
	for _, command := range divisions[0].Shared {
		if !strings.Contains(command, "internal/tui3") {
			t.Errorf("the record names %q as shared, want the family-wide suite", command)
		}
	}

	// AND THE SECOND ASK — the same three parts, each finished against its own
	// slice, with the whole run left where it already was. THEY ARE STILL
	// COMMANDS, and commands run by the same program: what makes them three
	// checks rather than one is that each NAMES SOMETHING DIFFERENT, which is
	// the whole of what this rule reads.
	admitted := nest.divide(t, divideArgsFor(wideEvidence,
		dividePart{Title: "rank", Summary: "s", Brief: "write rank.go",
			Acceptance: "go test ./internal/tui3/rank passes"},
		dividePart{Title: "sessions", Summary: "s", Brief: "write sessions.go",
			Acceptance: "go test ./internal/tui3/sessions passes"},
		dividePart{Title: "browse", Summary: "s", Brief: "write browse.go",
			Acceptance: "go test ./internal/tui3/browse passes"}))

	if !strings.HasPrefix(admitted, "split into 3 parts:") {
		t.Fatalf("the second ask was told %q, want the division taken", admitted)
	}
	// THE WORK DID NOT FALL BACK TO ONE PAIR OF HANDS. Three parts, not none and
	// not one: the refusal above was about the done-conditions and the division
	// was right all along.
	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the second ask bore %d parts, want 3; the worker was told %q", len(kids), admitted)
	}
	distinct := make(map[string]bool, len(kids))
	for _, kid := range kids {
		if carriesTheFamilySuite(kid.acceptance()) {
			t.Errorf("the part %q is still ordered to run the family-wide suite: %q",
				kid.title(), kid.acceptance())
		}
		distinct[kid.acceptance()] = true
	}
	if len(distinct) != len(kids) {
		t.Errorf("%d parts carry %d done-conditions between them, want one each: %v",
			len(kids), len(distinct), titlesOf(kids))
	}

	// AND THE CHECK WAS MOVED, NOT DROPPED. The parent's own done-condition is
	// the one place the family-wide run now lives, and it is untouched.
	if got := nest.parent.acceptance(); got != parentDone {
		t.Fatalf("the parent is finished against %q, want its own %q unchanged", got, parentDone)
	}
	if !carriesTheFamilySuite(nest.parent.acceptance()) {
		t.Fatalf("nobody is left to run %q: the parent is finished against %q",
			familySuite, nest.parent.acceptance())
	}
}

// ── the two false refusals: prose, and a clause cut inside a quotation ────────

// A SENTENCE THAT OPENS WITH A PROGRAM'S NAME IS STILL A SENTENCE. "make
// targets are documented" is English about a repository and not an order to run
// anything, and two parts that both say it are two parts that agree about the
// shape of the work — which is what an honest division looks like. A reading
// that took the first word as the whole answer refused them, and handed the
// worker its own prose back quoted as though it were a shell line.
func TestTwoPartsSharingAProseConditionThatOpensWithAProgramAreAdmitted(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, divideArgsFor(wideEvidence,
		dividePart{Title: "rank", Summary: "s", Brief: "write rank.go",
			Acceptance: "rank.go is written; make targets are documented"},
		dividePart{Title: "browse", Summary: "s", Brief: "write browse.go",
			Acceptance: "browse.go is written; make targets are documented"}))

	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division taken: neither part was ordered to run anything", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 2 {
		t.Fatalf("the division bore %d parts, want 2", len(kids))
	}
}

// AND "make sure the tests still pass" IS THE SAME MISTAKE IN THE WORDS PEOPLE
// ACTUALLY WRITE. It is worth its own case because it is the sentence a worker
// is most likely to put in every part of a division by hand.
func TestTwoPartsSharingMakeSureTheTestsStillPassAreAdmitted(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, divideArgsFor(wideEvidence,
		dividePart{Title: "rank", Summary: "s", Brief: "write rank.go",
			Acceptance: "rank.go is written. make sure the tests still pass"},
		dividePart{Title: "browse", Summary: "s", Brief: "write browse.go",
			Acceptance: "browse.go is written. make sure the tests still pass"}))

	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division taken", answer)
	}
}

// A CLAUSE CUT INSIDE A QUOTATION IS NOT A CLAUSE. The splitting knows nothing
// about quotes, so two genuinely different quoted commands both leave behind the
// fragment in front of the semicolon — `sh -c 'test -f a` — which is identical in
// both parts and is one command in neither. Refusing on it is a division lost
// over a piece of text nobody wrote.
func TestTwoQuotedCommandsThatDifferInsideTheQuotesAreAdmitted(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	answer := nest.divide(t, divideArgsFor(wideEvidence,
		dividePart{Title: "alpha", Summary: "s", Brief: "write a",
			Acceptance: "sh -c 'test -f a; echo A'"},
		dividePart{Title: "beta", Summary: "s", Brief: "write b",
			Acceptance: "sh -c 'test -f a; echo B'"}))

	if !strings.HasPrefix(answer, "split into 2 parts:") {
		t.Fatalf("the worker was told %q, want the division taken: the two parts run different commands", answer)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 2 {
		t.Fatalf("the division bore %d parts, want 2", len(kids))
	}
}

// ── the second telling: the harness lifts what the worker could not ──────────

// familyParts is the measured shape — parts that each have a check of their own
// and each carry the family's suite — written once because the cases below put
// the SAME SHAPE twice, and "the same shape" is the whole point of them.
//
// THE NUMBER OF PARTS IS THE CALLER'S because a task may hand out only so many
// pieces in its life ([TaskGraph.claimChild]), and a case that asks three times
// has to fit three asks under that ceiling rather than trip it and report the
// wrong refusal.
func familyParts(titles ...string) []dividePart {
	parts := make([]dividePart, 0, len(titles))
	for _, title := range titles {
		parts = append(parts, dividePart{Title: title, Summary: "s",
			Brief:      "write " + title + ".go",
			Acceptance: title + "_test.go passes; " + familySuite + " passes"})
	}
	return parts
}

// A REFUSAL A WORKER CANNOT ACT ON IS A DOOR IT BURNS ITS STEPS AGAINST. On a
// cheap model the divider re-asked with the same shape four times, collected
// four refusals and ended `stopped: 6 steps without progress` with all three
// parts' files already written. So the FIRST telling refuses — a worker that can
// rewrite its done-conditions still does — and the SECOND admits the division
// with the shared command taken off every part and given to the parent.
func TestASecondAskWithTheSameSharedCheckIsRepairedRatherThanRefusedAgain(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)

	refused := nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "sessions", "browse")...))
	if !strings.HasPrefix(refused, "not split:") {
		t.Fatalf("the first ask was told %q, want the refusal: a worker that can redraw its "+
			"done-conditions must still be asked to", refused)
	}
	if kids := nest.graph.children(nest.parent.id); len(kids) != 0 {
		t.Fatalf("%d parts exist after the first ask, want none", len(kids))
	}

	// AND THE SAME SHAPE AGAIN, which is the worker saying by doing it that it
	// cannot take the road it was pointed at.
	admitted := nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "sessions", "browse")...))
	if !strings.HasPrefix(admitted, "split into 3 parts:") {
		t.Fatalf("the second ask was told %q, want the division admitted: refusing twice is a "+
			"worker spending its steps against a door", admitted)
	}
	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the second ask bore %d parts, want 3", len(kids))
	}

	// THE CHECK IS OFF EVERY PART AND NOT MERELY OFF TWO. It is the family's, so
	// leaving it on the first part would keep exactly the run that judges a tree
	// without its siblings' files in it.
	for _, kid := range kids {
		if carriesTheFamilySuite(kid.acceptance()) {
			t.Errorf("the part %q was admitted still ordered to run the family-wide suite: %q",
				kid.title(), kid.acceptance())
		}
	}
	// AND EACH PART KEPT ITS OWN. A part with nothing to be finished against is
	// a part nobody can check.
	for _, own := range []struct{ title, check string }{
		{"rank", "rank_test.go"}, {"sessions", "sessions_test.go"}, {"browse", "browse_test.go"},
	} {
		for _, kid := range kids {
			if kid.title() == own.title && !strings.Contains(kid.acceptance(), own.check) {
				t.Errorf("the part %q is finished against %q, want it to keep %q",
					own.title, kid.acceptance(), own.check)
			}
		}
	}

	// AND THE PARENT OWNS IT NOW.
	family := nest.parent.familyChecks()
	if len(family) != 1 || !strings.Contains(family[0], "internal/tui3") {
		t.Fatalf("the parent owns %v, want the family-wide check that came off its parts", family)
	}

	// AND THE RECORD TELLS THE TWO TELLINGS APART.
	divisions := journaledDivisions(t, nest.journal)
	if len(divisions) != 2 {
		t.Fatalf("the record holds %d divisions, want the two that were put", len(divisions))
	}
	if divisions[0].Decision != divisionRefusedShared {
		t.Errorf("the first line says %q, want %q", divisions[0].Decision, divisionRefusedShared)
	}
	if divisions[1].Decision != divisionRepairedShared {
		t.Fatalf("the second line says %q, want %q: an autopsy has to tell a worker that fixed it "+
			"from a harness that moved it", divisions[1].Decision, divisionRepairedShared)
	}
	if divisions[1].Admitted != 3 {
		t.Errorf("the repaired line says %d parts were admitted, want 3", divisions[1].Admitted)
	}
	if len(divisions[1].Shared) == 0 {
		t.Errorf("the repaired line names nothing that moved, want the family-wide check")
	}
}

// A REPEATED ACTION NOBODY DECLARED IS A LINT, NOT A PERMISSION.
//
// This is the negative control for the family road, and it is the leak that road
// had: the duplicate-work rule reads a part's done-condition PROSE for commands
// two parts both order, and everything it found used to land on the parent's own
// checker as something it could issue. So `./counter.sh` written into three
// acceptances — a real action, nobody's declared verification — became a command
// the parent's checker was invited to run, which is the prose harvest coming back
// by another road.
//
// THE LINT ITSELF IS KEPT, because it is worth having: the repeat is still found,
// still taken off every part, and still told to the parent's WORKER, whose hands
// are ordinary hands. What it does not do any more is hand a checker a verb.
func TestAProseRepeatedActionIsLiftedWithoutBecomingCheckerAuthority(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	// THE SPELLING IS ONE THE LINT CAN SEE: it reads a done-condition clause as a
	// command only when the first word is a program the shell would find and some
	// later word names something findable ([ordersWork]), which is what makes this
	// a control over the road that really lifts rather than over one that never
	// fires.
	const action = "sh ./counter.sh --all"
	repeating := func() []dividePart {
		parts := familyParts("rank", "sessions", "browse")
		for i := range parts {
			parts[i].Acceptance = parts[i].Title + "_test.go passes; " + action + " passes"
		}
		return parts
	}
	nest.divide(t, divideArgsFor(wideEvidence, repeating()...))
	admitted := nest.divide(t, divideArgsFor(wideEvidence, repeating()...))
	if !strings.HasPrefix(admitted, "split into 3 parts:") {
		t.Fatalf("the second ask was told %q, want the division admitted", admitted)
	}

	// THE LINT RAN: the repeat came off every part and the parent was told it owns
	// it, in the sentence its worker reads for what finishing means.
	// The clause is compared as it stands, trailing words and all ([orderedChecks]
	// says why), so what the parent owns is the sentence the parts repeated.
	family := nest.parent.familyChecks()
	if len(family) != 1 || !strings.Contains(family[0], action) {
		t.Fatalf("the parent was given %q, want the repeated action the parts all carried", family)
	}
	if told := nest.parent.instruction(); !strings.Contains(told, action) {
		t.Fatalf("the parent is never told about the repeat it now owns:\n%s", told)
	}
	for _, kid := range nest.graph.children(nest.parent.id) {
		if strings.Contains(kid.acceptance(), action) {
			t.Errorf("the part %q is still ordered to repeat the action: %q", kid.title(), kid.acceptance())
		}
	}

	// AND NO CHECKER GAINED A VERB. Not the parent's, which is where the lift
	// landed, and not any part's.
	ground := t.TempDir()
	for _, node := range append(nest.graph.children(nest.parent.id), nest.parent) {
		door := auditDoorFor(node, ground)
		if len(door.checks) != 0 {
			t.Fatalf("%q's checker was handed %q, and nobody declared any of it", node.title(), door.checks)
		}
		if _, ok := doorRefusal(action, door); ok {
			t.Fatalf("%q's checker may run the action its parts merely repeated", node.title())
		}
		if strings.Contains(door.offer(), "counter.sh") {
			t.Fatalf("%q's checker is offered the action nobody declared:\n%s", node.title(), door.offer())
		}
	}
}

// AND A FAMILY CHECK DECLARED IN THE TYPED FIELD MOVES THE SAME WAY.
//
// A part's `checks` is what its own checker will really run (task_checks.go), so
// a suite left standing there is the whole family's suite run once per part —
// the same waste, arriving through the typed door instead of through prose. It
// comes off every part's list and lands on the parent, whose door then opens on
// it and whose parts' doors do not.
func TestASharedCheckDeclaredByEveryPartIsLiftedOffTheirOwnDoors(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	declaring := func() []dividePart {
		parts := familyParts("rank", "sessions", "browse")
		for i := range parts {
			parts[i].Acceptance = parts[i].Title + "_test.go passes"
			parts[i].Checks = []string{"go test ./internal/" + parts[i].Title, familySuite}
		}
		return parts
	}
	// The first ask is the refusal a worker that can redraw its own division still
	// gets; the second is the harness lifting what the worker could not.
	nest.divide(t, divideArgsFor(wideEvidence, declaring()...))
	admitted := nest.divide(t, divideArgsFor(wideEvidence, declaring()...))
	if !strings.HasPrefix(admitted, "split into 3 parts:") {
		t.Fatalf("the second ask was told %q, want the division admitted", admitted)
	}

	kids := nest.graph.children(nest.parent.id)
	if len(kids) != 3 {
		t.Fatalf("the division bore %d parts, want 3", len(kids))
	}
	for _, kid := range kids {
		checks := kid.repeatableChecks()
		if containsWord(checks, familySuite) {
			t.Errorf("the part %q is still declared to run the family's suite: %q", kid.title(), checks)
		}
		if len(checks) != 1 || !strings.Contains(checks[0], kid.title()) {
			t.Errorf("the part %q kept %q, want its own check and nothing else", kid.title(), checks)
		}
		if door := auditDoorFor(kid, t.TempDir()); containsWord(door.checks, familySuite) {
			t.Errorf("the part %q's own checker still opens on the family's suite: %q", kid.title(), door.checks)
		}
	}
	family := nest.parent.familyChecks()
	if len(family) != 1 || family[0] != familySuite {
		t.Fatalf("the parent owns %v, want the family-wide check that came off its parts", family)
	}
	if door := auditDoorFor(nest.parent, t.TempDir()); !containsWord(door.checks, familySuite) {
		t.Fatalf("the parent's own door holds %q, want the check only it can honestly make", door.checks)
	}
}

// THE PARENT IS TOLD WHAT IT NOW OWNS, in the sentence a worker reads to find
// out what finishing means — and told WHEN, because a parent that ran it the
// moment it noticed it would be making the parts' own mistake one level up.
func TestTheParentsInstructionNamesTheFamilyChecksItWasGiven(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)

	// A NODE THAT OWNS NOTHING DRAWS NOTHING. The emptiness law, and the reason
	// this line is safe on the road every node's brief travels.
	if before := nest.parent.instruction(); strings.Contains(before, familyChecksRule) {
		t.Fatalf("a node that owns no family checks is already told about them: %q", before)
	}

	nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "sessions", "browse")...))
	nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "sessions", "browse")...))

	told := nest.parent.instruction()
	if !strings.Contains(told, familySuite) {
		t.Fatalf("the parent is never told which check it owns:\n%s", told)
	}
	if !strings.Contains(told, familyChecksRule) {
		t.Fatalf("the parent is told the command with no sentence saying whose it is or when it "+
			"runs, which is how it comes to run it before its parts are home:\n%s", told)
	}
	// AND IT IS SAID UNDER THE DONE-CONDITION, because that is the section a
	// worker reads for what finishing means.
	if at := strings.Index(told, briefDoneHeading); at < 0 || at > strings.Index(told, familyChecksRule) {
		t.Fatalf("the family's checks are not under %q:\n%s", briefDoneHeading, told)
	}
}

// AND THE PARENT'S OWN DOOR OPENS ON THE ONES THAT WERE DECLARED. A node that
// owns its family's DECLARED verification and whose checker could not run it
// would be a permission lost on the way up; a node whose checker gained a verb
// out of prose would be a permission invented on the way up. The door is the
// first and never the second ([declaredAmong]).
func TestTheParentsAuditDoorOffersTheDeclaredFamilyChecksItOwns(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	declaring := func() []dividePart {
		parts := familyParts("rank", "sessions", "browse")
		for i := range parts {
			parts[i].Acceptance = parts[i].Title + "_test.go passes"
			parts[i].Checks = []string{familySuite}
		}
		return parts
	}
	nest.divide(t, divideArgsFor(wideEvidence, declaring()...))
	nest.divide(t, divideArgsFor(wideEvidence, declaring()...))

	door := auditDoorFor(nest.parent, t.TempDir())
	if !strings.Contains(door.offer(), familySuite) {
		t.Fatalf("the parent's door offers %q, want the family-wide check it now owns", door.offer())
	}
}

// AND A THIRD ASK IS NOT A THIRD TELLING. Once the node has been told, every
// later ask takes the repair road — and the parent must not collect the same
// command twice, which would be a run somebody paid for twice and a
// done-condition that said one thing in two places.
func TestAThirdAskAfterARepairNeitherRefusesNorDoublesTheFamilysChecks(t *testing.T) {
	nest := newDivideNest(t, wideBrief, 0)
	// TWO PARTS AN ASK, so that three asks fit under the number of pieces one
	// task may ever hand out. A third ask that tripped the fan ceiling would
	// report a refusal this case is not about.
	nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "browse")...))
	nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "browse")...))

	third := nest.divide(t, divideArgsFor(wideEvidence, familyParts("rank", "browse")...))
	if !strings.HasPrefix(third, "split into 2 parts:") {
		t.Fatalf("the third ask was told %q, want the division admitted", third)
	}
	if family := nest.parent.familyChecks(); len(family) != 1 {
		t.Fatalf("the parent owns %v, want the one check it was given, listed once", family)
	}
}
