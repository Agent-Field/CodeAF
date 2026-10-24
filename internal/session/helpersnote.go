package session

// THE ONE NOTE THAT LETS A CONVERSATION SUGGEST BRINGING IN HELPERS.
//
// A person deep in a piece of work can turn it into a team effort with the
// conversation in charge: the chat's Teams menu has `+ New from this session`,
// which makes a new team with this conversation as its manager, and `/helpers`
// then asks it for helpers (internal/tui3's helpers.go). Nobody finds a menu
// row they have never heard of in the middle of their work, and the model,
// which can see the work growing, is the one placed to mention it. So a
// conversation is told, once, that it may.
//
// IT IS A NOTE AND NOT A PARAGRAPH OF THE PAGE, for two reasons that are one.
// The page (prompts/system.md) is the fixed prefix every request of every turn
// of every conversation pays for, and it is held under a budget that only
// shrinks (prefixbudget_test.go); a sentence most conversations never act on
// does not belong there. And the suggestion only fits a conversation that has
// gone on: a note that lands after [helpersNoteAfter] things the person has
// said costs nothing in the short exchanges that are most of them, and arrives
// in the long ones, which are the ones whose work may have outgrown a
// conversation.
//
// IT IS ONCE, AND SAID AS RARE. It lands at most once in a process, never in a
// conversation that already has a part in a team (its role note says what it
// is), and its words make the suggestion a single sentence at the end of a
// reply, never repeated, never for small work and never instead of the work.
// Only the person can make the team; the note says so, because a model that
// thought it could would promise one and fail.
//
// IT IS FOR A CONVERSATION A PERSON SITS IN, ON THE SURFACE THAT HAS THE
// COMMAND ([Config.mayOfferHelpers]): not a task's node or worker, not an
// automation's run, not home's short `ask here` exchange.

// helpersNoteOpening is the note's identity: [isVolatileNote] tells it from
// the person's own words by it, and [Agent.lastNoteLocked] finds it by it.
const helpersNoteOpening = "Instructions from codeaf, not from the person: when to suggest helpers."

// helpersNoteAfter is how many messages the person has sent before the note
// lands: past a quick question and its follow-ups, into work that is going on.
const helpersNoteAfter = 4

// helpersNoteBody is what the note says.
const helpersNoteBody = "If the work here has grown long, with parts that could be worked on at once by a few helpers, " +
	"you may suggest a team once, briefly, at the end of a reply, in words like: " +
	"\"This could be split up and worked on in parallel by a small team. Want to bring in some helpers? " +
	"Choose + New from this session in the Teams menu and I'll suggest who.\" " +
	"Only the person can make the team, with that menu row; this conversation then becomes its manager. " +
	"Not for small or quick work, never instead of doing what was asked, and never again once you have suggested it or they passed."

// mayOfferHelpers says whether this conversation may be told it can suggest
// /helpers: one a person is sitting in ([Config.Interactive], which the
// surface sets and an automation's run clears), that is not a task's node or
// worker, not home's short `ask here` exchange, and that can be in a team at
// all ([Config.teamProfile]).
func (c Config) mayOfferHelpers() bool {
	return c.Interactive && !c.InTask && c.taskID == 0 && c.AutomationRun == nil && !c.Errand && c.teamProfile() != ""
}

// landHelpersNoteLocked lands the note when this conversation has earned it
// and has not had it. The caller holds a.mu, at the step boundary
// ([Agent.landVolatileLocked]), after the team role has been composed, so a
// conversation that is in a team is known to be one.
func (a *Agent) landHelpersNoteLocked() {
	if a.helpersNoted || a.personSeq < helpersNoteAfter || !a.config.mayOfferHelpers() {
		return
	}
	if a.teamRoleText != "" || a.lastNoteLocked(teamRoleNoteOpening) != "" {
		return
	}
	a.helpersNoted = true
	if a.lastNoteLocked(helpersNoteOpening) != "" {
		return
	}
	a.landNoteLocked(helpersNoteOpening, helpersNoteBody)
}
