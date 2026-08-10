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
//   - /model does not journal store.CommandSetModel. The command kind is
//     real (internal/resident/setmodel.go applies it, and the belt could
//     reach it), but the TUI's slash handler and the models palette call
//     commander.SetModel, which repoints a session's own client model
//     locally — cmd/aforge/chat.go's chatCommander.SetModel — rather than
//     requesting the journaled command. 5.23's live subtree rebinding is
//     therefore not yet wired to this door; the entry below carries an
//     empty Journal until it is.
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
	return append(slashRows(), append(nodeKeyRows(), append(threadKeyRows(), beltRows()...)...)...)
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
		{ID: "slash.model", Verb: "choose model", Description: "choose any model slot",
			Scope: slashScope, Slash: "model"},
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
		{ID: "key.thread.receipts", Verb: "toggle receipts", Description: "expand or collapse reading receipts",
			Scope: ScopeThread, Key: "v"},
		{ID: "key.thread.copy-answer", Verb: "copy answer", Description: "copy the focused answer to the clipboard",
			Scope: ScopeThread, Key: "y"},
		{ID: "key.thread.copy-file", Verb: "copy file", Description: "copy the path of the file the answer produced",
			Scope: ScopeThread, Key: "Y"},
		{ID: "key.thread.narrow-split", Verb: "narrow split", Description: "narrow the chat/task split",
			Scope: ScopeThread, Key: "["},
		{ID: "key.thread.widen-split", Verb: "widen split", Description: "widen the chat/task split",
			Scope: ScopeThread, Key: "]"},
		{ID: "key.thread.cycle-focus", Verb: "cycle focus", Description: "cycle the keyboard through input, questions, thread, and the task list",
			Scope: ScopeThread, Key: "tab"},
		{ID: "key.thread.newline", Verb: "insert newline", Description: "insert a newline in the draft without sending",
			Scope: ScopeThread, Key: "ctrl+j"},
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
// internal/head/toolbelt.go's control tool carries five of them behind one
// verb argument (beltVerbKind), and revise and expedite are one tool each;
// all seven share the property that a model composing the belt, not a typed
// accelerator, is the only door.
func beltRows() []Entry {
	const controlTool = "control"
	return []Entry{
		{ID: "belt.cancel", Verb: "cancel", Description: "cancel the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandCancel, Tool: controlTool}},
		{ID: "belt.pause", Verb: "pause", Description: "pause the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandPause, Tool: controlTool}},
		{ID: "belt.resume", Verb: "resume", Description: "resume the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandResume, Tool: controlTool}},
		{ID: "belt.restart", Verb: "restart", Description: "restart the jobs named in the conversation",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandRestart, Tool: controlTool}},
		{ID: "belt.reprioritize", Verb: "reprioritize", Description: "move the jobs named in the conversation ahead of the rest",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandReprioritize, Tool: controlTool}},
		{ID: "belt.revise", Verb: "revise", Description: "edit a job's remaining plan to match what the user just said",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandRedirect, Tool: "revise"}},
		{ID: "belt.expedite", Verb: "expedite", Description: "push a job to the front of the queue and trim its unstarted tail",
			Scope: ScopeTalk, Journal: Journal{Kind: store.CommandExpedite, Tool: "expedite"}},
	}
}
