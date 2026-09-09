package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
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

// commandRowWord is what a command row says it is in its right margin, the
// way a place row says `a place` (homeplaces.go's [placeRowWord]).
const commandRowWord = "a command"

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

// homeCommandRow paints one offered command: the word a person would type,
// and in the right margin what it is and what it does — the same note the
// chat menu's row carries (commands.go's [command.note]), so the two surfaces
// teach the same sentence.
func (a *app) homeCommandRow(line homeLine, at, width int, pal palette) string {
	h := &a.home
	return overlayRow(line.cmd.typed(), commandRowWord+" · "+line.cmd.note(),
		at == h.cursor, false, at == h.hover, width, pal)
}

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
// FOUR FATES, AND EVERY ONE OF THEM IS VISIBLE:
//
//	configures the draft   /model — it acts on the target and says so on home's
//	                       own line ([homeView.say])
//	names a place          /settings /home /search /spend /standing /memory
//	                       /history /budget — unchanged, a place replaces a place
//	is about THIS screen   /resume and /folder, whose answers home already IS —
//	                       one line each, naming the gesture that does it here
//	needs a conversation   it opens one AT THE TARGET first (homedraft.go), then
//	                       runs there, which is the same door `enter` uses
//
// AND EVERYTHING ELSE STILL GOES TO THE ONE DISPATCHER, with its answer echoed
// onto home's message line by [app.noteWritten] — so /help, /status, /cost,
// /crew frugal, /budget 20 and `there is no command called /x · / lists them`
// are all read where the person is standing.

// The sentences the gate says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// homeIsTheResumeWord is /resume and /sessions on the screen that IS the
	// list of every conversation on this machine. It names the gesture rather
	// than refusing, because the thing the person asked for is already in front
	// of them.
	homeIsTheResumeWord = "this list is /resume · enter opens a row"
	// homeMoveTheTargetWord is /folder, /place and /dir, whose overlay this
	// screen cannot draw and whose question home answers two better ways.
	homeMoveTheTargetWord = "alt+w moves the next conversation · or type a path"
)

// homeSlash is what enter does with a slash line on the action row: the line is
// consumed the way chat's enter consumes it (input.go's [app.enterLine]) — the
// box is emptied — and then the gate above decides which of the four fates it
// has. A command that stays on this screen therefore leaves the composer empty
// rather than leaving the person to erase what they said.
func (a *app) homeSlash(line string) tea.Cmd {
	h := &a.home
	name, rest, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	rest = strings.TrimSpace(rest)
	word := canonicalCommand(strings.ToLower(name))
	h.box.reset()
	h.build()
	switch word {
	case "model":
		return a.homeModelCommand(rest)
	case "resume":
		h.say(homeIsTheResumeWord, "")
		return nil
	case "folder":
		// BARE, its overlay is one home cannot draw and its question is one home
		// answers two better ways. WITH A PATH it is the context browser opened
		// straight at that folder — a real gesture, and one this screen is a good
		// door onto precisely because home has a composer of its own to type it
		// into (foldercontext.go).
		if rest == "" {
			h.say(homeMoveTheTargetWord, "")
			return nil
		}
	}
	if homeNeedsConversation(word, rest) {
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
	a.echoHome = true
	defer func() { a.echoHome = false }()
	return a.slash(line)
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

// homeNeedsConversation is the third fate: a command whose whole answer is
// about a conversation, asked on a screen that is not in one.
//
// THE TEST IS THE COMMAND AND SOMETIMES ITS ARGUMENT, because two of them have
// a bare form that is a place and a worded form that is work: `/standing` is
// the standing place and `/standing <words>` raises a card in a conversation;
// `/crew` is a picker and `/crew frugal` is a note. A table that keyed on the
// name alone would send a person to the wrong one of each pair.
func homeNeedsConversation(word, rest string) bool {
	switch word {
	case "files", "permissions", "connect", "harness", "subharness",
		"copy", "select", "rewind", "compact", "export", "image", "task":
		return true
	case "attach", "crew":
		// Bare, each of these opens an overlay this screen cannot draw. With
		// words it writes a note, which the echo onto home's line now shows.
		return rest == ""
	case "standing":
		// Bare it is the standing place; with words it is a card raised in a
		// conversation, and there has to be one.
		return rest != ""
	}
	return false
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
