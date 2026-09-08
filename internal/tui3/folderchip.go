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

// attachedPlaces are the folders THE PERSON ATTACHED, newest first, straight off
// the one door that has them.
//
// IT IS THE SAID ROWS AND ONLY THE SAID ROWS. `Places` also carries what the
// ground ladder worked out and wrote down (session.PlaceKept) — a cache of an
// answer rather than an instruction — and an indicator that drew those would be
// telling a person they had attached folders they never touched, with a `✕`
// beside each one. The picker's first rows are a RANKING and may have both;
// this row is a CLAIM about what somebody did, and may not.
func (a *app) attachedPlaces() []session.PlaceRef {
	door, ok := a.placeDoor()
	if !ok {
		return nil
	}
	refs := door.Places()
	out := make([]session.PlaceRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Arrival == session.PlaceSaid {
			out = append(out, ref)
		}
	}
	return out
}

// placeScope is THE DIRECTORY A PERSON ACTUALLY POINTED AT, where the engine's
// repository-root snap moved it — `PlaceRef.Chose` on the integrated tree — and
// "" for a folder chosen at its own root.
//
// IT IS A SEAM AND IT IS ONE LINE. The field arrives with the context lane's
// work and this lane's base does not carry it, so the whole of what the browser
// needs to change to draw it is the body of this function: `return ref.Chose`.
// It is a var for [servedSighting]'s reason — a test states a scope without the
// field existing, so what this surface DOES with one is pinned today and only
// the reading of it is outstanding.
//
// `Path` stays the key for everything else: it is what the conversation holds,
// what [app.dropPlaceChip] removes, and what work is cut from.
var placeScope = func(ref session.PlaceRef) string { return "" }

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
	refs := a.attachedPlaces()
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
		// THE CELL NAMES WHAT THE PERSON POINTED AT, not what the engine snapped
		// it to. Somebody who chose `~/repo/internal/session` and read `repo` off
		// this row would have been shown a scope they did not pick, which is the
		// one thing the wave's brief forbids by name.
		name := ref.Path
		if scope := placeScope(ref); scope != "" {
			name = scope
		}
		out = append(out, mark+" "+fit(placeChipName(name), placeChipCap)+drop)
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
	refs := a.attachedPlaces()
	if at < 0 || at >= len(refs) || at >= placeTrayCap {
		return nil, false
	}
	return a.dropPlace(refs[at].Path)
}

// dropPlace is the removal itself, and it is ONE function because there are TWO
// doors onto it that must not drift: the tray's `✕`, and the picker's own action
// row — which is what keeps taking a folder off from being a gesture you need a
// mouse for (folderpick.go's [folderPick.actionWord], and
// docs/DESIGN-LANGUAGE.md's rule that the keyboard stays first-class).
func (a *app) dropPlace(path string) (tea.Cmd, bool) {
	door, ok := a.agent.(placeRemover)
	if !ok || strings.TrimSpace(path) == "" {
		return nil, false
	}
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
	shown := tildePath(msg.path, a.tilde)
	if msg.err != nil {
		a.noteFacts(msg.err.Error())
		a.touch()
		return
	}
	if a.placeChosen == msg.path {
		a.placeChosen = ""
	}
	// AND THE SHEET, IF IT IS STILL UP, LEARNS THAT THIS ROW IS FREE AGAIN. Its
	// action row reads what the conversation holds, and a row that went on saying
	// `remove this folder` about a folder already gone would offer to do
	// something twice.
	a.markFolderHeld()
	a.noteFacts(placeDroppedWord+shown, shown)
	a.touch()
}

// placeDroppedWord leads the line a folder taken off says. `no longer` is the
// plain half of `folder ·`, and the path is the payload in both.
const placeDroppedWord = "folder removed · "
