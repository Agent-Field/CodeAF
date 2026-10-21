package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/atlas"
)

// THE ARCHITECTURE MAP, INSIDE THE CONVERSATION (/atlascmd.go).
//
// `codeaf atlas` opens the map on its own terminal, the way a person opens any
// other read-only command. /atlas opens the SAME map — the same [atlas.Model],
// the same data — as a fullscreen sheet over this conversation, for the person
// who is mid-chat and wants to see how the two-computer work is put together
// without leaving the window they are in. It is one model with two keepers,
// not two pictures: a copy would be a map that drifts from itself the first
// time either side changes a box.
//
// The sheet is not a place (pages.go): it names no room in the machine, it is
// reached by one command, and esc hands the frame straight back to the
// conversation that was standing underneath — the rewind timeline's argument,
// said there for the same reason.

// atlasState is the sheet's own state: whether it is up, the map it draws and
// the draft it is holding for the person. The model is kept between opens,
// because a box a person dragged stays where they put it — a picture whose
// layout resets on every look is a picture that fights the person arranging it.
type atlasState struct {
	on bool
	// picking is the picker over the registry, drawn in the sheet's place while
	// bare /atlas stands: one row per registered map, enter opens the one the
	// cursor is on, esc or q hands the conversation straight back.
	picking bool
	cursor  int
	model   *atlas.Model
	// mapName is the map the model draws. The model is kept between opens, so a
	// box a person dragged stays where they put it — but a model kept across a
	// DIFFERENT map would show the old one's boxes, so the name rides along and
	// a changed map rebuilds.
	mapName string
	// draft and caret are the sentence the box held when the map was asked
	// for, handed back on the way out the rewind sheet's way (rewindsheet.go).
	draft []rune
	caret int
}

// runAtlas is /atlas: open the architecture map over the conversation. The
// words after the name pick the map — /atlas pairing — and bare /atlas raises
// the picker, one row per registered map, because guessing a map is guessing
// at a picture the registry already names.
func (a *app) runAtlas(rest string) tea.Cmd {
	if name := strings.TrimSpace(rest); name != "" {
		mp, ok := atlas.ByName(name)
		if !ok {
			// THE REGISTRY ANSWERS IN THE TRANSCRIPT, the way every other
			// refusal here is said: one line, what was asked and what there
			// was instead, in the registry's own words so the CLI and the chat
			// cannot drift apart about it.
			a.note(atlas.NoMap(name))
			return nil
		}
		return a.openAtlas(mp)
	}
	return a.openAtlasPicker()
}

// openAtlasPicker is bare /atlas: the list of registered maps, and enter on
// one of them opens it.
func (a *app) openAtlasPicker() tea.Cmd {
	if !a.raiseAtlas() {
		return nil
	}
	a.atlas.picking, a.atlas.cursor = true, 0
	a.touch()
	return a.wake()
}

// raiseAtlas stands up the sheet the picker and the map share, with the
// fullscreen pages stood down and the pointers' hover dropped — the same
// tidying /rewind does on its way in, because only one surface may believe it
// owns the frame and a map drawn under a place would take the keys of a
// screen nobody can see. It answers whether the sheet went up.
func (a *app) raiseAtlas() bool {
	// THE WALL OWNS THE FRAME ALREADY, and it cannot be typed under, so this
	// guard is the net under a future door rather than the road anybody takes.
	if a.atlas.on || a.wall.on || a.roomOpen() || a.railFull() {
		return false
	}
	a.standDownFullscreen()
	a.closeLists()
	a.dropHover()
	// AND THE DRAFT TRAVELS WITH IT, the rewind sheet's way: whatever the box
	// held is the person's, and a map they looked at does not take it.
	a.atlas.draft = append([]rune(nil), a.input.value...)
	a.atlas.caret = a.input.cursor
	a.atlas.on = true
	return true
}

