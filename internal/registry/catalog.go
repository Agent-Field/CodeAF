package registry

import "github.com/Agent-Field/aforge-v2/internal/store"

// seedRows is the whole catalog, seeded faithfully from what the surface can
// already do today rather than from what 5.22 says it should eventually do.
// Every row here is traceable to a real handler:
//
//   - the 17-row slash table, internal/tui/commands.go's slashCommands
//   - the bare-letter and chord keybindings routed in
//     internal/tui/model.go's updateKey and named in internal/tui/keys.go's
//     actionRunes and internal/tui/commands.go's keyBindings
//   - the head belt verbs that journal a real store command, from
//     internal/head/toolbelt.go's control/revise/expedite tools
//
// No entry names a command kind or tool the corresponding package does not
// actually define — TestJournalKindsAreRealCommandKinds and
// TestJournalToolsAreRealBeltTools check that mechanically, and
// internal/tui's own coverage test fails the build if a slash command exists
// that has no row here.
//
// Left out on purpose, findings for the ledger rather than gaps to quietly
// fill:
//
//   - CommandSplice and CommandAmend have no seeded entry. Splice is what
//     an ordinary chat message already does on every turn, not a discrete
//     verb a person invokes; amend is issued by the reconciler itself, not
//     requested through a key, a slash, or the belt. Neither has a door yet
//     for this registry to name.
//   - The belt's steer and note tools are real and journal something (a
//     redirection broadcast, a notebook fact) but neither journals a
//     store.CommandKind, so neither fits this wave's "belt verbs that map to
//     journaled commands" seeding rule. They will want a Journal shape of
//     their own in a later wave rather than a misleading empty one now.
func seedRows() []Entry {
	return append(slashRows(),
		append(nodeKeyRows(),
			append(threadKeyRows(),
				append(beltRows(), itemRows()...)...)...)...)
}

// slashRows is the 17-command slash table, one row each, in the same order
// as internal/tui/commands.go's slashCommands. Descriptions are copied
// verbatim from there. Every one of these is also reachable from a task's
// steer line — submitSteer hands a leading "/" straight to executeSlash — so
// they are scoped to both the room and a task's activity view rather than
// ScopeThread alone.
func slashRows() []Entry {
	const slashScope = ScopeThread | ScopeNode
	return []Entry{
		{ID: "slash.graph", Verb: "show tasks", Description: "show or hide the task list",
			Scope: slashScope, Key: "alt+g", Slash: "graph"},
		{ID: "slash.self", Verb: "open self", Description: "open the employee file",
			Scope: slashScope, Key: "alt+3", Slash: "self"},
		{ID: "slash.tasks", Verb: "focus tasks", Description: "focus and expand the active-task dock",
			Scope: slashScope, Slash: "tasks"},
		{ID: "slash.node", Verb: "open node", Description: "open one piece of work by id prefix or current selection",
			Scope: slashScope, Slash: "node"},
		{ID: "slash.open", Verb: "open deliverable", Description: "open the focused deliverable in your OS",
			Scope: slashScope, Slash: "open"},
		{ID: "slash.notebook", Verb: "browse notebook", Description: "browse or search the scoped notebook",
			Scope: slashScope, Slash: "notebook"},
		{ID: "slash.history", Verb: "find history", Description: "find finished work in permanent memory",
			Scope: slashScope, Slash: "history"},
		{ID: "slash.budget", Verb: "change budget", Description: "show or change today's dollar limit",
			Scope: slashScope, Slash: "budget"},
		{ID: "slash.standing", Verb: "list standing", Description: "list the rules you have standing",
			Scope: slashScope, Slash: "standing"},
		{ID: "slash.settings", Verb: "open settings", Description: "open every setting in one place",
			Scope: slashScope, Key: "alt+,", Slash: "settings"},
		{ID: "slash.help", Verb: "open help", Description: "show the complete keyboard and command guide",
			Scope: slashScope, Key: "?", Slash: "help"},
		// /model journals now. The work slot is the one the graph carries, so a
		// change to it asks CommandSetModel of every live job this room owns —
		// the same typed command the belt would write — while the other slots
		// stay a local preference with nothing in the journal to move. The Kind
		// names what the door can journal, which is what a surface reading this
		// registry needs to know before it offers the door.
		{ID: "slash.model", Verb: "choose model", Description: "choose any model slot",
			Scope: slashScope, Slash: "model", Journal: Journal{Kind: store.CommandSetModel}},
		{ID: "slash.memory", Verb: "browse notebook (alias)", Description: "alias for /notebook",
			Scope: slashScope, Slash: "memory"},
		{ID: "slash.session", Verb: "show session", Description: "show the current session and database",
			Scope: slashScope, Slash: "session"},
		{ID: "slash.new", Verb: "new session", Description: "start a fresh chat session",
			Scope: slashScope, Slash: "new"},
		{ID: "slash.cancel", Verb: "cancel work", Description: "cancel a piece of work that has not finished",
			Scope: slashScope, Slash: "cancel", Journal: Journal{Kind: store.CommandCancel}},
		{ID: "slash.quit", Verb: "quit", Description: "exit aforge cleanly",
			Scope: slashScope, Slash: "quit"},
	}
}

