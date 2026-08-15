package settings

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// Live apply with debounced atomic writes (8.2.19).
//
// Two halves, and the split is the whole design:
//
//   - LIVE is what the screen shows. A toggle flips under the band on the
//     keystroke; there is no "saving…" state and no spinner, because the value
//     the user chose is the value the surface is now showing.
//   - The WRITE is deferred. Every persisted row in internal/config goes
//     through one atomic read-modify-rename of the profile's config.json, so
//     four keystrokes on one row must not be four whole-file rewrites, and two
//     rows edited in the same breath must not race each other's copy of the
//     file. Pending edits are held in a map keyed by registry key — a row
//     edited five times is one entry — and flushed together, in registry
//     order, once the deadline passes.
//
// There is no timer and no goroutine here. The deadline is compared against
// the clock on the paths that already run — the next keystroke, the next
// click, closing the sheet — and, for a host that routes messages, by the tick
// [Model.Key] arms (key.go). That is deliberate: this package cannot race the
// renderer, cannot leak a goroutine when the host drops the pane, and can be
// tested to the millisecond without a sleep anywhere in the suite.
// [Model.Close] flushes, so the durability question a debounce actually raises
// — "did the thing I just typed survive me leaving?" — is answered yes, always,
// whether or not the host routes anything.

// edit is one unwritten change: what will be written, and what the value
// column shows meanwhile.
type edit struct {
	raw   string
	shown string
}

// stage records a live edit and arms the debounce. A pinned row never reaches
// here — [Model.editable] is the gate, and the row says why.
func (m *Model) stage(setting config.Setting, raw string) {
	m.pending[setting.Key] = edit{raw: raw, shown: shownValue(setting, raw)}
	delete(m.failed, setting.Key)
	if m.debounce < 0 {
		m.Flush()
		return
	}
	// A trailing debounce: each edit moves the deadline, so a row toggled
	// eight times while somebody makes up their mind is one write and not
	// eight. The unbounded-latency risk that shape carries is closed by
	// [Model.Close], which flushes — nothing a user typed can be lost by
	// leaving, only delayed by staying.
	m.deadline = m.now().Add(m.debounce)
	m.armed = true
	m.epoch++
	m.needTick = true

	// A gate may read the value that just moved.
	m.reselect()
}

// due reports whether the pending edits have waited long enough.
func (m *Model) due() bool {
	return m.armed && len(m.pending) > 0 && !m.now().Before(m.deadline)
}

// settle flushes if the deadline has passed. Called from the paths that
// already run; it is the whole clock this package has.
func (m *Model) settle() bool {
	if !m.due() {
		return false
	}
	m.Flush()
	return true
}

// Flush writes every pending edit now, through [config.Setting.Apply] — the
// registry's own write path, which validates, refuses a pinned row, persists
// atomically, and fires the registry's Applied callback so a process holding
// its own copy of a value hears about the change. This package has no writer
// of its own and never will: a second one would be a second set of rules about
// what a valid value is.
//
// A refusal is kept against its row and rendered under it in the words the
// registry chose, and the row falls back to showing what the store actually
// holds — an edit that did not land must never keep looking like it did.
func (m *Model) Flush() {
	if len(m.pending) == 0 {
		m.armed = false
		return
	}
	changed := false
	for _, r := range m.rows {
		pending, ok := m.pending[r.setting.Key]
		if !ok {
			continue
		}
		delete(m.pending, r.setting.Key)
		if err := r.setting.Apply(pending.raw); err != nil {
			m.failed[r.setting.Key] = plainError(err)
			changed = true
			continue
		}
		delete(m.failed, r.setting.Key)
		m.wrote[r.setting.Key] = true
		changed = true
	}
	// Anything still pending names a row that has left the registry between
	// the edit and the flush. Dropping it is the only honest option: there is
	// nothing left to write it to.
	clear(m.pending)
	m.armed = false
	if changed {
		m.Refresh()
		m.touch()
	}
}

// shownValue is the display form of a value that has not been written yet.
// The registry formats what it has stored; until the write lands, this
// package has to format what the user typed, in the same shapes, so a row does
// not visibly change its mind about its own units a third of a second after
// the edit.
func shownValue(setting config.Setting, raw string) string {
	text := strings.TrimSpace(raw)
	switch setting.Kind {
	case config.SettingDollars:
		if text == "" {
			return "$0"
		}
		if strings.HasPrefix(text, "$") {
			return text
		}
		return "$" + text
	case config.SettingPercent:
		if strings.HasSuffix(text, "%") {
			return text
		}
		return text + "%"
	case config.SettingText:
		if text == "" && setting.EmptyLabel != "" {
			return setting.EmptyLabel
		}
		return text
	default:
		return text
	}
}

// plainError keeps the registry's own sentence and drops nothing. It exists so
// the render path never has to think about a nil error or a multi-line one: a
// refusal is one line under the row, in coral, in the words a person wrote.
func plainError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return text
}