// openAtlas raises the sheet with one map in it.
func (a *app) openAtlas(mp *atlas.Map) tea.Cmd {
	// The picker raises this same sheet, so an open here may already be up:
	// enter ON THE PICKER is the one road that arrives with the sheet
	// standing, and it opens the map in place rather than being refused by
	// the raise guard like a second /atlas would be.
	if !a.atlas.on {
		if !a.raiseAtlas() {
			return nil
		}
	}
	a.atlas.picking, a.atlas.cursor = false, 0
	if a.atlas.model == nil || a.atlas.mapName != mp.Name {
		a.atlas.model = atlas.New(mp, 0, 0)
		a.atlas.mapName = mp.Name
	}
	// THE MAP IS DRAWN AT THE TERMINAL'S SIZE, which the model has not been
	// told yet on a first open: the same read the frame body makes, said once
	// at the door so the first frame is already the right shape.
	w, h := a.size()
	a.atlas.model.Update(tea.WindowSizeMsg{Width: w, Height: h})
	a.touch()
	return a.wake()
}

// closeAtlas takes the sheet down and gives the conversation back exactly as
// it was: the draft goes back in the box, the transcript was never touched,
// and the place standing underneath it still is.
func (a *app) closeAtlas() {
	a.input.value = append(a.input.value[:0], a.atlas.draft...)
	a.input.cursor = min(a.atlas.caret, len(a.input.value))
	a.atlas.on = false
	a.atlas.picking, a.atlas.cursor = false, 0
	a.atlas.draft, a.atlas.caret = nil, 0
	a.touch()
}

// atlasKey is the sheet's keyboard: esc and q leave, everything else is the
// map's. The map's own quit keys are kept from it on purpose — q closes the
// sheet rather than the conversation, and ctrl+c still quits everything,
// because leaving is never modal (input.go).
func (a *app) atlasKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.atlas.picking {
		switch msg.String() {
		case "esc", "q":
			a.closeAtlas()
		case "up", "k":
			if a.atlas.cursor > 0 {
				a.atlas.cursor--
			}
		case "down", "j":
			if a.atlas.cursor < len(atlas.Maps)-1 {
				a.atlas.cursor++
			}
		case "enter":
			if a.atlas.cursor < len(atlas.Maps) {
				return a.openAtlas(atlas.Maps[a.atlas.cursor])
			}
		}
		return nil
	}
	switch msg.String() {
	case "esc", "q":
		a.closeAtlas()
		return nil
	}
	model, cmd := a.atlas.model.Update(msg)
	a.atlas.model = model.(*atlas.Model)
	return cmd
}

// atlasPress, atlasMoved and atlasReleased are the map's mouse, forwarded
// while the sheet is up. A press on a flow tab opens the story, a press on a
// box selects it and a drag moves it — the same map the standalone command
// draws, driven by the same messages.
func (a *app) atlasPress(msg tea.MouseClickMsg) {
	if msg.Mouse().Button != tea.MouseLeft {
		return
	}
	model, _ := a.atlas.model.Update(msg)
	a.atlas.model = model.(*atlas.Model)
}

func (a *app) atlasMoved(msg tea.MouseMotionMsg) {
	model, _ := a.atlas.model.Update(msg)
	a.atlas.model = model.(*atlas.Model)
}

func (a *app) atlasReleased(msg tea.MouseReleaseMsg) {
	model, _ := a.atlas.model.Update(msg)
	a.atlas.model = model.(*atlas.Model)
}

// atlasFrame is the sheet's whole screen: the map's frame at the terminal's
// size, and nothing of the conversation showing through at the edges. A
// sheet drawn into a viewport is a sheet you read past (view.go).
func (a *app) atlasFrame(width, height int) []string {
	if a.atlas.picking {
		return a.atlasPickerFrame(width, height)
	}
	a.atlas.model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return strings.Split(a.atlas.model.Frame(), "\n")
}

// atlasPickerFrame draws the picker in the sheet's place: one row per
// registered map, its name and its one-line description, in the registry's
// order — the same rows `codeaf atlas` draws on its own terminal (pick.go),
// said here with this surface's own mark on the row the cursor is on.
func (a *app) atlasPickerFrame(width, height int) []string {
	rows := []string{"atlas — choose a map", ""}
	for at, mp := range atlas.Maps {
		rows = append(rows, atlas.PickRow(mp, at == a.atlas.cursor))
	}
	rows = append(rows, "", "enter open · esc/q leave")
	if len(rows) > height {
		rows = rows[:height]
	}
	return rows
}

// atlasBeat is one beat of a playing flow, re-arming the clock while the
// story is still running and dropping it the moment it stops.
func (a *app) atlasBeat() tea.Cmd {
	model, cmd := a.atlas.model.Update(atlas.Beat{})
	a.atlas.model = model.(*atlas.Model)
	return cmd
}
