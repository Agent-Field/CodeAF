package session

// The BIRTH SEAM: what the person's standing orders put into the world of work
// that is only just starting. docs/STANDING-ORDERS.md's D10 names three
// enforcement seams — birth, landing, tick — and this file is the first of
// them, on both of the places work is born: a task node's brief, assembled just
// in time as the node starts, and a conversation's own instructions, rendered
// at the start of every turn.
//
// IT IS ONE RESOLVER CALL AND ONE SECTION, DELIBERATELY. Which orders govern a
// place is [standing.Store.Applicable]'s question and nobody else's (D5), so
// nothing here reads an altitude, an exception or a status — a second reading
// of reach would be a second answer to the one question every seam in this wave
// asks, and a task, a conversation and the /standing page would drift apart.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// standingWorldHeading titles the section on BOTH sides of this seam. It is ONE
// CONSTANT because it is one thing said twice: a heading spelled in two files is
// a heading that will one day be spelled two ways, and then the model has two
// names for the same orders and the person has none.
const standingWorldHeading = "Standing orders"

// standingWorldIntro is the sentence under the heading, and it is the whole of
// what a model has to understand about the lines below it: WHOSE they are, that
// they did not arrive with this piece of work, and that they are not advice.
const standingWorldIntro = "These are the person's own conditions over this place. They were set before this work and they hold until the person says otherwise — they are not suggestions. Work within them."

// standingWorldReport is the closing line a TASK's section carries and a
// conversation's does not. A node works with nobody to ask, so an order it
// cannot honour has exactly one place to be said (task_contract.go); a
// conversation can say so in the very next sentence it writes.
const standingWorldReport = "If you cannot honour one of these, say so in your report."

// standingWorldMost is how many orders one section may list. A world section is
// context somebody pays for on every request of every turn, and a person with
// forty orders over a project has a working agreement, not a preamble.
const standingWorldMost = 8

// renderStandingWorld is the section itself: the heading, the sentence that says
// whose these are, one order per line, and the caller's closing line.
//
// THE EMPTINESS LAW. No orders is NO SECTION — not an empty heading, not "none"
// — because a brief that says nothing stands over it has spent tokens saying
// there is nothing to say.
//
// LONGEST-STANDING FIRST, AND THAT IS THIS SECTION'S ORDER AND NOT THE PAGE'S.
// The /standing page leads with what the person touched most recently, because
// a page is about what they are thinking about now. A world section is about
// what the work must obey, and there the settled conditions lead: an order that
// has stood for months is the house rule, and the one made this morning is
// already in the conversation above. It is also what makes the clip honest —
// when there are more than [standingWorldMost], the ones that survive are the
// ones that have been true the longest.
//
// ONE ORDER IS ONE LINE. A compiled prompt may be written across several lines,
// and a list whose rows are paragraphs is a list nothing can count, so the
// whitespace inside an order is folded before it is written.
func renderStandingWorld(items []standing.Item, closing string) string {
	kept := append([]standing.Item(nil), items...)
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].Created.Before(kept[b].Created) })
	lines := make([]string, 0, len(kept))
	for _, item := range kept {
		if folded := strings.Join(strings.Fields(item.Prompt()), " "); folded != "" {
			lines = append(lines, folded)
		}
	}
	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString(standingWorldHeading + ":\n\n" + standingWorldIntro + "\n\n")
	over := 0
	if len(lines) > standingWorldMost {
		over = len(lines) - standingWorldMost
		lines = lines[:standingWorldMost]
	}
	for _, line := range lines {
		out.WriteString("- " + line + "\n")
	}
	if over > 0 {
		out.WriteString("…" + strconv.Itoa(over) + " more\n")
	}
	if closing != "" {
		out.WriteString("\n" + closing + "\n")
	}
	return out.String()
}

// ── a task node, at the moment it starts ────────────────────────────────────

// standingWorld is the section a starting node's brief ends with, or "" when
// nothing stands over this work. Nil home, no ambient side and an unreadable
// store all answer the same way — a capability that cannot work is absent, and
// a node is not where a person learns their disk is gone.
//
// THE PLACE IS THE WORKSPACE AND THE OWNING CONVERSATION'S SESSION ID, which is
// the same pair the conversation itself resolves ([Agent.standingPlace]). A node
// has no conversation of its own — it is a worktree with a brief — so the
// question "which conversation admitted this work?" has exactly one answer, and
// it is [TaskGraph.home], the conversation whose graph this is and whose id
// every node's journal is filed under ([Agent.sessionID]). Passing an empty
// session id instead would be correct about the worktree and WRONG about the
// work: an order the person set for this conversation alone must reach the task
// that conversation just handed out, or "just here, in this chat" stops meaning
// anything the moment the chat does the work rather than talking about it.
//
// IT IS ASKED ONCE PER FRONTIER PASS AND NOT ONCE PER NODE, because every node
// in one graph sits in one place: ten nodes starting together would otherwise
// be ten readings of one folder for ten identical answers.
func (g *TaskGraph) standingWorld() string {
	if g.home == nil {
		return ""
	}
	store := g.home.standingOrders()
	if store == nil {
		return ""
	}
	items, err := store.Applicable(g.home.standingPlace())
	if err != nil {
		return ""
	}
	return renderStandingWorld(items, standingWorldReport)
}

// ── a conversation, at the start of every turn ──────────────────────────────

// refreshStandingLocked puts the orders that govern this conversation in front
// of the model. It is called with a.mu held, at the start of every turn, beside
// the clock's own refresh ([Agent.refreshClockLocked]) and for that line's
// reason: the world a turn reasons with must be this turn's, and an order stood
// up an hour ago is not something the model may still be blind to.
//
// THE TRIGGER IS THE TURN AND NOT AN EVENT, and the cost is what makes that
// affordable: [standing.Store.Applicable] is a directory read and a file per
// order, next to a provider call that is about to cross the network. A cache
// invalidated by the update events would be a second account of what stands
// here, kept in step by hand.
//
// AND THE COMMON ANSWER IS BYTE-IDENTICAL. A conversation whose orders have not
// moved renders the same block, so message[0] is the string the provider
// already cached (taskdelta.go makes the same argument for its own block).
//
// A TASK NODE RENDERS NOTHING HERE, and not by a check: a node's config carries
// no Standing door at all (task_run.go's child config), so [Agent.standingOrders]
// answers nil. Its orders arrive in its brief, once, where the rest of its world
// arrives — a node told the same thing twice would be a node weighing it twice.
func (a *Agent) refreshStandingLocked() {
	a.standingText = a.standingBlockLocked()
}

// standingBlockLocked is the <standing> block message[0] carries, or "" when
// nothing stands over this conversation. It is tagged the way <memory>, <state>
// and <elsewhere> are tagged and joined on the same way, because a model that
// has learned to read fenced blocks in its instructions should not have to learn
// a fourth grammar for the fourth.
func (a *Agent) standingBlockLocked() string {
	store := a.standingOrders()
	if store == nil {
		return ""
	}
	items, err := store.Applicable(a.standingPlaceLocked())
	if err != nil {
		return ""
	}
	section := renderStandingWorld(items, "")
	if section == "" {
		return ""
	}
	return "\n<standing>\n" + section + "</standing>\n"
}
