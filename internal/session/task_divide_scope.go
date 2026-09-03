package session

// SCOPE OWNERSHIP: the parts of one division may not claim the same path.
//
// THIS IS A HARD FLOOR AND NOT A SECOND OPINION. The ledger is the contract of
// what ships — [Agent.stageTaskWork] stages the ledger and nothing else — so a
// path that two parts both wrote appears in the parent's ledger twice and is
// staged once. One version wins, silently, before git is ever asked to notice a
// conflict. That is not a merge to resolve; it is work that no longer exists.
//
// So the overlap is answered where it can still be free: at admission, before
// any part is claimed, started, or paid for. The reviewer keeps its own job of
// SHARPENING boundaries (task_divide.go's [divideReviewBrief] asks it to fix the
// boundary or fold the parts together), and this stands underneath it for the
// division the reviewer approved anyway — a model that reads the parts well is
// still a model, and the failure it lets through is invisible.
//
// AND IT IS DELIBERATELY DIM. Only the exact same normalised path claimed by two
// parts collides. Two parts working in one directory, or one part owning a
// folder while another owns a file inside it, are left alone: the measured
// data-loss case is the same path written twice, and a prefix rule over prose
// read out of a brief would refuse honest divisions to catch a case nobody has
// seen. If real usage produces one, it is a change to make then.
//
// THE DIMNESS IS WHY THE CLAIM IS ONE SENTENCE AND NOT TWO. A part's brief was
// read as a claim until #281, and a brief is prose: it names the material the
// part works on, which includes everything the part must READ. Two parts told to
// read one plan and write a file each — the ordinary shape of divided work —
// were refused on the plan, and the road's own end-to-end suite worked around it
// by leaving the ground empty. The answer is not a grammar over the prose,
// which is the machinery this file already refused to build. It is to read the
// sentence that was always about the finished tree: the DONE-CONDITION.

import (
	"sort"
	"strings"
)

// scopeCollisions is every path more than one part of this division claims,
// spelled the way the parts spell it and in a stable order.
//
// WHAT COUNTS AS A CLAIM is a path named in the part's DONE-CONDITION, and
// nothing named anywhere else. The done-condition is the one sentence of a part
// that describes the tree AFTER the part is finished — what somebody else could
// check without taking the worker's word for it — so it is the sentence that
// says what this part produces. The brief is context and is read for nothing
// here: what the part works on, what its author already learned, the material
// every part reads. The schema and prompts/divide.md teach exactly that, so a
// worker naming a shared input in its brief is doing what it was asked to.
//
// THE OTHER ROAD IS COVERED BY CONSTRUCTION. A part the harness drew out of a
// sketch has no done-condition of its own, so task_divide_sketch.go's
// [divisionStandInDone] writes one out of that part's scope — which means the
// scope a drawing gave a part is still read here, and a sketch that put two
// parts on one file is refused exactly as it was.
//
// [pathTokens] does the reading and [groundHolds] does the judging, exactly as
// the ground ladder does it (taskstands.go): a word only counts when it really
// lands under the family tree, which is what keeps ordinary prose — a sentence
// that happens to end in a full stop, an interpreter named in passing — from
// reading as somebody's claim.
//
// THE PATHS ARE NORMALISED BEFORE THEY ARE COMPARED, because `report.md` and
// `/…/tree/report.md` in two done-conditions are one file and a comparison on
// the words would say they were two. [resolvePath] is the same reading a shell standing in
// that directory would make.
//
// It is one pass over the parts with a set, not a comparison of every part
// against every other: a division may carry a dozen parts and each
// done-condition a dozen paths, and the honest shape of "who else claimed this"
// is a lookup.
func scopeCollisions(parts []dividePart, tree string) []string {
	if strings.TrimSpace(tree) == "" || len(parts) < 2 {
		return nil
	}
	owner := make(map[string]int, 4*len(parts))
	said := make(map[string]bool, 4)
	var shared []string
	for index, part := range parts {
		for _, token := range pathTokens(part.Acceptance) {
			if !groundHolds(tree, token) {
				continue
			}
			path := resolvePath(tree, token)
			first, claimed := owner[path]
			if !claimed {
				owner[path] = index
				continue
			}
			// TWO SPELLINGS INSIDE ONE PART ARE NOT A COLLISION, and neither is
			// a third part arriving at a path already reported: a part is
			// allowed to name its own file twice, and a person reading the
			// refusal wants the path once.
			if first == index || said[path] {
				continue
			}
			said[path] = true
			shared = append(shared, token)
		}
	}
	sort.Strings(shared)
	return shared
}

