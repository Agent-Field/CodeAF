package homes

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
)

// The home group: 5.24's "collapsed dim group" below the task cards.
//
// It is four doors and a lid, and the lid is a row rather than a chrome header
// for one reason — 5.15 says selection is navigation and there is one cursor.
// A header that the cursor could not land on would be a control the keyboard
// cannot reach, and a header the cursor CAN land on that does nothing on enter
// would be an affordance that lies. So the lid is a row, enter toggles it, and
// the same key means "go in" on every row beneath it.
//
// Why the group is collapsed by default is 5.24's own sentence: live work keeps
// the top. Four permanent rows under a rail of running tasks would push the
// thing the person came for off the bottom of a 24-row terminal, every session,
// to save one keystroke on the rooms they visit least.

// GroupRowID is the lid's row id. It is deliberately NOT a scope id —
// [Owns] returns false for it — because entering it does not re-scope, it
// expands. A wiring that forwarded it to [Source.Scope] would get a clean
// "not mine" rather than an empty room.
const GroupRowID = "home:group"

// GroupWord is the lid's name.
const GroupWord = "homes"

// Rows are the group's rail rows, ready to append to the home scope below the
// task cards. Collapsed it is one row; expanded it is the lid plus the four
// homes, indented one step so the group reads as a group without a second
// chrome vocabulary.
//
// The returned slice is freshly allocated and the caller owns it: the rail
// takes a copy of the scope it is handed, and a source that kept reusing one
// backing array could mutate what is on screen.
func Rows(s State) []rail.Row {
	lid := rail.Row{
		ID:       GroupRowID,
		Kind:     rail.RowStep,
		Name:     GroupWord,
		Status:   s.groupStatus(),
		Composer: rail.ComposerNone,
	}
	if !s.Expanded {
		// Collapsed, the lid carries the whole group's attention: a charter
		// waiting to be stood up is not allowed to become invisible because the
		// room it lives in is shut (10.3.15 — background work with no panel
		// goes invisible, and that is the failure, not the tidiness).
		lid.Questions = s.needs()
		if lid.Questions == 0 && s.broken() > 0 {
			lid.Life = LifeFailed
		}
		return []rail.Row{lid}
	}
	rows := make([]rail.Row, 0, 1+len(All()))
	rows = append(rows, lid)
	for _, h := range All() {
		rows = append(rows, s.Row(h))
	}
	return rows
}

// Row is one home's rail row. It is exported because the narrow-width list
// rendering and the palette both want a single home's row without the group
// around it.
func (s State) Row(h Home) rail.Row {
	r := rail.Row{
		ID:        h.ScopeID(),
		Kind:      rail.RowStep,
		Depth:     1,
		Name:      h.Word(),
		Status:    h.Blurb(),
		Composer:  h.Composer(),
		Questions: s.NeedsIn(h),
	}
	if r.Questions == 0 && s.BrokenIn(h) > 0 {
		// A dead service is broken, not waiting: coral ✕, not amber ?. The two
		// are different asks and 5.16 gives them different words.
		r.Life = LifeFailed
	}
	return r
}

// groupStatus is the lid's one line. It names the rooms when nothing is asking
// and states the ask when something is, because a summary that reads the same
// in both states is a summary that is only ever decoration.
func (s State) groupStatus() string {
	if n := s.needs(); n > 0 {
		return plural(n, "room needs you", "rooms need you")
	}
	words := make([]string, 0, len(All()))
	for _, h := range All() {
		words = append(words, h.Word())
	}
	return strings.Join(words, ", ")
}

// NeedsIn counts what in one home is waiting on a human.
//
// The notebook is deliberately zero: a belief never asks. A belief the machine
// is unsure of is an `unsettled` fact, which is a QUESTION row inside the self
// room's beliefs route, and it is counted there — once, where it can be
// answered, rather than twice because two doors reach the same table.
func (s State) NeedsIn(h Home) int {
	switch h {
	case HomeSelf:
		n := 0
		for i := range s.Self.Routes {
			for j := range s.Self.Routes[i].Items {
				if s.Self.Routes[i].Items[j].Needs {
					n++
				}
			}
		}
		return n
	case HomeStanding:
		n := 0
		for i := range s.Standing.Charters {
			if s.Standing.Charters[i].State == CharterProposed {
				n++
			}
		}
		return n
	}
	return 0
}

// BrokenIn counts what in one home has stopped badly.
func (s State) BrokenIn(h Home) int {
	if h != HomeServices {
		return 0
	}
	n := 0
	for i := range s.Services.Services {
		if s.Services.Services[i].Life == LifeFailed {
			n++
		}
	}
	return n
}

func (s State) needs() int {
	n := 0
	for _, h := range All() {
		n += s.NeedsIn(h)
	}
	return n
}

func (s State) broken() int {
	n := 0
	for _, h := range All() {
		n += s.BrokenIn(h)
	}
	return n
}

// plural renders `1 room needs you` / `3 rooms need you` without a Sprintf on a
// path a rail repaints on a timer.
func plural(n int, one, many string) string {
	word := many
	if n == 1 {
		word = one
	}
	var buf [24]byte
	out := strconv.AppendInt(buf[:0], int64(n), 10)
	out = append(out, ' ')
	out = append(out, word...)
	return string(out)
}
