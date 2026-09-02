package session

// WHAT A PART IS TOLD, AND WHICH HALF OF IT A MODEL WROTE.
//
// A part is a worker that never sees the conversation its work came out of and
// cannot ask anybody anything. Its opening message is the whole of its world, and
// that world has always been assembled from two halves that are NOT the same kind
// of thing:
//
//   - THE FAMILY'S CONTEXT — the work being divided, and the map of who owns what
//     — which is identical for every part and is a FACT THE HARNESS ALREADY HOLDS.
//   - THE SCOPE — what this one part works on, the material it opens, what its
//     done-condition rests on — which only the worker holding the material can
//     write, and which is different for every part. What the part OWNS is its
//     done-condition and travels in the contract's own DONE WHEN section, which
//     is why this heading says WORKS ON (#281).
//
// THE TWO HALVES USED TO BE DECIDED BY THE ROAD RATHER THAN BY THE HALF. A part
// the harness drew out of a sketch got the parent's brief and its siblings
// composed around it (task_divide_sketch.go); a part a worker wrote with
// `divide_work` got the worker's prose and NOTHING ELSE, and the parent's brief
// reached it only if the cheapest model on the road remembered to restate it N
// times. Which means the weakest world was handed to the part on the road a cheap
// crew actually drives, and nothing downstream could see that it had happened —
// the reviewer may sharpen a brief, but it fails open, so a division admitted
// without a reading was admitted without context too.
//
// SO THE CONTEXT IS COMPOSED, ONCE, BY THIS FILE, AND BOTH ROADS COME THROUGH IT
// ([Agent.divideOnce] is the single call site, and the sketch road reaches it by
// putting its drawing to that same door). A worker writes a scope; the harness
// writes everything around it. Nothing is left to a model remembering to repeat
// itself, which is the same law the person's own words are carried under
// (task_brief.go): what the code already holds is never asked of a model.
//
// AND THE PERSON'S ASK IS NOT HERE, deliberately. It rides on every node's spec
// and [composeBrief] prints it above the work under the heading that says whose
// words they are, so a part gets it exactly once — putting it in the context
// block as well would print the same paragraph twice under two headings, which
// reads as two instructions that happen to agree.

import "strings"

// These three are the BOUNDARY, and it is stated in both directions because a
// part that does not know its siblings exist is a part that does all four jobs
// and collides with three workers doing the same.
//
// IT IS STATED BY THE HARNESS FOR EVERY PART, whoever wrote the parts. A worker
// writing its own division knows what it gave away and the schema asks it to say
// so — but knowing is not saying, and a part whose author forgot is a part that
// finds out by overwriting somebody's work. The settled parts list is what
// actually got handed out, so it is the honest answer to "who else is holding
// what", and it is the same answer on both roads.
const (
	divisionThisPart    = "WHAT THIS PART WORKS ON"
	divisionOtherParts  = "THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS RIGHT NOW: "
	divisionStayInScope = ". Do none of them, and do not change what they own — make your own part whole and say in your report what you did."
)

// divisionScopeLimit bounds ONE part's line of the sibling map. It is short
// because every part carries every other part's line: a scope somebody wrote
// three paragraphs of would be paid for once per part and would bury the boundary
// it exists to draw. It is not shorter than this because what a map is FOR is the
// files and the material a sibling has claimed, and a title alone names none of
// them.
const divisionScopeLimit = 320

// divisionFamily is the context of one division, composed ONCE for all of its
// parts: the work being divided, how each part is named to the others, and the
// room a part's own scope has inside the bound.
//
// IT IS A VALUE COMPUTED BEFORE THE FIRST PART IS BUILT because it is the same
// for all of them. The parent's brief is fitted to its room once here rather than
// once per part — a division into five parts fitting the same document five times
// is four passes over a page nobody changed.
type divisionFamily struct {
	// ground is the work being divided, already fitted to the room a part's
	// brief leaves for it, or empty where there is nothing to carry.
	ground string
	// scopes is how each part is named to its siblings, in the order the parts
	// were settled in.
	scopes []string
	// room is what ONE part's own scope may take, and it is the same figure for
	// every part of the division. See [familyOf] for the arithmetic it comes out
	// of and what it guarantees.
	room int
}

// familyOf composes the context every part of one division is given.
//
// THE GROUND IS WHAT A PART INHERITS ([TaskGraph.inheritedLocked]): the brief the
// work was admitted with AND the reports of whatever ran before it, which is
// where a family's findings actually are. It stops short of the standing orders,
// which the frontier appends to every part in its own right — a part composed on
// the whole assembled brief would read the house rules twice.
//
// AND THE PERSON'S OWN WORDS COME OFF THE FRONT OF IT. Where somebody wrote the
// work themselves the brief opens on their sentence verbatim, and [composeBrief]
// is already printing that sentence above all of this under the heading that says
// whose it is. Printing it twice under two headings reads as two instructions
// that happen to agree.
//
// ── ONE BOUND, AND WHAT IT GUARANTEES ──
//
// A part's whole brief fits in [taskShapeBriefLimit], which is the bound every
// brief on this road is held to. The three sections are fitted in the order they
// may not be lost in, and the arithmetic is done ONCE, here, against the worst
// case any part of this division can present:
//
//  1. THE BOUNDARY IS NEVER CUT. Its widest form names every scope — one more
//     than any single part is given — and that width is taken off the top.
//  2. THE SCOPE IS NEVER CUT FOR THE GROUND. What is left after the boundary is
//     the scope's room, and the LONGEST scope in the division is reserved out of
//     it, so the fitting is the same for the first part and the fifth.
//  3. THE GROUND TAKES WHAT REMAINS. A part that lost the last page of what the
//     work already found out is worse off; a part that lost the sentence saying
//     what it owns, or the one saying what its siblings own, is dangerous.
//
// The ground is [TaskGraph.inheritedLocked]'s answer, which has already fitted
// the prerequisite reports and marked any cut with [clip]. This second fit
// must not stack a second mark on a cut that already carried one — [clip]
// moves a trailing mark rather than doubling it, so a report is marked once
// or not at all.
func familyOf(request, brief string, parts []dividePart) divisionFamily {
	scopes := make([]string, len(parts))
	for index, part := range parts {
		scopes[index] = siblingScope(part)
	}
	family := divisionFamily{scopes: scopes}
	family.room = taskShapeBriefLimit -
		len(divisionOtherParts+strings.Join(scopes, "; ")+divisionStayInScope) -
		len("\n\n"+"\n\n"+divisionThisPart+"\n")
	longest := 0
	for _, part := range parts {
		if size := len(strings.TrimSpace(part.Brief)); size > longest {
			longest = size
		}
	}
	if longest > family.room {
		longest = family.room
	}
	family.ground = fit(personsWordsOff(request, brief), family.room-longest)
	return family
}

