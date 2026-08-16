package chat

import tea "charm.land/bubbletea/v2"

// Per-block disclosure: the fold a reader opens one row at a time.
//
// 7.2's transcript backlog asks for "per-block expand/collapse (▸/▾) with state
// that survives re-render", and 13.4's audit answered T2 PARTIAL for two years
// of waves running: "fold survives re-render but toggle is GLOBAL on ctrl+r …
// no per-block toggle, no click". 13.14 shipped the pointer everywhere else and
// filed the same gap in its own still-open list — "transcript blocks have no
// per-block fold-on-click".
//
// WHY IT IS THE WHOLE OF A REPORT AND NOT A CONVENIENCE. Measured against the
// reader's own journal: their bitcoin research room draws two rows, and both of
// them are collapsed. The charge holds seven folded lines of the brief; the
// delivery card holds sixty-two folded lines of the answer, and the ONE line it
// shows above the fold is the worker's throat-clearing — "I have solid data
// now. Let me consolidate what I have:". Every word the room was entered for
// was behind a `▸` that no click opened. "It shows nothing when I click and go
// there" is a precise description of that frame: the record was drawn, and the
// only door to it was a keystroke advertised as "toggle receipts".
//
// TWO DOORS, ONE STATE. `ctrl+r` stays exactly what it was — the room-wide
// toggle 13.10 advertises in the footer — and the pointer now opens one row.
// They cannot disagree because the per-block overrides are what the global
// toggle CLEARS: after ctrl+r every row in the room is on the same side of its
// fold, which is what a reader who pressed "open everything" asked for, and a
// global open that quietly left three rows shut would be the affordance lying.
//
// STATE SURVIVES RE-RENDER, which is 7.2's actual requirement and the only hard
// part. A task room rebuilds its whole block list on every journal move
// (13.15's decision 2), so a flag living on the block would be thrown away
// several times a second while a job runs. The override is keyed by BLOCK ID —
// `msg-<seq>`, `room-charge`, `room-work-<node>`, all of them stable across
// rebuilds by construction (record.go) — so a row the reader opened comes back
// open, and a row that has not been born yet inherits the room's global state.

// foldOpen is whether the block with this id should be drawn expanded: the
// reader's own decision if they have made one, and the room's otherwise.
func (a *App) foldOpen(id string) bool {
	if open, chosen := a.folds[id]; chosen {
		return open
	}
	return a.receiptsOpen
}

// applyFold puts a block on the side of its fold the reader last asked for.
// Every path that appends a message block to a transcript goes through it, so
// there is one answer to "is this row open" and one place it is given.
func (a *App) applyFold(block *messageBlock) {
	if block == nil || !block.collapsible {
		return
	}
	block.SetExpanded(a.foldOpen(block.ID()))
}

// toggleFold is the pointer's door: it flips ONE row and remembers that the
// reader flipped it.
//
// It returns no command and never can. A fold is a change to what is already on
// screen — nothing is read, posted, answered or spent — so it is finished the
// moment the bytes are re-rendered, and the invalidate is the whole of it.
func (a *App) toggleFold(block *messageBlock) tea.Cmd {
	if block == nil {
		return nil
	}
	// THE CARD IS A DOOR TO ITS TASK, and it is the same seam because it is the
	// same gesture: the pane resolved a cell to a block, the block recorded
	// which of its doors that cell was on ([messageBlock.doorAt]), and the ACT
	// belongs here. §3's law is that "the whole block is a click target to the
	// record, from the moment it appears"; the reader filed its absence — "the
	// card has no click to go to task at all".
	if block.door == doorRoom {
		return a.openCardRoom(block)
	}
	if !block.collapsible {
		return nil
	}
	open := !block.Expanded()
	if a.folds == nil {
		a.folds = make(map[string]bool, 4)
	}
	a.folds[block.ID()] = open
	moved := block.SetExpanded(open)
	// AN EXECUTION ROW'S FOLD CHANGES WHICH ROWS EXIST, so it is a rebuild and
	// not a re-render. A batched run of nine searches lays out its nine calls
	// when it opens, and a long result grows the continuation that holds the
	// rest of itself — neither is a segment inside the row that was clicked, so
	// re-rendering that row alone would open a door onto nothing. The stamp is
	// cleared because it fingerprints the JOURNAL, and nothing in the journal
	// moved: the reader did.
	if isTraceBlockID(block.ID()) && a.view != nil && a.view.kind == viewNode {
		a.view.stamp = ""
		a.paintRoom()
		moved = true
	}
	if moved {
		a.shell.Invalidate()
	}
	return nil
}

// clearFolds drops every per-block override, which is what makes the global
// toggle global again. See the note above on the two doors.
func (a *App) clearFolds() {
	for id := range a.folds {
		delete(a.folds, id)
	}
}

// openCardRoom walks into the task a card is about.
//
// It goes through [App.jumpTo] — the same door the palette, the board and an
// `@job` mention already open — rather than calling openTaskRoom directly, so
// the rail's cursor lands on the row the reader entered and esc walks back out
// the way it always did. One door, a fifth hand on it.
//
// The id is the RAIL's, not the graph's: a task row is `task:<node>` in every
// scope the map builds (scope.go), and jumpTo is a lookup against those rows.
// A card whose job the map has never heard of opens nothing rather than
// guessing, which is 5.20 rule 1 — a surface may not name a door onto nowhere.
func (a *App) openCardRoom(block *messageBlock) tea.Cmd {
	if block == nil || block.job == "" {
		return nil
	}
	return a.jumpTo(rowTaskPrefix + block.job)
}