// scopeRefusal is the whole of the ownership check at one call site: the refusal
// to hand the worker, or an empty string where the parts own separate work.
//
// IT IS ONE FUNCTION BECAUSE THE CHECK IS ASKED TWICE, on the two sets of parts
// that can exist ([Agent.divideOnce] says where each stands). Two spellings of
// "the parts may not share a path" would be two rules, and the day they
// disagreed the honest one would be whichever set of parts the reader in front
// of you was holding (design-law §ONE SOURCE OF TRUTH).
//
// WHAT DIFFERS BETWEEN THE TWO IS THE ENDING AND NOTHING ELSE, because what
// differs is what has already been spent by the time the refusal is written.
func (a *Agent) scopeRefusal(parts []dividePart, ending string) string {
	shared := scopeCollisions(parts, a.config.Workspace)
	if len(shared) == 0 {
		return ""
	}
	return divisionScopesOverlap(shared, ending)
}

// The two endings a scope refusal can have, and they are two because THE
// REFUSAL MUST NOT CLAIM A COST THAT WAS ALREADY PAID. A division refused
// before the reviewer is read cost nothing, and saying so is the whole of what
// makes a worker willing to redraw the boundary and come straight back. A
// division refused after it was read has cost that reading, and the same
// sentence there would be the harness telling a worker its money is still in
// its pocket.
//
// THE SECOND ONE SAYS NOTHING ABOUT SPEND RATHER THAN NAMING A FIGURE, which is
// the emptiness law: the worker cannot act on the number, a person reads the
// spend in the ledger where it is actually true, and a sentence that argued
// about it would be a paragraph in front of an answer already made.
const (
	scopeSpentNothing = "nothing is cancelled and nothing is spent."
	scopeSpentTheRead = "nothing is cancelled."
)

// divisionScopesOverlap is the answer to a division whose parts claim the same
// path, and it is [divisionNotAsWritten]'s ending with a sentence of its own:
// an ordinary tool result, nothing admitted, and a worker that can fix exactly
// what is wrong and ask again.
//
// IT NAMES THE PATH, because that is the whole of what the worker can act on.
// "the parts overlap" sends a model back to re-read four briefs looking for
// something it already thought it had got right; "report.md is claimed by more
// than one part" is one edit. And it says WHY sharing cannot work — one
// deliverable, one ledger — because a rule whose reason is invisible is a rule
// models write around.
//
// IT DOES NOT END "carry on with the work in your own hands" like the gates'
// refusals do. Those are findings that this work is not worth dividing; this is
// a finding that these PARTICULAR BOUNDARIES are wrong, and the division may
// well be right the moment they are redrawn.
func divisionScopesOverlap(shared []string, ending string) string {
	return "not split: " + scopeClaimedTwice(shared) +
		", and the parts of one division cannot share a file — everything they write goes into one deliverable, so whichever finished last would quietly replace the other's work. Give each part files of its own, say in each part's done-condition which ones it produces, and ask again; " + ending
}

// scopeClaimedTwice spells the colliding paths as the head of that sentence. The
// list is bounded by [divideRefusalBytes] for the reason the reviewer's own line
// is: what the worker does next is one edit, and a refusal that listed forty
// paths would be a page of reading in front of an answer already made.
func scopeClaimedTwice(shared []string) string {
	names := clip(strings.Join(shared, ", "), divideRefusalBytes)
	if len(shared) == 1 {
		return names + " is claimed by more than one part"
	}
	return names + " are each claimed by more than one part"
}