// nodeKeyRows are the bare-letter accelerators that only mean something
// while a task's activity view holds the keyboard — internal/tui/node.go's
// cancelInspectedNode, restartInspectedNode, and toggleNodeSteerFocus, routed
// in internal/tui/model.go's updateKey inside the `if m.nodeViewID != ""`
// branch. None of these three has a slash alias: a task is inspected by
// opening it, not by typing at it.
func nodeKeyRows() []Entry {
	return []Entry{
		{ID: "key.node.cancel", Verb: "cancel", Description: "cancel the worker being inspected",
			Scope: ScopeNode, Key: "c", Journal: Journal{Kind: store.CommandCancel}},
		{ID: "key.node.restart", Verb: "restart", Description: "restart a worker that failed or was cancelled",
			Scope: ScopeNode, Key: "r", Journal: Journal{Kind: store.CommandRestart}},
		{ID: "key.node.steer-focus", Verb: "steer or read", Description: "swap the keyboard between the steer line and the feed",
			Scope: ScopeNode, Key: "tab"},
	}
}

// threadKeyRows are the bare-letter and chord accelerators that act on the
// room rather than on one task, routed in internal/tui/model.go's updateKey
// below its node-view guard (v, y, Y, tab, [, ]) or above it, unconditioned
// on which task is open (the alt chords, ctrl+c). The chords that also act
// from inside a task's activity view carry ScopeNode too, matching exactly
// where updateKey actually dispatches them.
func threadKeyRows() []Entry {
	const everywhere = ScopeThread | ScopeNode
	return []Entry{
		// The one row with two true accelerators. internal/tui routes it from
		// a bare "v" (help.go names it); internal/tui2/chat routes it from
		// ctrl+r, because a composer-first room hands every printable key to
		// the draft. Both are real bindings in live code, so both are
		// recorded — see [Surface] for why neither may be dropped or
		// rewritten into the other.
		{ID: "key.thread.receipts", Verb: "toggle receipts", Description: "expand or collapse reading receipts",
			Scope: ScopeThread, Key: "v", ChordKey: "ctrl+r"},
		// Both copies carry a chord beside their bare key for the reason the
		// receipts row above them does: in a composer-first surface every
		// printable character is text, so a surface that binds these binds the
		// chord, and Entry.On projects the row onto whichever key that surface
		// really has. Recording only the bare key made both rows unrunnable
		// wherever the composer holds the keyboard.
		{ID: "key.thread.copy-answer", Verb: "copy answer", Description: "copy the focused answer to the clipboard",
			Scope: ScopeThread, Key: "y", ChordKey: "ctrl+y"},
		{ID: "key.thread.copy-file", Verb: "copy file", Description: "copy the path of the file the answer produced",
			Scope: ScopeThread, Key: "Y", ChordKey: "alt+y"},
		// The composer's own kill chord. It is seeded from a live handler like
		// every other row here — internal/tui2/composer's Model.KillToStart,
		// bound to ctrl+u — and it is in the catalog rather than left as a
		// muscle-memory chord because 5.22's law admits no typed-only action:
		// the key is the accelerator, and the `?` sheet and the summon palette
		// are the visible doors. Its Key is already a chord, so it needs no
		// ChordKey to survive a composer-first surface (see [Entry.KeyOn]) —
		// which is the whole reason ctrl+u is bindable in a room where "v" and
		// "y" are not.
		{ID: "key.thread.clear-draft", Verb: "clear draft", Description: "clear the draft from the cursor back to its start",
			Scope: ScopeThread, Key: "ctrl+u"},
		// The thread switcher (chat-simplify.md 5.2's J3). It is recorded with
		// BOTH spellings for the reason the receipts and copy rows above it are:
		// `t` is the key the doc names and the one a reader learns, and in a
		// composer-first room a bare letter is draft text — so the chat surface
		// claims `t` only where it cannot mean anything else (an empty draft, no
		// overlay) and binds alt+t everywhere. Both are real bindings in live
		// code, so both are recorded.
		{ID: "key.threads", Verb: "switch thread", Description: "open the list of working conversations, or start one",
			Scope: ScopeThread, Key: "t", ChordKey: "alt+t"},
		{ID: "key.thread.narrow-split", Verb: "narrow split", Description: "narrow the chat/task split",
			Scope: ScopeThread, Key: "["},
		{ID: "key.thread.widen-split", Verb: "widen split", Description: "widen the chat/task split",
			Scope: ScopeThread, Key: "]"},
		{ID: "key.thread.cycle-focus", Verb: "cycle focus", Description: "cycle the keyboard through input, questions, thread, and the task list",
			Scope: ScopeThread, Key: "tab"},
		{ID: "key.thread.newline", Verb: "insert newline", Description: "insert a newline in the draft without sending",
			Scope: ScopeThread, Key: "ctrl+j"},
		// The summon key. ctrl+space is the chord because a terminal never
		// delivers a cmd- chord and almost nothing squats on this one — it is
		// the NUL byte, which every decoder in the stack already names
		// "ctrl+space" — so it is bindable in a composer-first room without
		// taking a letter away from the draft. ctrl+k goes on working and is
		// NOT a second row: the rule this file already keeps is that where a
		// surface accepts two spellings of one chord, Key names the one the
		// help screen leads with and the synonym is not a second registration.
		{ID: "key.palette", Verb: "find anything", Description: "search every job, room, action and setting in one list",
			Scope: everywhere, Key: "ctrl+space"},
		{ID: "key.quit", Verb: "stop or quit", Description: "stop a reply on its way; press again within seconds to quit, or quit at once when idle",
			Scope: everywhere, Key: "ctrl+c"},
		{ID: "key.place-thread", Verb: "go to thread", Description: "open the home thread",
			Scope: everywhere, Key: "alt+1"},
		{ID: "key.place-board", Verb: "go to board", Description: "open the task board",
			Scope: everywhere, Key: "alt+2"},
		{ID: "key.voice", Verb: "talk", Description: "start or finish voice input",
			Scope: everywhere, Key: "alt+v"},
		{ID: "key.boost", Verb: "boost", Description: "cycle boosted next answer, pinned, or off",
			Scope: everywhere, Key: "alt+b"},
	}
}

