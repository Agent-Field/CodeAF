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
// that runs them. The ranking, the token-finding and the sealing are not
// reimplemented — they are the chat composer's own [menu], synced against
// home's box, so the two surfaces cannot grow two answers to what "/mo"
// offers.
//
// THE DISPATCH IS NOT HERE EITHER. [app.homeSlash] hands the line to
// [app.slash] — the same switch chat's enter runs — so a command behaves on
// home exactly as it does in chat, and the conversation-scoped ones act on the
// conversation this window holds behind the screen, which is parity rather
// than a limitation: the window always holds one.

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

// homeSlash is what enter does with a slash line on the action row: the line
// is consumed the way chat's enter consumes it (input.go's [app.enterLine]) —
// the box is emptied, and the words go to the one dispatcher. A command that
// stays on this screen (/home, an unknown word) therefore leaves the composer
// empty rather than leaving the person to erase what they said.
func (a *app) homeSlash(line string) tea.Cmd {
	h := &a.home
	h.box.reset()
	h.build()
	return a.slash(line)
}

// homeRunCommand is enter on an offered command row, and it is [app.runMenu]'s
// mechanics over home's box: the token is rewritten with the chosen word, and
// then the row does what it says — run bare, or hold the box for the words it
// takes. A token that is not the whole line is a MENTION and never a command:
// the row rewrites the word and seals the list, exactly as chat's list does, so
// "/settings is what I want" becomes a sentence rather than a dispatch.
func (a *app) homeRunCommand(line homeLine) tea.Cmd {
	h := &a.home
	chosen := line.cmd
	at := h.cmd.at
	end := tokenEnd(h.box.value, at)
	word := chosen.typed()
	if at != 0 || strings.TrimSpace(string(h.box.value[end:])) != "" {
		head := append([]rune(nil), h.box.value[:at]...)
		tail := append([]rune(nil), h.box.value[end:]...)
		h.box.value = append(append(head, []rune(word)...), tail...)
		h.box.cursor = at + len([]rune(word))
		h.cmd.dismiss(at)
		h.build()
		return nil
	}
	h.cmd.close()
	if chosen.args != "" {
		h.box.setText(word + " ")
		h.build()
		return nil
	}
	h.box.reset()
	h.build()
	return a.slash(word)
}
