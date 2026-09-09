package tui3

// ── A LENS IS A POSTURE, NOT A PAGE ─────────────────────────────────────────
//
// A LENS IS A POSTURE, NOT A PAGE. [participantLens] is a person INSIDE the
// conversation — thinking alongside the model, owed every word of the dialogue
// at full fidelity, with finished work folded away behind a chip.
// [overseerLens] is a person CHECKING ON work another agent is doing — owed the
// trajectory at a glance, with the machinery one gesture down. A third surface
// picks one of these or argues, in writing, for a third; [transcriptLens] is
// the one that argued, and its comment is the argument.
//
// It exists because the two pages had drifted apart one knob at a time. The
// deck carried a `clock` bool, a `showsWork` bool and a `toolTail` int, each
// added by the change that needed it, each documented where it was declared —
// so the question "how does a room differ from the conversation" had no answer
// short of grepping three files, and every new difference was settled by whim
// rather than derived from what the two readers are DOING. The whole difference
// is now readable in the two literals at the foot of this file, and a fourth
// knob has to be added to a struct that states, in one place, what it is for.
//
// NOTHING ON SCREEN EVER SAYS "LENS". These are code names for a posture, and
// NO MACHINERY VOCABULARY (CLAUDE.md) governs every word a person reads: the
// surfaces are "the conversation" and "a task's page", and they say so.
//
// THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A FACT. Every field below moves
// something nearer or further from the eye. None of them decides whether an
// event is ingested, and none of them may: the reducer is one reducer
// (feed.go), and a difference that has to delete a fact is not a lens.

// receiptsAt is WHERE THE NUMBERS LAND — beside the work that spent them, or
// gathered in one place at the top of the page.
type receiptsAt int

const (
	// receiptsInline is the conversation's: a turn's elapsed and its cost sit
	// under the turn, because out there a turn is one exchange and the person
	// reading it is the person who paid for it (timestamps.go).
	receiptsInline receiptsAt = iota
	// receiptsHeader is a room's: a node's life is one long turn, so per-turn
	// receipts down the scroll would be one figure repeated at every step. The
	// header carries the total instead (room.go's [app.roomHeadWord]).
	receiptsHeader
)

// foldStyle is WHAT COLLAPSES ON THIS PAGE, and it is a value rather than a
// pair of bools so that adding a fourth page is choosing from a list rather
// than reasoning about which combination of flags means what.
type foldStyle int

const (
	// foldTurns is the conversation's: one chip per completed turn, hiding the
	// machinery between a question and its answer (workfold.go's
	// [deriveWorkfolds]).
	foldTurns foldStyle = iota
	// foldPhases is a room's: one chip per settled phase inside the node's one
	// trunk turn, hiding the machinery that preceded each settled paragraph
	// (workfold.go's [derivePhaseFolds]).
	foldPhases
	// foldNone folds nothing at all. It is not a default — [foldTurns] is the
	// zero value on purpose, because the conversation is the surface every
	// other one is derived from — and the one page that takes it argues for it
	// at [transcriptLens].
	foldNone
)

// folders is the fold-style dispatch, as a TABLE. A page's fold policy is data
// about the page, and an `if` ladder over three styles is a fourth style away
// from being unreadable — the table also states, by omission, that [foldNone]
// derives nothing rather than deriving an empty answer twice.
var folders = map[foldStyle]func(d deck) map[int]workfold{
	foldTurns:  func(d deck) map[int]workfold { return deriveWorkfolds(d.entries, d.runningTurn) },
	foldPhases: func(d deck) map[int]workfold { return derivePhaseFolds(d.entries) },
}

