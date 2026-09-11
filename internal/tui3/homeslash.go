package tui3

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE COMMAND LIST IS THE ONE THING HOME'S COMPOSER DID NOT HAVE. Chat's box
// answers a "/" with a ranked menu and enter runs the row; home's box used to
// answer the same characters by starting a conversation with them, which is
// the one screen where a command typed in full did nothing it promised. This
// file is home's half of that parity: the offers in the drop-up, and the enter
// that runs them. The ranking, the token-finding, the sealing AND the choosing
// are not reimplemented — they are the chat composer's own [menu] and its own
// [chooseCommand], run over home's box, so the two surfaces cannot grow two
// answers to what "/mo" offers or to what enter does with the row.
//
// THE DISPATCH IS MOSTLY NOT HERE EITHER. [app.homeSlash] hands the line to
// [app.slash] — the same switch chat's enter runs — through a gate that first
// asks what the command MEANS on a screen with no conversation in front of it.
// That gate is the second half of this file and it replaced a ruling: that the
// conversation-scoped commands act on the conversation this window holds behind
// the screen, "which is parity rather than a limitation: the window always
// holds one." The window does always hold one. What it does not do is DRAW one,
// and a command whose answer is drawn where nobody is looking is not parity.

// homeCommand is one slash command, offered in the drop-up because what was
// typed matches its name or one of its aliases. It is numbered OUTSIDE the
// [homeRowKind] iota block for [homePlace]'s reason: that block is edited by
// other lanes in the same wave, and a constant appended to it is a conflict
// over a line that says nothing.
const homeCommand homeRowKind = 250

// commandLines is the command half of the drop-up: the ranked rows for the
// /-word under the caret, or nothing when the caret is not in one.
//
// THE CHAT COMPOSER'S OWN SYNC RANKS THEM. [menu.sync] finds the token, keeps
// the seal a chosen row left, and ranks the matches with the one ranker
// (commands.go's [menu.rank]) — aliases surface the canonical row there, so
// they do here too, and a second ranker on this screen would be two answers to
// what "/conf" offers.
//
// THE ROWS RISE BEST-FIRST OUT OF THE BOX, which is the drop-up's law
// ([homeView.placeLines] states it): the last line appended is the row against
// the box, and the best match is the one a person should read first. The cap
// is [menuRows] — chat's list shows eight rows at once, and a drop-up that
// offered forty would bury the conversation matches the same characters also
// found.
func (h *homeView) commandLines() []homeLine {
	h.cmd.sync(&h.box)
	if !h.cmd.open {
		return nil
	}
	hits := h.cmd.hits
	if len(hits) > menuRows {
		hits = hits[:menuRows]
	}
	lines := make([]homeLine, 0, len(hits))
	for i := len(hits) - 1; i >= 0; i-- {
		// A POINTER INTO THE ONE COMMAND TABLE, which is built once at init and
		// never rewritten, so a row can hold it the way it holds a place's word
		// rather than the way it must not hold a world index ([homeLine.row]
		// states that law).
		lines = append(lines, homeLine{kind: homeCommand, cmd: &commands[hits[i]]})
	}
	return lines
}

// homeCommandRow paints one offered command: the word a person would type, and
// in the right margin WHAT ENTER WILL DO WITH IT HERE, then the command's own
// note (commands.go's [command.note]).
//
// THE MARGIN USED TO SAY `a command`, which every row of the list already
// demonstrated by existing. What a person standing on home cannot know is the
// thing this list is now the only place to learn: that `/compact` opens a
// conversation before it runs, that `/model` changes a draft and touches nothing
// behind the screen, that `/quit` closes a conversation they cannot see. The
// owner asked for that to be readable BEFORE the key is pressed, including for
// the commands that do nothing useful here — so the fate leads the margin and
// the note follows it.
//
// AND THE FATE IS NEVER THE HALF THAT IS CUT. [overlayRow] fits the whole margin
// to the room the label leaves, from the right, so a margin built as one string
// would lose its tail — which on a narrow home would be the note on one row and
// half the fate on the next. The two are budgeted here instead: the note takes
// what is left after the fate, whole or not at all.
func (a *app) homeCommandRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	label := line.cmd.typed()
	return overlayRow(label, commandMargin(label, *line.cmd, width),
		at == h.cursor, false, at == h.hover, width, pal)
}

