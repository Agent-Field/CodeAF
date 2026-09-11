package session

// Governing directions are resolved at each execution turn, through the same
// read-only owner capability in chats, workers, checkers and scheduled runs.
// The standing owner decides applicability; organization supplies only explicit
// placement depths. A persisted task brief must not freeze a second authoritative
// copy that can survive a later correction or exception.

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// standingWorldHeading titles the section on BOTH sides of this seam. It is ONE
// CONSTANT because it is one thing said twice: a heading spelled in two files is
// a heading that will one day be spelled two ways, and then the model has two
// names for the same orders and the person has none.
const standingWorldHeading = "Standing orders"

// ── THE TWO REGISTERS, AND WHY AN ORDER'S KIND PICKS ONE ────────────────────
//
// ONE INTRO OVER EVERY KIND WAS A SENTENCE THAT WAS TRUE OF ONE OF THEM. A HOLD
// is the only kind that never wakes: "always use tabs here" has no moment, no
// rhythm and no probe, and its whole work is done at birth — here, riding into
// the world of the work that starts ([standing.WhenHold] says exactly that). It
// IS a condition over this place, and the binding sentence below is the honest
// one for it. The other five kinds are APPOINTMENTS: a moment, a rhythm, a file
// that changed, a quiet machine, a look at the world. Each of them is answered
// by the pass on its own clock and none of them is a rule anybody is working
// under, so "remind me at 6 to check the deploy" arriving in a worker's brief as
// a condition it must work within made every task in the project more cautious
// for a reason the person never asked for.
//
// SO THE KIND DECIDES THE SENTENCE AND NOTHING ELSE DOES. No status, no
// altitude, no second reading of reach — which of these orders govern this place
// is [standing.Store.Applicable]'s one question (this file's header) and asking
// any part of it again here would be the drift that header exists to prevent.
// What is read is ONE FIELD against ONE NAME, and everything that is not a hold
// takes the softer register: an order whose kind this build cannot read is not a
// house rule.

// standingWorldHolding is the sentence a HOLD rides under, and it is the whole
// of what a model has to understand about the lines below it: WHOSE they are,
// that they did not arrive with this piece of work, and that they are not
// advice.
const standingWorldHolding = "These are the person's own conditions over this place. They were set before this work and they hold until the person says otherwise — they are not suggestions. Work within every compatible condition. Never use recency or folder order to silently discard a condition. An explicit exception changes only its identified scope or clause. If conditions materially conflict, ask one focused question when the person is present; otherwise report that the affected work needs the person. Continue unaffected work within the compatible conditions."

// standingWorldWaiting is the sentence every other kind rides under. It says
// the same two things about whose they are and how long they last, and then it
// says the one thing the binding sentence must not say about an appointment:
// that it is not asking this worker for anything. A reminder is worth knowing
// about — it is what the person has going on around this work — and it is not a
// condition on the work.
const standingWorldWaiting = "These are the person's own standing orders over this place, each waiting on a moment, a rhythm or a change of its own. They are here so you know what stands; none of them is a condition over this work and none asks anything of you now."

// standingWorldReport is the closing line a TASK's section carries and a
// conversation's does not. A node works with nobody to ask, so an order it
// cannot honour has exactly one place to be said (task_contract.go); a
// conversation can say so in the very next sentence it writes.
const standingWorldReport = "If you cannot honour one of these, say so in your report."

// standingWorldMost is how many orders one section may list. A world section is
// context somebody pays for on every request of every turn, and a person with
// forty orders over a project has a working agreement, not a preamble.
const standingWorldMost = 8

// Governing input has a hard admission bound, not a truncation rule. A turn
// whose complete conditions cannot fit must stop before any action is started.
const governingHoldLimit = 64
const governingPromptBytes = 64 * 1024

// standingWorldPlaced opens the section for work that is placed in folders: it
// names them, and says what a folder beside a condition means. It exists because
// a run told only "the person's own conditions over this place" re-judged, from
// a rule's wording, whether the rule was about its work — validator S11 on
// 2026-09-10 dropped the Travel folder's rule with "Travel one … not
// applicable", from work placed in Travel. Which rules apply is the owner's
// decision ([standing.Store.ApplicableScope]), already made; the run is told the
// placements so it does not make that decision a second time.
const standingWorldPlaced = "This work is placed in %s. A condition marked with a folder reaches this work through that placement and applies to all of it, whatever the work is about: never decide from its wording whether it applies."

