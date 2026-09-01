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
//   - THE SCOPE — what this one part owns, the material it works on, what its
//     done-condition rests on — which only the worker holding the material can
//     write, and which is different for every part.
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
	divisionThisPart    = "WHAT THIS PART OWNS"
	divisionOtherParts  = "THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS RIGHT NOW: "
	divisionStayInScope = ". Do none of them, and do not change what they own — make your own part whole and say in your report what you did."
)

// divisionScopeLimit bounds ONE part's line of the sibling map. It is short on
// purpose: the map answers "what is somebody else's", which a title and a line
// of summary answer, and every part carries every other part's line — so a
// summary somebody wrote three paragraphs into would be paid for once per part
// and would bury the boundary it exists to draw.
const divisionScopeLimit = 240

// divisionFamily is the context of one division, composed ONCE for all of its
// parts: the work being divided, and how each part is named to the others.
//
// IT IS A VALUE COMPUTED BEFORE THE FIRST PART IS BUILT because it is the same
// for all of them. The parent's brief is fitted to its bound once here rather
// than once per part — a division into five parts fitting the same document five
// times is four passes over a page nobody changed.
type divisionFamily struct {
	// ground is the work being divided, already fitted to the room a part's
	// brief leaves for it, or empty where there is nothing to carry.
	ground string
	// scopes is how each part is named to its siblings, in the order the parts
	// were settled in.
	scopes []string
}

// familyOf composes the context every part of one division is given.
//
// THE GROUND IS THE PARENT'S OWN BRIEF and not its assembled one, which is the
// difference [TaskNode.ownBrief] states: the assembled brief carries the standing
// orders over this place and the reports of whatever ran before it, and every
// part is started by the same frontier that appends both — so a part composed on
// the assembled one would read the house rules twice.
//
// AND A BRIEF THAT IS THE PERSON'S OWN SENTENCE IS NOT CARRIED AT ALL. Where
// somebody wrote the work themselves the brief and the request are one paragraph
// ([composeBrief] prints it once for the same reason), and a part told it under
// two headings would be reading two instructions that happen to agree.
func familyOf(request, brief string, parts []dividePart) divisionFamily {
	scopes := make([]string, len(parts))
	for index, part := range parts {
		scopes[index] = siblingScope(part)
	}
	family := divisionFamily{scopes: scopes}
	// THE ROOM THE BOUNDARY NEEDS IS TAKEN OUT OF THE BOUND BEFORE THE GROUND IS
	// FITTED, and it is taken against ALL of the scopes — which is more than any
	// one part's sentence names and is therefore the same arithmetic for every
	// part of the division. A ground clipped to fit part one and not part four
	// would be one division whose parts had read different documents.
	room := taskShapeBriefLimit - len(divisionOtherParts+strings.Join(scopes, "; ")+divisionStayInScope) - len("\n\n")
	brief = strings.TrimSpace(brief)
	if brief == strings.TrimSpace(request) {
		return family
	}
	if brief != "" && room > 0 {
		family.ground = clip(brief, room)
	}
	return family
}

// partBrief is one part's whole world: the family's context, the boundary, and
// then the scope whoever divided the work wrote for this part.
//
// THE SCOPE COMES LAST, under a heading of its own. What a worker reads first is
// why this work exists and what it may not touch; what it reads last, and acts
// on, is its own job — and the heading is what keeps the two from reading as one
// paragraph of instruction.
//
// IT IS HELD TO THE SAME BOUND EVERY BRIEF ON THIS ROAD IS HELD TO, once per
// half. The two halves have different authors and neither may eat the other: a
// context clipped to make room for a rambling scope would lose the findings the
// parts were cut out of, and a scope clipped to make room for the context would
// lose the only sentence saying what this part is for.
func (f divisionFamily) partBrief(index int, scope string) string {
	sections := make([]string, 0, 3)
	if f.ground != "" {
		sections = append(sections, f.ground)
	}
	if siblings := f.siblings(index); siblings != "" {
		sections = append(sections, siblings)
	}
	if scope = clip(strings.TrimSpace(scope), taskShapeBriefLimit); scope != "" {
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

// siblingScope is how one part is named to its siblings: its title, and the line
// its author wrote about what it does.
//
// IT IS NOT [partScope] (task_divide_scope.go), which reads the material a part
// claims so that two parts claiming the same thing are refused. This one is
// prose for a worker to read; that one is a set of paths for the harness to
// compare, and one function answering both questions would have to be wrong
// about one of them.
//
// THE TITLE IS DROPPED WHERE THE SUMMARY ALREADY OPENS ON IT. A title is
// frequently the summary cut to [TaskNameWords] — that is exactly what the sketch
// road mints (task_divide_sketch.go's [sketchName]) — and "the http client major:
// the http client major version" is one name said twice with a colon in it.
func siblingScope(part dividePart) string {
	title := strings.TrimSpace(part.Title)
	summary := clip(strings.TrimSpace(part.Summary), divisionScopeLimit)
	switch {
	case summary == "":
		return title
	case title == "" || strings.HasPrefix(strings.ToLower(summary), strings.ToLower(title)):
		return summary
	}
	return title + ": " + summary
}