// commandMargin is that margin: the fate, and the note if there is room for a
// readable amount of it.
func commandMargin(label string, c command, width int) string {
	fate := homeFate(c.name, c.args)
	note := c.note()
	switch {
	case fate == "":
		// A COMMAND WITH NO FATE IS A PROGRAMMING MISTAKE and not a row that
		// quietly says less than its neighbours ([TestEveryCommandHasAFateAtHome]
		// fails the build on it). The list still draws what it can.
		return note
	case note == "":
		return fate
	}
	spare := overlayNoteRoom(label, width) - ansi.StringWidth(fate) - ansi.StringWidth(commandFateJoin)
	if spare < commandNoteFloor {
		return fate
	}
	return fate + commandFateJoin + fit(note, spare)
}

const (
	// commandFateJoin is the middot between the fate and the note, the same
	// separator every other margin on this surface joins two clauses with.
	commandFateJoin = " · "
	// commandNoteFloor is the least room a command's own note is worth drawing
	// in after the fate has taken its share. Under it the fate stands alone:
	// three cells of a sentence with an ellipsis after them teach nobody
	// anything, and what enter DOES here is the half a person came for.
	commandNoteFloor = 12
)

// ── THE GATE: WHAT A COMMAND MEANS ON A SCREEN WITH NO CONVERSATION ─────────
//
// Every command used to go straight to [app.slash], and the header above calls
// that parity. It was parity for about half of them and a trapdoor for the
// rest: ten commands answer by opening a bottom-anchored overlay, which a place
// cannot draw and the keyboard cannot reach (pages.go's [app.closeModals] tells
// that story), and sixteen more answer with a note written into a conversation
// nobody is looking at. So the same three characters did three different things
// depending on which command they spelled, and two of the three were silence.
//
// EVERY COMMAND HAS A FATE HERE, IT IS WRITTEN DOWN ONCE, AND IT IS ON THE ROW
// BEFORE THE KEY IS PRESSED. [homeFate] is that one place: the drop-up draws it
// in the row's margin ([app.homeCommandRow]) and [app.homeSlash] switches on it,
// so the list cannot promise something the dispatch does not do. A command added
// to the table with no fate fails the build rather than drawing a blank margin
// and quietly taking the last road ([TestEveryCommandHasAFateAtHome]).
//
//	pins the next conversation's model   /model — the target, and home says so
//	next conversation's folder           /folder /place /dir — the browser, aimed
//	                                     at the target (folderplace.go)
//	opens the page                       a place replaces a place
//	this list is /resume                 the thing asked for is on the screen
//	onto home's tray                     /attach /image — the files home is
//	                                     already carrying into the next one
//	opens a conversation here first      it opens one AT THE TARGET (homedraft.go)
//	                                     and runs there, the door `enter` uses
//	answers here                         a note, echoed onto home's own line
//	runs on the conversation behind home  /land /workspace — and they say so
//	a fresh conversation behind home     /new
//	closes the conversation behind home  /quit
//
// AND EVERY ONE OF THEM LEAVES A SENTENCE ON HOME'S LINE. The three that act on
// the conversation behind the screen are the ones this cost most: /new, /quit
// and /land each did their work in a place nobody could see and said nothing at
// all where the person was standing.

// The fates. Each is a person-facing phrase drawn in the drop-up's margin and
// quoted in the manual exactly as it is spelled here, so they are constants and
// not string literals in two files.
const (
	fateTargetModel  = "pins the next conversation's model"
	fateTargetFolder = "next conversation's folder"
	fatePlace        = "opens the page"
	fateResume       = "this list is /resume"
	fateTray         = "onto home's tray"
	fateNeedsChat    = "opens a conversation here first"
	fateAnswers      = "answers here"
	fateBehind       = "runs on the conversation behind home"
	fateFresh        = "a fresh conversation behind home"
	fateQuit         = "closes the conversation behind home"
)