// ── the second rule: A FAMILY-WIDE CHECK BELONGS TO THE FAMILY ────────────────
//
// A CHECK EVERY PART IS ORDERED TO RUN IS THE FAMILY'S, NOT ANY PART'S. When one
// command stands in two or more sibling done-conditions, the division is not
// admitted. Each part is asked for the check that proves its own slice, and the
// whole run is the parent's to make once, after their work is home.
//
// IT IS THE SAME SENTENCE THE OWNERSHIP RULE ABOVE MAKES ABOUT PATHS, made about
// commands: what a part is finished against describes the part, and not the
// family it belongs to. The loss it answers is different and it is not silent —
// it is measured waste. A division hands its parts out to run AT THE SAME TIME,
// each in a worktree of its own, so a done-condition copied into three parts is
// three concurrent runs of that check plus the parent's own afterwards, and every
// one of the three judges a tree that does not yet hold its siblings' files. The
// measured shape was three parts each ordered to run an eight-minute suite: four
// runs where the work needed one, and only the last of them able to be right.
//
// AND IT CLASSIFIES BY REPETITION ACROSS SIBLINGS, NEVER BY A PROGRAM NAME AND
// NEVER BY A DURATION. There is no build system named anywhere below and no
// threshold in seconds, because either would be a list to keep up to date and
// neither is the fact that matters. Two genuinely targeted checks differ in what
// they NAME — a file each, a package each — and stay two checks; a family-wide
// check is THE SAME WORDS IN EVERY PART, and being the same words in every part
// is exactly what makes it nobody's. A slow check that only one part was told to
// run is that part's own business and is left alone.

// sharedCheckCommands is every command more than one part of this division is
// ordered to run, normalised and in a stable order.
//
// IT DOES ITS OWN READING RATHER THAN ASKING [declaredChecks], and that is a fact
// about where the sentences come from. That door harvests the two conventions
// PROSE HAS FOR MARKING a command — a backticked span and a `$ `-prefixed line —
// which is right for a document somebody wrote to be read. A done-condition is
// not that document: real ones are plain sentences, "rank_test.go passes; go test
// ./internal/tui3/... passes", with nothing marked at all, and a reading that
// waited to be told would see no commands here on the day the waste happens.
//
// SO THE DISCRIMINATOR IS WHETHER THE CLAUSE ORDERS WORK ([ordersWork]), and
// keeping ordinary prose out of it is the whole of why this rule is safe to
// have. OVER-REFUSING A LEGITIMATE DIVISION WOULD SHUT THE ROAD ON ITS OWN BEST
// CASE, which is the same dimness the ownership rule above is built out of.
//
// TWO SPELLINGS INSIDE ONE PART ARE NOT A REPEAT, exactly as [scopeCollisions]
// already has it: a part is allowed to say the same thing twice, and what this
// rule is about is one command standing in the done-conditions of two DIFFERENT
// siblings.
func sharedCheckCommands(parts []dividePart) []string {
	if len(parts) < 2 {
		return nil
	}
	owner := make(map[string]int, 2*len(parts))
	said := make(map[string]bool, 2)
	var shared []string
	for index, part := range parts {
		for _, command := range orderedChecks(part.Acceptance) {
			first, ordered := owner[command]
			if !ordered {
				owner[command] = index
				continue
			}
			if first == index || said[command] {
				continue
			}
			said[command] = true
			shared = append(shared, command)
		}
	}
	sort.Strings(shared)
	return shared
}

// orderedChecks is one part's done-condition read as the commands it orders,
// normalised by [normalizedCheckCommand] so that two spellings of one check are
// one command here.
//
// WHICH CLAUSES THOSE ARE IS [ordersWork]'s question, and the answer is stated
// there rather than here so that there is one of it.
//
// AND THE CLAUSE IS COMPARED AS IT STANDS, TRAILING WORDS AND ALL. That is a
// stated limit rather than an oversight: a done-condition copied into every part
// is copied WORD FOR WORD, which is the defect this is for, and two parts that
// spell one command with different prose behind it — "go test ./x/... passes"
// against "go test ./x/... is green" — are two clauses here and are admitted.
// The alternative is guessing where a command stops and English starts, and a
// guess that ran short would refuse `go test ./x -run A` against `go test ./x`,
// which are genuinely two checks and must stay two.
func orderedChecks(acceptance string) []string {
	var out []string
	for _, clause := range checkClauses(strings.ReplaceAll(acceptance, "`", "")) {
		if !ordersWork(clause) {
			continue
		}
		out = append(out, normalizedCheckCommand(clause))
	}
	return out
}