// beltRows are the head belt verbs that journal a real store command,
// reachable only through the user's own words — never a key, never a slash.
// Seven store verbs, two tools: internal/head/toolbelt.go's stop tool withdraws
// and its change tool carries everything else, with no verb argument at all —
// the words go over the funnel and the revision judge reads them. The rows stay
// one per store kind because this catalog is an index of what the product can
// DO, and what a surface renders is a verb rather than a tool.
func beltRows() []Entry {
	const (
		stopTool   = "stop"
		changeTool = "change"
	)
	return []Entry{
		{ID: "belt.cancel", Verb: "cancel", Description: "cancel the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandCancel, Tool: stopTool}},
		{ID: "belt.pause", Verb: "pause", Description: "pause the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandPause, Tool: stopTool}},
		{ID: "belt.resume", Verb: "resume", Description: "resume the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandResume, Tool: changeTool}},
		{ID: "belt.restart", Verb: "restart", Description: "restart the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandRestart, Tool: changeTool}},
		{ID: "belt.reprioritize", Verb: "reprioritize", Description: "move the jobs named in the conversation ahead of the rest",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandReprioritize, Tool: changeTool}},
		{ID: "belt.revise", Verb: "revise", Description: "edit a job's remaining plan to match what the user just said",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandRedirect, Tool: changeTool}},
		{ID: "belt.expedite", Verb: "expedite", Description: "push a job to the front of the queue and trim its unstarted tail",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandExpedite, Tool: changeTool}},
	}
}

// itemRows are the verbs that act on ONE thing the resident knows or is doing,
// drawn on that thing's own page: a standing rule, a service, a belief, a way
// of working, a forged tool. They are [ScopeItem] — see that scope for why they
// stay out of the unscoped palette — and they have no key and no slash alias by
// construction: the accelerator for "retire this" is having the thing open, and
// a letter that meant retire from anywhere would be a letter that eventually
// retires the wrong thing.
//
// Two shapes, and which one a row is is data rather than a rule a surface has
// to remember. A PURE command (pause, stop, forget, run) fires on the spot,
// with a [Entry.Confirm] on the ones that cannot be undone by doing the
// opposite. A verb that carries an argument the resident must interpret — a new
// cadence, a corrected belief, the reason a version goes back — carries a
// [Entry.Steer] instead and seeds the composer, because the one mouth is the
// conversation and a form field is not it.
//
// Every Kind here is a real store command kind and every one of them has an
// executor: charter and service kinds are the ones internal/head's rule and
// service tools already journal, and the craft and skill kinds are applied in
// internal/resident beside them. The two belief rows journal no command kind at
// all — a belief is not graph work — so they name the belt tool that carries
// them, which is the same honesty rule the belt rows above keep.
func itemRows() []Entry {
	return []Entry{
		{ID: "charter.pause", Verb: "pause", Description: "stop this rule running until you start it again",
			Scope: ScopeItem, Journal: Journal{Kind: store.CommandCharterPause}},
		{ID: "charter.cadence", Verb: "change when", Description: "say when this should run instead",
			Scope: ScopeItem, Steer: "change %s to run ",
			Journal: Journal{Kind: store.CommandCharterCadence}},
		{ID: "charter.probation", Verb: "ask first", Description: "have it ask you before it runs again",
			Scope: ScopeItem, Journal: Journal{Kind: store.CommandCharterProbation}},
		{ID: "charter.retire", Verb: "retire", Description: "stop watching for this for good",
			Scope: ScopeItem, Confirm: "Retire this rule? It stops watching for good.",
			Journal: Journal{Kind: store.CommandCharterRetire}},

		{ID: "service.stop", Verb: "stop", Description: "stop this running for now",
			Scope: ScopeItem, Confirm: "Stop this? Whatever it was serving goes down with it.",
			Journal: Journal{Kind: store.CommandServiceStop}},
		{ID: "service.restart", Verb: "restart", Description: "stop it and start it again",
			Scope: ScopeItem, Journal: Journal{Kind: store.CommandServiceRestart}},
		{ID: "service.autorestart", Verb: "auto-restart", Description: "bring it back on its own when it stops answering",
			Scope: ScopeItem, Journal: Journal{Kind: store.CommandServiceAutoRestart}},

		{ID: "belief.forget", Verb: "forget", Description: "stop remembering this",
			Scope: ScopeItem, Confirm: "Forget this? I stop working from it.",
			Journal: Journal{Tool: "forget"}},
		{ID: "belief.edit", Verb: "edit", Description: "say what I should remember instead",
			Scope: ScopeItem, Steer: "what I should remember instead of %s is ",
			Journal: Journal{Tool: "note"}},

		{ID: "craft.run", Verb: "run", Description: "do this the way you have before",
			Scope: ScopeItem, Journal: Journal{Kind: store.CommandCraftRun}},
		{ID: "craft.revert", Verb: "revert", Description: "go back to the version before this one",
			Scope: ScopeItem, Steer: "put %s back to the previous version because ",
			Journal: Journal{Kind: store.CommandCraftRevert}},
		{ID: "craft.retire", Verb: "retire", Description: "stop working this way",
			Scope: ScopeItem, Confirm: "Retire this way of working? I stop reaching for it.",
			Journal: Journal{Kind: store.CommandCraftRetire}},

		{ID: "skill.retire", Verb: "retire", Description: "take this tool off the shelf",
			Scope: ScopeItem, Confirm: "Retire this tool? It comes off the shelf and stops being used.",
			Journal: Journal{Kind: store.CommandSkillRetire}},
	}
}
