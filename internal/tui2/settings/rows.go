package settings

import (
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/registry"
)

// The row list the sheet is actually showing, and the three questions asked of
// every row it shows: what does it read, where did that reading come from, and
// is it visible at all.

// gate is a condition-gated row's predicate (8.2.19: "condition-gated rows
// appearing the moment their parent flips"). It is handed a reader over the
// LIVE values — pending edits included — so a row appears on the keystroke
// that flipped its parent, not on the write that follows a third of a second
// later.
type gate func(value func(key string) (string, bool)) bool

// gates is the dependency table, keyed by the registry key of the DEPENDENT
// row. It is empty, and that is a finding rather than an omission: no row in
// internal/config depends on another one today — every row there resolves from
// the environment, the profile file, or a built-in default, and none of them
// reads a sibling. The mechanism is here because the list, the search, the
// selection clamp and the scroll window all have to be written as if the row
// set can change; discovering that at the first dependent row would mean
// rewriting all four.
var gates = map[string]gate{}

// rebuild recomputes which rows the sheet is showing. Everything that can
// change the answer — the query, the tab, a value a gate reads, a write
// landing — goes through here, and Render never computes it, because a pane's
// Render is a pure function of its state (pane.go's contract).
func (m *Model) rebuild() {
	m.visible = m.visible[:0]
	m.hits = m.hits[:0]
	if m.query != "" {
		// The highlight offsets are kept here, once per query, rather than
		// recomputed per row per frame: a fuzzy match on every keystroke is
		// cheap, and a fuzzy match per row per frame is a search running at
		// the frame rate.
		for _, hit := range m.search(m.query) {
			m.visible = append(m.visible, hit.row)
			m.hits = append(m.hits, hit.hits)
		}
		return
	}
	// One page, not seven tabs. The groups are shown by position and by one
	// faint lowercase word each (15) — a tab bar is a header naming structure,
	// and it hid four fifths of the sheet behind a keystroke nobody knew about.
	for index, r := range m.rows {
		if !m.shown(r) {
			continue
		}
		m.visible = append(m.visible, index)
		m.hits = append(m.hits, nil)
	}
}

// reselect rebuilds the list and keeps the band on the row it was already on
// when that row is still there. This is what makes a gate flipping, or a write
// landing, not throw the user's place away.
func (m *Model) reselect() {
	previous := ""
	if r, ok := m.current(); ok {
		previous = r.setting.Key
	}
	m.rebuild()
	if previous != "" {
		for position, index := range m.visible {
			if m.rows[index].setting.Key == previous {
				m.selected = position
				return
			}
		}
	}
	m.clamp()
}

// reselectFresh rebuilds without trying to keep the previous row under the
// band. A search that kept the old selection would put the band on whichever
// result happened to match the row you were already on, which is not what the
// user asked for by typing.
func (m *Model) reselectFresh() {
	m.rebuild()
	m.clamp()
}

func (m *Model) clamp() {
	if len(m.visible) == 0 {
		m.selected = 0
		return
	}
	m.selected = max(0, min(m.selected, len(m.visible)-1))
}

// shown answers the gate question for one row.
func (m *Model) shown(r row) bool {
	g, gated := gates[r.setting.Key]
	if !gated {
		return true
	}
	return g(m.liveValue)
}

func (m *Model) current() (row, bool) {
	if m.selected < 0 || m.selected >= len(m.visible) {
		return row{}, false
	}
	return m.rows[m.visible[m.selected]], true
}

// liveValue reads a row's value the way the screen shows it: a pending edit
// first, then the registry. Gates read through here so a dependent row appears
// while the parent's write is still in the debounce window.
func (m *Model) liveValue(key string) (string, bool) {
	if pending, ok := m.pending[key]; ok {
		return pending.raw, true
	}
	for _, r := range m.rows {
		if r.setting.Key == key {
			return r.setting.Value(), true
		}
	}
	return "", false
}

// value is what the value column shows: the pending edit if this row has one,
// otherwise the registry's own formatted reading. A pending edit carries its
// own display form because the registry only formats what it has already
// stored — an unwritten "25" would otherwise render without its dollar sign
// for as long as the debounce runs.
func (m *Model) value(r row) string {
	if pending, ok := m.pending[r.setting.Key]; ok {
		return pending.shown
	}
	return r.setting.Value()
}

// editable reports whether enter does anything on this row. An environment pin
// is the only thing that takes a row away from the user, and it says so.
func (m *Model) editable(r row) bool {
	if _, pinned := r.setting.PinnedBy(); pinned {
		return false
	}
	return true
}

// source is where a row's current reading came from.
type source uint8

const (
	// sourceDefault is a built-in default: nothing is written down anywhere.
	sourceDefault source = iota
	// sourceSaved is written down in this profile.
	sourceSaved
	// sourcePinned is held by an environment variable, which also makes the
	// row read-only (5.20: the surface never fights the shell it was launched
	// from).
	sourcePinned
	// sourceUnknown is a row whose store this surface cannot inspect — the
	// model slots and the chat divider live beside the graph. It renders as
	// the missing-data glyph, never as a guess (10.2.8).
	sourceUnknown
)

// provenance answers "where did this value come from" for one row.
func (m *Model) provenance(r row) (source, string) {
	if name, pinned := r.setting.PinnedBy(); pinned {
		return sourcePinned, name
	}
	if m.wrote[r.setting.Key] || m.persisted[r.setting.Key] {
		return sourceSaved, "saved"
	}
	if r.setting.PrefsField != "" {
		return sourceUnknown, ""
	}
	return sourceDefault, "default"
}

// kindChips is the action strip for one row: what the keyboard does here, as
// verb·key chips (5.22 rule 1 — the affordance appears at the point of
// attention; registry.Chip — the verb comes first and the key annotates it).
func kindChips(setting config.Setting) []registry.Chip {
	switch setting.Kind {
	case config.SettingBool:
		return []registry.Chip{registry.ChipFor("toggle", "space")}
	case config.SettingChoice:
		return []registry.Chip{
			registry.ChipFor("cycle", "space"),
			registry.ChipFor("pick", "enter"),
		}
	case config.SettingModel:
		return []registry.Chip{registry.ChipFor("choose model", "enter")}
	default:
		return []registry.Chip{registry.ChipFor("edit", "enter")}
	}
}