// homeFate is what a command DOES on home, and it is the whole of the gate's
// decision. It is a pure function of the word and its argument for two reasons:
// the drop-up asks it about a row while a frame is being built, where nothing
// may touch a disk or a door; and the test that walks [commands] can then ask it
// about every row without an app.
//
// THE ARGUMENT IS PART OF THE QUESTION, because four commands mean two different
// things with and without one: `/standing` is a page and `/standing <words>`
// raises a card in a conversation; `/crew` is a picker and `/crew frugal` is a
// note; `/task` is the task page and `/task <brief>` starts work; `/memory` is
// the place and `/memory <query>` prints. A table keyed on the name alone would
// send a person to the wrong one of each pair. The drop-up asks with the row's
// own placeholder ([command.args]), which is empty on exactly the bare rows.
//
// A WORD NOBODY DEFINED HAS NO FATE, and the dispatch answers it the way it
// always did — `there is no command called /x · / lists them`, on home's line.
func homeFate(word, rest string) string {
	rest = strings.TrimSpace(rest)
	switch canonicalCommand(strings.ToLower(strings.TrimPrefix(word, "/"))) {
	case "model":
		return fateTargetModel
	case "folder":
		return fateTargetFolder
	case "settings", "search", "spend", "history", "home":
		return fatePlace
	case "resume":
		return fateResume
	case "attach", "image":
		return fateTray
	case "quit":
		return fateQuit
	case "new":
		return fateFresh
	case "land", "workspace":
		return fateBehind
	case "files", "permissions", "connect", "harness", "subharness", "autonomy",
		"copy", "select", "rewind", "compact", "export":
		return fateNeedsChat
	case "standing":
		// Bare it is the standing place; with words it is a card raised in a
		// conversation, and there has to be one.
		if rest == "" {
			return fatePlace
		}
		return fateNeedsChat
	case "task":
		// AND A BARE /task IS A PAGE AND NOT A USAGE LINE (commands.go's
		// [app.runTaskCommand] says why). It used to open a conversation at the
		// target and then open the task page over it, which is a conversation
		// started for a question that never needed one.
		if rest == "" {
			return fatePlace
		}
		return fateNeedsChat
	case "crew":
		// Bare it is a picker this screen cannot draw; with a word it is a note.
		if rest == "" {
			return fateNeedsChat
		}
		return fateAnswers
	case "effort":
		// BOTH FORMS NEED A CONVERSATION, and that is what tells this apart from
		// /crew and /model. The crew is the machine's, the model has a target
		// rule home can pin — and a thinking rung is the CONVERSATION's own scope
		// (effortchip.go), so there is nothing here for it to be set on. Home's
		// own rung is not this one either: `ctrl+v` on a standing item's card
		// moves that item's, and the install's default is the `thinking` row of
		// /settings (effortscope.go).
		return fateNeedsChat
	case "memory", "memories":
		// Bare it is the memory place; a query prints matching rows.
		if rest == "" {
			return fatePlace
		}
		return fateAnswers
	case "help", "manual", "status", "cost", "budget", "cache", "debug",
		"stop", "remember", "forget":
		return fateAnswers
	}
	return ""
}

// The sentences the gate says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// homeIsTheResumeWord is /resume and /sessions on the screen that IS the
	// list of every conversation on this machine. It names the gesture rather
	// than refusing, because the thing the person asked for is already in front
	// of them.
	homeIsTheResumeWord = "this list is /resume · enter opens a row"
	// homeTypeThePathWord is a bare /attach. The browser is /folder's door and
	// this command's own is a path, so the line says the two ways a file gets
	// onto home's tray rather than opening a sheet nobody asked for.
	homeTypeThePathWord = "type the path after /attach · or drop the file here"
	// homeRidesWord is the tail of the line a file attached at home leaves: the
	// tray belongs to the person and travels into the conversation home opens
	// next (home.go's [app.homeStart] carries it there).
	homeRidesWord = " · rides with the next conversation"
	// homeFreshBehindWord is /new said where a person can read it. The command
	// replaces the conversation this window is holding behind home — which is
	// not the conversation `enter` on home is about to open — and it used to do
	// that in complete silence.
	homeFreshBehindWord = "started a fresh conversation behind home"
)