// personsWordsOff is the ground with the person's own sentence taken off the
// front of it, where the brief opens on it. See [familyOf]: their words are
// printed once, by [composeBrief], and this is the one place they could come to
// be printed a second time.
func personsWordsOff(request, brief string) string {
	brief = strings.TrimSpace(brief)
	if request = strings.TrimSpace(request); request == "" {
		return brief
	}
	return strings.TrimSpace(strings.TrimPrefix(brief, request))
}

// partBrief is one part's whole world: the family's context, the boundary, and
// then the scope whoever divided the work wrote for this part.
//
// THE SCOPE COMES LAST, under a heading of its own. What a worker reads first is
// why this work exists and what it may not touch; what it reads last, and acts
// on, is its own job — and the heading is what keeps the two from reading as one
// paragraph of instruction.
func (f divisionFamily) partBrief(index int, scope string) string {
	sections := make([]string, 0, 3)
	if f.ground != "" {
		sections = append(sections, f.ground)
	}
	if siblings := f.siblings(index); siblings != "" {
		sections = append(sections, siblings)
	}
	if scope = fit(strings.TrimSpace(scope), f.room); scope != "" {
		// A SCOPE STANDING ALONE WEARS NO HEADING, by the emptiness law: where
		// there is no context and no sibling to tell it apart from, the heading
		// would be a section marker over the whole of a one-section document.
		if len(sections) > 0 {
			scope = divisionThisPart + "\n" + scope
		}
		sections = append(sections, scope)
	}
	return strings.Join(sections, "\n\n")
}

// siblings is the sentence naming what somebody else owns right now, or an empty
// string where this part has no siblings to name — which is the one-part
// division no road admits, and is answered here rather than assumed.
func (f divisionFamily) siblings(index int) string {
	others := make([]string, 0, len(f.scopes))
	for other, scope := range f.scopes {
		if other != index && scope != "" {
			others = append(others, scope)
		}
	}
	if len(others) == 0 {
		return ""
	}
	return divisionOtherParts + strings.Join(others, "; ") + divisionStayInScope
}

// siblingScope is how one part is named to its siblings: its title, and the
// opening of the scope its author wrote for it.
//
// IT IS CUT FROM THE SCOPE AND NOT FROM THE SUMMARY, which is the difference
// between a map somebody can stay off and a label. The summary says what a part
// does; the scope is where the files, the paths and the material it has claimed
// are written, and a sibling that is told the words but not the material is a
// sibling that finds the boundary by writing over it. The summary stands in only
// where there is no scope at all.
//
// THE TITLE IS DROPPED WHERE THE SCOPE ALREADY SAYS IT. A title is frequently the
// scope's own words cut to [TaskNameWords] — that is exactly what the sketch road
// mints (task_divide_sketch.go's [sketchName]) — and "the http client major: the
// http client major version" is one name said twice with a colon in it.
//
// AND THE WHOLE LABEL IS BOUNDED, title included. Every field here was written by
// a model, so any of them can arrive at any length; a bound on one of the two
// halves is not a bound.
func siblingScope(part dividePart) string {
	title := strings.TrimSpace(part.Title)
	scope := strings.TrimSpace(part.Brief)
	if scope == "" {
		scope = strings.TrimSpace(part.Summary)
	}
	switch {
	case scope == "":
		return clip(title, divisionScopeLimit)
	case title == "" || strings.Contains(strings.ToLower(scope), strings.ToLower(title)):
		return clip(scope, divisionScopeLimit)
	}
	return clip(title+": "+scope, divisionScopeLimit)
}

// fit is [clip] with the one answer clip has no room to give: a section whose
// room will not hold even the mark of a cut is LEFT OUT, rather than printed as a
// stub of nothing. Every figure this file bounds against is arithmetic on strings
// a model wrote, so any of them can come out at or below zero.
func fit(text string, room int) string {
	if room <= len("…") {
		return ""
	}
	return clip(text, room)
}

// inheritedBrief is the work being divided, as a part of it inherits it: what
// this node was admitted with and what the work before it learned, read under the
// graph's lock ([TaskGraph.inheritedLocked] says why the standing orders are not
// in it).
//
// IT IS NOT [TaskNode.assembledBrief], and the difference is the one section a
// part would otherwise read twice. It is not the spec's brief alone either: the
// reports of the work that ran before this node are where a family's findings
// are, and a part cut off from them is a part that finds them out again.
func (n *TaskNode) inheritedBrief() string {
	if n == nil {
		return ""
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.graph.inheritedLocked(n)
}
