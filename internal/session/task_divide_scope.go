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

import (
	"sort"
	"strings"
)

// scopeCollisions is every path more than one part of this division claims,
// spelled the way the briefs spell it and in a stable order.
//
// WHAT COUNTS AS A CLAIM is a path named in the part's brief or its
// done-condition, which are the two sentences that say what the part is for.
// [pathTokens] does the reading and [groundHolds] does the judging, exactly as
// the ground ladder does it (taskstands.go): a word only counts when it really
// lands under the family tree, which is what keeps ordinary prose — a sentence
// that happens to end in a full stop, an interpreter named in passing — from
// reading as somebody's scope.
//
// THE PATHS ARE NORMALISED BEFORE THEY ARE COMPARED, because `report.md` and
// `/…/tree/report.md` in two briefs are one file and a comparison on the words
// would say they were two. [resolvePath] is the same reading a shell standing in
// that directory would make.
//
// It is one pass over the parts with a set, not a comparison of every part
// against every other: a division may carry a dozen parts and each brief a
// dozen paths, and the honest shape of "who else claimed this" is a lookup.
func scopeCollisions(parts []dividePart, tree string) []string {
	if strings.TrimSpace(tree) == "" || len(parts) < 2 {
		return nil
	}
	owner := make(map[string]int, 4*len(parts))
	said := make(map[string]bool, 4)
	var shared []string
	for index, part := range parts {
		for _, token := range pathTokens(partScope(part)) {
			if !groundHolds(tree, token) {
				continue
			}
			path := resolvePath(tree, token)
			first, claimed := owner[path]
			if !claimed {
				owner[path] = index
				continue
			}
			// TWO SPELLINGS INSIDE ONE BRIEF ARE NOT A COLLISION, and neither is
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

// partScope is the text in which this part says what it owns, and it is NOT the
// whole of the brief.
//
// A PART'S BRIEF CARRIES FAMILY CONTEXT THAT IS NOT A CLAIM. Where the harness
// composed the brief (task_divide_sketch.go's [sketchBrief]) it wrote the
// PARENT'S own brief above the part's scope — the same paragraphs in every
// sibling — and its siblings' scopes below it, so that a part knows what not to
// touch. Reading either as this part's claim would make every division of that
// shape collide with itself on the first path the parent ever mentioned, which
// is the road refusing its own best case.
//
// So the boundary the harness wrote is the boundary this reads to. The markers
// are the ones it wrote and there is one source of them; a brief a worker wrote
// itself carries neither, and is its own scope whole.
func partScope(part dividePart) string {
	own := part.Brief
	if at := strings.Index(own, divisionThisPart); at >= 0 {
		own = own[at+len(divisionThisPart):]
	}
	if at := strings.Index(own, divisionOtherParts); at >= 0 {
		own = own[:at]
	}
	return own + "\n" + part.Acceptance
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
		", and the parts of one division cannot share a file — everything they write goes into one deliverable, so whichever finished last would quietly replace the other's work. Give each part files of its own, say in its brief which ones it owns, and ask again; " + ending
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