// homeSlash is what enter does with a slash line on the action row: the line is
// consumed the way chat's enter consumes it (input.go's [app.enterLine]) — the
// box is emptied — and then the fate above decides what happens. A command that
// stays on this screen therefore leaves the composer empty rather than leaving
// the person to erase what they said.
func (a *app) homeSlash(line string) tea.Cmd {
	h := &a.home
	name, rest, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	rest = strings.TrimSpace(rest)
	word := canonicalCommand(strings.ToLower(name))
	h.box.reset()
	h.build()
	switch homeFate(word, rest) {
	case fateTargetModel:
		return a.homeModelCommand(rest)

	case fateTargetFolder:
		// THE BROWSER, AIMED AT THE TARGET (folderplace.go). Bare it opens where
		// the next conversation would; with a path it opens on that path. Both
		// forms answer one question — which folder does the next conversation open
		// in — so both open the one surface that answers it, and picking a row
		// pins the rule above home's box rather than moving the conversation
		// behind the screen.
		return a.openTargetFolderPick(rest)

	case fateResume:
		h.say(homeIsTheResumeWord, "")
		return nil

	case fateTray:
		return a.homeTrayCommand(word, rest)

	case fateFresh:
		// /new IS ABOUT THE CONVERSATION BEHIND THE SCREEN, and its own road says
		// `new session · <path>` — true, and read on home as though it were about
		// the conversation `enter` is going to open. So the line under the box
		// says which conversation actually changed, and the refusals travel here
		// through the seam [app.renewRefusing] exists for.
		renewed, started := a.renewRefusing(func(text string) { h.say(text, "") })
		if started {
			h.say(homeFreshBehindWord, "")
		}
		return renewed

	case fateNeedsChat:
		// THE SAME DOOR `enter` TAKES, and it has to be: `/compact` typed at home
		// used to compact a conversation behind the screen, and `/files` opened a
		// shelf over one. Both are now about the conversation this line is
		// opening, which is the conversation the rule above the box named.
		started, opened := a.homeOpenAtTarget()
		if !opened {
			return nil
		}
		// AND THE DISPATCH RUNS AGAINST THE NEW AGENT. Both roads into
		// [app.homeOpenAtTarget] swap the agent synchronously, so `a.agent` here
		// is the conversation that just opened and [app.slash] acts on it.
		return tea.Batch(started, a.slash(line))
	}
	// AND THE ANSWER OF EVERYTHING ELSE IS ECHOED WHERE IT WAS TYPED. /help,
	// /status, /cost, /crew frugal, /budget 20 and `there is no command called
	// /x · / lists them` all answer with a note, which lands in the conversation
	// behind this screen — true, kept, and unreadable until you leave. The flag
	// puts the first line of it on home's own message line as well
	// ([app.noteWritten]).
	//
	// /quit IS ON THIS ROAD AND NEEDS NOTHING ADDED TO IT. Closing the
	// conversation behind home says `closed · <name>` through the same echo
	// (keeper.go's [app.leaveFront]), and when that one was the last conversation
	// this terminal held there is no line to write because aforge itself leaves.
	a.echoHome = true
	defer func() { a.echoHome = false }()
	return a.slash(line)
}

// homeTrayCommand is /attach <path> and /image <path> at home: the tray HOME is
// already carrying, and the line says which conversation those files are for.
//
// THE TRAY IS THE PERSON'S AND NOT THE CONVERSATION'S (attach.go's law, said
// again by home.go's [app.homeStart], which carries the chips into the
// conversation it opens). So these two commands needed no conversation to be
// opened for them — /image opened one, ran there, and left home behind for a
// picture that would have travelled anyway — and what they DID need was a
// sentence: the chip appears on a row above the box, which is easy to miss on a
// screen full of projects.
//
// A DIRECTORY AFTER /attach IS THE TARGET'S. The dispatcher hands one to
// [app.referPlace], which gives it to the conversation behind home — invisibly,
// where the person cannot read the answer. Here it is the same decision
// `/folder` makes, said in the same words.
func (a *app) homeTrayCommand(word, rest string) tea.Cmd {
	if rest == "" {
		if word == "image" {
			// The dispatcher's own usage line, said where it was typed rather than
			// in a conversation opened to hold it.
			a.echoHome = true
			defer func() { a.echoHome = false }()
			return a.slash("/image")
		}
		a.home.say(homeTypeThePathWord, "")
		return nil
	}
	// AND ONLY ON THIS MACHINE'S OWN DISK. Over a connection the directory this
	// process can stat is the laptop's and the next conversation is on the other
	// machine, so a pin taken here would name a folder it cannot open — which is
	// [folderRemoteWord]'s argument. The dispatcher's own road answers it there.
	if word == "attach" && !a.hosted() {
		if path := a.resolvePath(rest); path != "" {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				a.target.where = path
				a.home.say(targetMovedWord+a.hostedPath(shortPath(path, a.tilde, 0)), "")
				a.touch()
				return nil
			}
		}
	}
	held := len(a.chips)
	a.echoHome = true
	cmd := a.slash("/" + word + " " + rest)
	a.echoHome = false
	// A REFUSAL — no such file, not a picture, over the ceiling, already on the
	// tray — has already put its own sentence on this line through the echo, and
	// it is the truer one.
	if len(a.chips) > held {
		a.home.say(folderAttachedWord+a.chips[len(a.chips)-1].name()+homeRidesWord, "")
	}
	a.home.carrying = len(a.chips) > 0
	a.home.build()
	a.touch()
	return cmd
}