// lens is the WHOLE of what one page does differently from another.
//
// Every field is a salience decision and every one of them is read by exactly
// one place, named in its comment. A field nobody reads is a claim about the
// design that the program does not make, so there are none.
type lens struct {
	// clock says this page carries THE SESSION'S CLOCK — the per-turn receipts
	// and the seam marks (timestamps.go, read by render.go's [app.deckRows]).
	// Only the conversation does: [app.stamps] is keyed by the SESSION's turns,
	// so a page that ran the clock over a node's turns would draw the
	// conversation's receipts against somebody else's numbering and report
	// figures nobody measured.
	clock bool
	// receipts is where this page's accounting lands (workfold.go's
	// [app.workfoldLabel], room.go's [app.roomHeadWord]).
	receipts receiptsAt
	// foldPast is what collapses here, resolved through [folders] by
	// workfold.go's [app.deckFolds].
	foldPast foldStyle
	// compactLive covers only running machinery, independently of past folds.
	compactLive bool
	// toolTail is how many of a folded cluster's calls stay on screen, and nil
	// means [toolWindow] — the conversation's designed compactness, where the
	// fold sits among prose. It is a FUNCTION rather than a number because a
	// room's answer is its own view's height (room.go's [app.roomToolTail]) and
	// the height changes under a resize, a rail tier change and a draft growing
	// a line; it is resolved where the rows are built ([app.window]) so that
	// every caller measures the frame it is actually drawing.
	toolTail func(a *app) int
	// spawnCards says the reducer may grow a block for a task proposal
	// (app.go's [app.feedHooks]). A room has kin rows and no card, so the
	// hooks that mint one are not installed there — the EVENT is still
	// ingested by the same reducer, which is the difference between lowering
	// salience and dropping a fact.
	spawnCards bool
}

// participantLens is the person inside the conversation: the dialogue at full
// fidelity, its clock and its receipts under each turn, and finished work
// folded to a chip because they asked a question and what they were owed was
// the answer.
var participantLens = lens{
	clock:       true,
	receipts:    receiptsInline,
	foldPast:    foldTurns,
	compactLive: true,
	spawnCards:  true,
}

// overseerLens is the person checking on work somebody else is doing: no clock
// of ours over their turns, the numbers gathered in the header, settled phases
// folded to chips, and the live frontier kept compact. Opening its outline
// retains a screenful of calls, independently of the three-row compact budget.
var overseerLens = lens{
	receipts:    receiptsHeader,
	foldPast:    foldPhases,
	compactLive: true,
	toolTail:    (*app).roomToolTail,
}

// transcriptLens is THE THIRD SURFACE, AND HERE IS THE ARGUMENT.
//
// A run's page is a graph, and clicking a node in it opens that node's
// transcript inside the graph (roomorch.go). The gesture that gets a person
// there is already "show me what this one did" — they descended two pages to
// ask it — and the page answers by drawing the tail of the journal under the
// graph. Folding settled phases back up would answer a request to see the
// machinery with a chip saying machinery happened, which is the shape ruling 1
// reversed for the ROOM and would be reintroducing it one page down.
//
// AND THE LADDER WOULD DEAD-END, which is the half that settles it. That page
// mints its fold state fresh on every read — the map is a literal in
// [app.orchTranscriptDeck], not a field anything keeps — so a chip drawn there
// could be pressed and would never open. A fold whose door does nothing is
// worse than no fold, and DISCOVERABILITY BEFORE PURITY says a disclosure
// ships with its working door or does not ship.
//
// THE SECOND HALF IS A LIMITATION AND NOT A PRINCIPLE, and the difference
// matters for whoever reads this next. Only the first paragraph is an argument
// about what a person came to that page for; the dead end is an accident of how
// that deck is built. So if [app.orchTranscriptDeck] ever KEEPS its fold map —
// on the run, the way a room keeps its own — this lens should collapse back into
// [overseerLens] and be deleted, not go on standing as a third posture because
// it happens to exist. A page that folds nothing has to re-earn that every time
// somebody asks.
//
// It keeps the room's tool tail: the same reason, said about the same rows.
var transcriptLens = lens{
	receipts: receiptsHeader,
	foldPast: foldNone,
	toolTail: (*app).roomToolTail,
}