// ordersWork says whether one clause of a done-condition is an ORDER TO RUN
// SOMETHING rather than a condition somebody will read.
//
// A COMMAND NAMES SOMETHING THE MACHINE CAN FIND — a path, a package, a flag —
// AND A SENTENCE OF ORDINARY WORDS THAT MERELY OPENS WITH A PROGRAM'S NAME IS A
// SENTENCE. Both halves are needed, and the second half is the one that was
// missing: the first word on its own said that "make targets are documented" and
// "make sure the tests still pass" were commands, so two parts that both wrote
// either of those ordinary sentences had a working division refused — and were
// handed their own English back, quoted as though it were a shell line. That is
// a road broken to catch nothing.
//
// AND THE CLAUSE HAS TO BE WHOLE ([wholeClause]) before any of it is read, which
// is what keeps a clause the splitting cut in half out of the comparison.
//
// THE LIMIT THIS BUYS, SAID PLAINLY: a check spelled with no path and no flag —
// `make check`, `npm test` — is not read as a command here, so a division that
// repeats one of those is admitted and the repeat is missed. That is the
// deliberate trade, and it is the right way round. A missed repeat costs time
// somebody can see and count; a division refused over a sentence breaks a road
// that was working, and the defect this whole rule exists for named a package
// path in every copy of it.
func ordersWork(clause string) bool {
	if !wholeClause(clause) {
		return false
	}
	words := strings.Fields(clause)
	if len(words) < 2 || !onThePath(words[0]) {
		return false
	}
	for _, word := range words[1:] {
		if namesSomethingFindable(word) {
			return true
		}
	}
	return false
}

// namesSomethingFindable is the second half of that law asked of one word: a
// word holding a `/`, opening with a `-`, or carrying an `=` is a path, a flag or
// a setting, and every one of those is something the machine goes and finds. An
// ordinary English word is none of them.
func namesSomethingFindable(word string) bool {
	return strings.Contains(word, "/") || strings.HasPrefix(word, "-") ||
		strings.Contains(word, "=")
}

// wholeClause says whether a clause is one simple command that came through the
// splitting intact, and it exists because A CLAUSE SPLIT INSIDE A QUOTE IS NOT A
// CLAUSE.
//
// [checkClauses] cuts on a semicolon, a newline and a full stop, and it does none
// of that knowing about quotation — so `sh -c 'test -f a; echo A'` and
// `sh -c 'test -f a; echo B'` both leave behind the fragment `sh -c 'test -f a`,
// which is one command in neither part and identical in both. Two parts running
// genuinely different checks were refused for sharing a piece of text that
// nobody wrote. AN UNBALANCED COUNT OF EITHER QUOTE IS THAT CUT, and a clause
// carrying one is dropped rather than compared.
//
// AND A CLAUSE HOLDING ANY OF [shellComposition] IS NOT ONE SIMPLE COMMAND, which
// is the same reading [commandLike] already makes of a span before it will call
// it a check. The constant is reused rather than respelled for the reason it was
// made a constant in the first place (task_audit.go): one question with two
// answers is two questions.
func wholeClause(clause string) bool {
	if strings.ContainsAny(clause, shellComposition) {
		return false
	}
	return strings.Count(clause, "'")%2 == 0 && strings.Count(clause, `"`)%2 == 0
}

// checkClauses cuts a done-condition into the separate things it asks for. A
// done-condition is written as a list — semicolons, newlines, sentences — and
// each item of that list is either an order to run something or a condition to
// read, so the clause is the unit this rule compares.
func checkClauses(text string) []string {
	var clauses []string
	start := 0
	for index := 0; index < len(text); index++ {
		if !endsAClause(text, index) {
			continue
		}
		clauses = append(clauses, text[start:index])
		start = index + 1
	}
	return append(clauses, text[start:])
}