// homeModelCommand is /model at home, and it is about the DRAFT.
//
// It used to be [app.switchModel] on the conversation behind the screen, which
// re-modelled something the person was not looking at, wrote `model · <slug>`
// where they could not read it, and persisted the choice as the launch default
// — three effects from one command, none of them visible. Here it pins the
// model the next conversation will open on, and says exactly that.
func (a *app) homeModelCommand(rest string) tea.Cmd {
	// Bare is a question — "which ones are there" — and the list is the answer,
	// drawn in home's own body (homedraft.go). A slug is an instruction, and an
	// instruction that opened a list to confirm itself would be the surface
	// asking a person to say something twice.
	if rest == "" {
		a.openTargetPicker()
		return nil
	}
	// AND THE WORDS AFTER IT ARE READ FOR SHAPE, exactly as chat reads them
	// (commands.go's [modelArg]). A question opens the list with the query
	// already typed; a machine pin is about a conversation's own routing and is
	// left to the one dispatcher.
	switch intent, value := modelArg(rest); intent {
	case modelQuery:
		a.openTargetPicker()
		a.target.pick.filter.setText(value)
		a.target.pick.rank()
		a.touch()
		return nil
	case modelPinLane, modelAutoLane:
		return a.slash("/model " + rest)
	}
	// A SLUG THE CATALOG KNOWS IS CHECKED BEFORE IT IS TAKEN, on the dispatcher's
	// own argument: "openai/gpt-4o-mini-tts" is a name this surface can look up
	// and know answers in mp3, and pinning it would leave somebody starting a
	// conversation with a model that cannot hold one.
	if warning := a.nonChatWarning(rest); warning != "" {
		a.home.say(firstLine(warning), "")
		return nil
	}
	a.pinTargetModel(rest)
	return nil
}

// homeRunCommand is enter on an offered command row, and it is LITERALLY chat's
// own gesture: [chooseCommand] rewrites the token with the chosen word over
// home's box and home's list, and then the row does what it says — run bare, or
// hold the box for the words it takes. A token that is not the whole line is a
// MENTION and never a command, so "/settings is what I want" becomes a sentence
// rather than a dispatch. This used to be a hand copy of that function, which is
// how it came to be missing its clamp and to be writing [command.typed]'s
// "<slug>" placeholder into the box.
//
// THE TWO LINES CHAT DOES AFTER THE DISPATCH ARE NOT DONE HERE, and their
// absence is the difference between the surfaces rather than an omission: this
// screen has no recall list to remember a command into and no draft to drop
// (`↑` here walks the drop-up), so `remember` and `dropDraft` would be two
// no-ops standing where a person could read them as a promise ([app.runMenu]
// states the other half).
//
// The rebuild happens on every road out, because all three change the box and
// the drop-up is built from it.
func (a *app) homeRunCommand(line homeLine) tea.Cmd {
	h := &a.home
	word, run := chooseCommand(&h.box, &h.cmd, *line.cmd)
	h.build()
	if !run {
		return nil
	}
	// THE ROW GOES THROUGH HOME'S OWN GATE and not straight to the dispatcher,
	// because a command chosen off the list and the same command typed out in
	// full must do the same thing — and the gate is what makes /model open the
	// list over the target rather than an overlay nothing draws ([app.homeSlash]).
	return a.homeSlash(word)
}
