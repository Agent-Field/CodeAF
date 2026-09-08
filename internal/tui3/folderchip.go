package tui3

// THE FOLDER INDICATOR: what this conversation is about, where a person can see
// it, and the one gesture that takes it back off.
//
// Choosing a folder used to be a sentence in the transcript and nothing more.
// That is fine for the minute after you chose it and useless an hour later: the
// line has scrolled away, and the only way to find out which folders a
// conversation had gained was to open the picker and read its first rows. So a
// conversation that is about somewhere keeps a COMPACT CELL PER FOLDER on the
// tray above the box — the row this surface already keeps for "what the next
// message carries besides its words" (attach.go) — and a click takes one off.
//
// IT IS ONE CELL AND IT IS DIM. The folders are a fact about the conversation
// rather than a thing being said, and a permanent panel listing them would be
// the dashboard docs/DESIGN-LANGUAGE.md refuses. A conversation about nowhere
// draws nothing at all, which is nearly every conversation there is and is why
// the field test below is the first thing every reader of this file runs.
//
// THE ✕ IS DRAWN ONLY WHERE SOMETHING IS BEHIND IT. Taking a folder off needs
// `RemovePlace` on the far side ([placeRemover]); a build without it still shows
// what the conversation is about — that much is true and worth knowing — and
// simply does not offer the gesture, because a capability that cannot work is
// absent, not broken.

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// glyphPlaceChip is the tray's glyph for a folder this conversation is about.
// It is a DIFFERENT glyph from the picture and the file for [fileChipMark]'s
// reason: the three do different things to a message, and a tray that drew them
// alike would leave a person wondering why their folder was never read.
const (
	glyphPlaceChip      = "▥"
	glyphPlaceChipASCII = "/"
)

// placeChipMark is that glyph, in whichever alphabet the terminal has.
func placeChipMark(pal palette) string {
	if pal.ascii || pal.linear {
		return glyphPlaceChipASCII
	}
	return glyphPlaceChip
}

// placeChipCap is how much of a folder's name one cell shows. A tray is a
// reminder of a choice already made, so the NAME is what it carries — never the
// path, which would push four folders past the width of any terminal.
const placeChipCap = 18

// referredPlaceRefs is what this conversation is about, newest first, straight
// off the one door that has them. Nil where the build has no such door, which
// is the same silence the picker refuses on.
func (a *app) referredPlaceRefs() []session.PlaceRef {
	door, ok := a.placeDoor()
	if !ok {
		return nil
	}
	return door.Places()
}

// canRemovePlace reports whether taking a folder back off can actually reach the
// conversation. See this file's header.
func (a *app) canRemovePlace() bool {
	_, ok := a.agent.(placeRemover)
	return ok
}

// placeTrayCells is one cell per folder this conversation is about, in the
// order the conversation holds them — most recently referred first, so the
// folder somebody just chose is the one nearest the left margin.
//
// THE CELLS ARE BOUNDED. A conversation may be about sixteen folders
// (session.placesRemembered) and a tray that drew sixteen would be a wall above
// the box; three is what fits beside a file chip on an ordinary terminal, and
// the rest are counted in a cell of their own — `+2 more` — which says the truth
// without claiming space it has not got. `/folder`'s own first rows are the
// whole set, on demand, and the manual says so.
func (a *app) placeTrayCells() []string {
	refs := a.referredPlaceRefs()
	if len(refs) == 0 {
		return nil
	}
	drop := ""
	if a.canRemovePlace() {
		drop = " " + glyphChipDrop
		if a.pal.ascii || a.pal.linear {
			drop = " x"
		}
	}
	mark := placeChipMark(a.pal)
	shown := min(len(refs), placeTrayCap)
	out := make([]string, 0, shown+1)
	for _, ref := range refs[:shown] {
		out = append(out, mark+" "+fit(placeChipName(ref.Path), placeChipCap)+drop)
	}
	if rest := len(refs) - shown; rest > 0 {
		out = append(out, "+"+strconv.Itoa(rest)+placeTrayMoreWord)
	}
	return out
}

// placeTrayCap is how many folders the tray names before it starts counting.
const placeTrayCap = 3

// placeTrayMoreWord is the tail of the counting cell. It is a whole word rather
// than a bare number because a lone `+2` on a row that also carries `#2 chart.png`
// is two numbers about two different things.
const placeTrayMoreWord = " more folders"

// placeChipName is what one folder is called on the tray: its own name, and the
// name of the thing above it where that would otherwise be a bare `src` or
// `internal` that names half the machine.
func placeChipName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	name := filepath.Base(path)
	if !placeChipVague[strings.ToLower(name)] {
		return name
	}
	if parent := filepath.Dir(path); parent != path && parent != "" {
		if above := filepath.Base(parent); above != "" && above != string(filepath.Separator) {
			return above + "/" + name
		}
	}
	return name
}

// placeChipVague are the directory names that name nothing on their own. A chip
// reading `src` is a chip a person has to open the picker to understand.
var placeChipVague = map[string]bool{
	"src": true, "internal": true, "lib": true, "app": true, "pkg": true,
	"cmd": true, "docs": true, "test": true, "tests": true, "tmp": true,
}

// placeTrayWidth is what those cells occupy, gaps included — the offset the
// picture chips beside them start at ([app.chipTrayTarget]).
func placeTrayWidth(cells []string) int { return harnessTrayWidth(cells) }

// dropPlaceChip takes one folder off the conversation, and reports whether the
// gesture was answered at all.
//
// THE WIRE CALL IS A COMMAND AND NOT A SYSCALL ON THE UPDATE LOOP. Removing a
// place stamps the conversation's meta and, over a connection, is a round trip
// to another machine — neither of which a click may make the frame wait for
// (askFolderKids states the same law about a readdir). The cell comes off when
// the answer arrives, because until then it has not.
func (a *app) dropPlaceChip(at int) (tea.Cmd, bool) {
	door, ok := a.agent.(placeRemover)
	if !ok {
		return nil, false
	}
	refs := a.referredPlaceRefs()
	if at < 0 || at >= len(refs) || at >= placeTrayCap {
		return nil, false
	}
	path := refs[at].Path
	return func() tea.Msg { return placeDroppedMsg{path: path, err: door.RemovePlace(path)} }, true
}

// placeDroppedMsg is that answer coming back.
type placeDroppedMsg struct {
	path string
	err  error
}

// tookPlaceDropped says what happened, in one line either way. A folder that
// came off is worth a sentence for the same reason choosing one is: the tray
// changing under a click is a change to what the next request will be told, and
// this surface says those out loud.
func (a *app) tookPlaceDropped(msg placeDroppedMsg) {
	shown := shortPath(msg.path, a.tilde, 0)
	if msg.err != nil {
		a.noteFacts(msg.err.Error())
		a.touch()
		return
	}
	if a.placeChosen == msg.path {
		a.placeChosen = ""
	}
	a.noteFacts(placeDroppedWord+shown, shown)
	a.touch()
}

// placeDroppedWord leads the line a folder taken off says. `no longer` is the
// plain half of `folder ·`, and the path is the payload in both.
const placeDroppedWord = "folder removed · "