// endsAClause says whether the byte at index closes one item of that list.
func endsAClause(text string, index int) bool {
	switch text[index] {
	case ';', '\n':
		return true
	case '.':
		// A FULL STOP CLOSES A CLAUSE ONLY WITH A SPACE AFTER IT AND NO FULL STOP
		// BEFORE IT. Without the second half, `go test ./internal/tui3/... passes`
		// — the commonest spelling of a package tree there is — would read as a
		// sentence that ended two characters early, and the command this rule went
		// on to name in its refusal would be one the worker never wrote.
		return index+1 < len(text) && text[index+1] == ' ' &&
			(index == 0 || text[index-1] != '.')
	}
	return false
}

// sharedCheckRefusal is the whole of the family-wide-check rule at one call site:
// the commands more than one part was ordered to run and the refusal to hand the
// worker, or nothing at all where every part proves its own slice.
//
// IT ANSWERS THE COMMANDS AS WELL AS THE SENTENCE because the record wants them
// (sessionfile.go's [journalDivision.Shared]): an autopsy asking which check was
// family-wide should read it off the line rather than count shell calls across
// four worktrees.
//
// IT IS ONE FUNCTION FOR [Agent.scopeRefusal]'s reason: the rule is asked twice,
// on the two sets of parts that can exist, and two spellings of it would be two
// rules the day they disagreed.
func sharedCheckRefusal(parts []dividePart, ending string) ([]string, string) {
	shared := sharedCheckCommands(parts)
	if len(shared) == 0 {
		return nil, ""
	}
	return shared, divisionRepeatsOneCheck(shared, ending)
}

// divisionRepeatsOneCheck is the answer to a division whose parts were each
// ordered to run one check, and it is shaped exactly like
// [divisionScopesOverlap]: an ordinary tool result, nothing admitted, and a
// worker that can fix the one thing that is wrong and ask again.
//
// IT NAMES THE COMMAND, because that is the whole of what the worker can act on.
// "the parts repeat a check" sends a model back through four done-conditions
// looking for something it thought it had got right; the command itself is one
// edit in each of them. And it says WHY, because a rule whose reason is invisible
// is a rule models write around.
//
// AND IT SPELLS OUT THE ROAD, which for this refusal is three moves and not one.
// The worker gives each part the check that proves ITS OWN SLICE, KEEPS the
// whole run for itself in its own done-condition, and ASKS AGAIN — so the
// family-wide run is moved rather than dropped, and the answer that comes back
// is a division rather than one undivided worker. A refusal that only said no
// would leave a model free to pick either of the two wrong roads: fold the work
// back into one pair of hands, or delete the check so nobody makes it at all.
//
// IT IS NOT A FINDING THAT THE WORK CANNOT BE DIVIDED, and it says so, exactly as
// [divisionScopesOverlap] does about boundaries. The floor gates make that
// finding and it is a different one; this is a finding that these PARTICULAR
// DONE-CONDITIONS are wrong, and the division may well be right the moment they
// are rewritten.
func divisionRepeatsOneCheck(shared []string, ending string) string {
	return "not split: " + checkOrderedByTwoParts(shared) +
		", and a check every part was told to run belongs to the whole family rather than to any one part — the parts work side by side in trees of their own, so each of those runs judges a tree that does not hold its siblings' files yet. The division itself is not the problem: give each part the check that proves its own slice, over the files that part produces and no others; keep the whole run for yourself, in your own done-condition, and make it once after every part's work is home; then ask again; " + ending
}

// checkOrderedByTwoParts spells the repeated commands as the head of that
// sentence, clipped by [divideRefusalBytes] for [scopeClaimedTwice]'s reason:
// what the worker does next is one edit, and a refusal that listed forty
// commands would be a page of reading in front of an answer already made.
func checkOrderedByTwoParts(shared []string) string {
	names := clip(strings.Join(shared, ", "), divideRefusalBytes)
	if len(shared) == 1 {
		return names + " stands in more than one part's done-condition"
	}
	return names + " each stand in more than one part's done-condition"
}