// renderStandingWorld is the section itself: the heading, the folders the work
// is placed in when it is placed in any, the orders that hold under the
// sentence that binds — each marked with the folder it reaches the work
// through — the orders that are waiting under the sentence that does not, and
// the caller's closing line.
//
// THE EMPTINESS LAW. No orders is NO SECTION — not an empty heading, not "none"
// — because a brief that says nothing stands over it has spent tokens saying
// there is nothing to say. A tier with nothing in it is the same law one level
// down: no rows, no sentence introducing them.
//
// THE HOLDS LEAD, AND THAT IS THE ONE THING THE TIERS REORDER. A worker reads
// what it must obey before it reads what the person has going on, because the
// first is the only half of this section that can change what it does.
//
// LONGEST-STANDING FIRST WITHIN EACH TIER, AND THAT IS THIS SECTION'S ORDER AND
// NOT THE PAGE'S. The /standing page leads with what the person touched most
// recently, because a page is about what they are thinking about now. A world
// section is about what the work must obey, and there the settled conditions
// lead: an order that has stood for months is the house rule, and the one made
// this morning is already in the conversation above.
//
// AND WHEN SOMETHING MUST GO, IT IS NEVER A HOLD. The clip exists because a
// preamble has a budget ([standingWorldMost]), and the rows that must survive it
// are the ones that govern the work — eight reminders crowding out the one rule
// in the project would be this section spending its whole allowance on the half
// that asks for nothing. Within the holds, and within what room is left for the
// rest, the longest-standing still survive.
//
// ONE ORDER IS ONE LINE. A compiled prompt may be written across several lines,
// and a list whose rows are paragraphs is a list nothing can count, so the
// whitespace inside an order is folded before it is written.
func renderStandingWorld(items []standing.Item, places []workspace.GoverningCollection, closing string) string {
	kept := append([]standing.Item(nil), items...)
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].Created.Before(kept[b].Created) })
	var holding, waiting []string
	for _, item := range kept {
		folded := strings.Join(strings.Fields(item.Prompt()), " ")
		if folded == "" {
			continue
		}
		if item.When.Kind == standing.WhenHold {
			if through := reachedThrough(item, places); through != "" {
				folded = "[" + through + "] " + folded
			}
			holding = append(holding, folded)
			continue
		}
		waiting = append(waiting, folded)
	}
	if len(holding)+len(waiting) == 0 {
		return ""
	}

	over := 0
	if total := len(holding) + len(waiting); total > standingWorldMost {
		over = total - standingWorldMost
		if len(holding) > standingWorldMost {
			over = len(waiting)
			waiting = nil
		} else {
			waiting = waiting[:standingWorldMost-len(holding)]
		}
	}

	var out strings.Builder
	out.WriteString(standingWorldHeading + ":\n\n")
	// The placement sentence rides only over conditions it explains: a section
	// of reminders alone has no folder to mark.
	if placed := placedIn(places); placed != "" && len(holding) > 0 {
		out.WriteString(fmt.Sprintf(standingWorldPlaced, placed) + "\n\n")
	}
	written := false
	for _, tier := range []struct {
		intro string
		lines []string
	}{{standingWorldHolding, holding}, {standingWorldWaiting, waiting}} {
		if len(tier.lines) == 0 {
			continue
		}
		if written {
			// The tiers are separated by a blank line, so the second sentence
			// reads as an opening and not as one more row of the list above it.
			out.WriteString("\n")
		}
		written = true
		out.WriteString(tier.intro + "\n\n")
		for _, line := range tier.lines {
			out.WriteString("- " + line + "\n")
		}
	}
	if over > 0 {
		out.WriteString("…" + strconv.Itoa(over) + " more\n")
	}
	if closing != "" {
		out.WriteString("\n" + closing + "\n")
	}
	return out.String()
}

// placedIn names the folders work is placed in, directly placed first and the
// folders those sit in after them, or "" for work placed nowhere (the emptiness
// law: no placement, no sentence).
func placedIn(places []workspace.GoverningCollection) string {
	var direct, above []string
	for _, place := range places {
		name := strings.TrimSpace(place.Name)
		if name == "" {
			continue
		}
		if place.Depth == 0 {
			direct = append(direct, name)
		} else {
			above = append(above, name)
		}
	}
	if len(direct) == 0 {
		return ""
	}
	text := "the folder"
	if len(direct) > 1 {
		text += "s"
	}
	text += " " + strings.Join(direct, ", ")
	if len(above) > 0 {
		text += " (inside " + strings.Join(above, ", ") + ")"
	}
	return text
}

// reachedThrough names the folders of this work's placements a condition is
// scoped to, or "" for a condition that reaches the work some other way (its
// project, this conversation) and so has no folder to name. It names and never
// decides: the condition is here because the owner already found it applies.
func reachedThrough(item standing.Item, places []workspace.GoverningCollection) string {
	if item.Scope == nil {
		return ""
	}
	var names []string
	for _, place := range places {
		if slices.Contains(item.Scope.CollectionIDs, place.ID) && strings.TrimSpace(place.Name) != "" {
			names = append(names, strings.TrimSpace(place.Name))
		}
	}
	return strings.Join(names, ", ")
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
	return renderStandingWorld(items, nil, standingWorldReport)
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
// Every child now renders this same fresh block. The historical birth-only
// behavior below is superseded: child configs carry a read-only Governing door.
// Child execution carries no Standing mutation door. Governing is its separate
// read capability, so the same refresh replaces previous conditions each turn.

func (a *Agent) refreshStandingLocked() {
	a.standingText = a.standingBlockLocked()
}

// standingBlockLocked is the <standing> block message[0] carries, or "" when
// nothing stands over this conversation. It is tagged the way <memory>, <state>
// and <elsewhere> are tagged and joined on the same way, because a model that
// has learned to read fenced blocks in its instructions should not have to learn
// a fourth grammar for the fourth.
func (a *Agent) standingBlockLocked() string {
	a.governingPlaces = nil
	items, err := a.governingItemsLocked()
	a.governingRecords = nil
	a.governingReadError = ""
	if err != nil {
		a.governingReadError = err.Error()
		return "\n<standing>\nCurrent governing directions could not be read. Execution is stopped until they can be read.\n</standing>\n"
	}
	// All holds are rendered, so the durable exposure records every effective
	// condition. Appointments remain bounded optional background information.
	a.governingRecords = GoverningRules(items)
	if len(a.governingRecords) > governingHoldLimit {
		a.governingReadError = fmt.Sprintf("%d governing conditions exceed the limit of %d; narrow the governing scope before continuing", len(a.governingRecords), governingHoldLimit)
		a.governingRecords = nil
		return ""
	}
	section := renderStandingWorld(items, a.governingPlaces, "")
	if len(section) > governingPromptBytes {
		a.governingReadError = fmt.Sprintf("governing input exceeds %d bytes; narrow the governing scope before continuing", governingPromptBytes)
		a.governingRecords = nil
		return ""
	}
	if section == "" {
		return ""
	}
	return "\n<standing>\n" + section + "</standing>\n"
}
