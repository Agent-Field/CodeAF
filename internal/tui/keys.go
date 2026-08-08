package tui

// The keyboard here holds two populations and one law between them. Chords
// (ctrl+…, alt+…) and named keys (enter, esc, the arrows) mean the same thing
// in every zone, because no text field can ever produce them. Bare printable
// keys are the other population: while a field owns the keyboard they are
// characters, and any one of them that is also an action turns the field into
// a trap. That is not hypothetical — "check the tests" begins with the key
// that cancelled the job you were about to steer, and there was no confirm and
// no undo behind it.
//
// So the rule is structural rather than repeated per binding: a bare printable
// key that operates the surface belongs to actionRunes, a key that moves a text
// cursor belongs to editingKeys, and one gate in updateKey hands both back to
// the field whenever a field has focus. A future binding inherits the rule by
// joining the table, and the table test is what stops the trap from being dug
// again.
var actionRunes = map[string]bool{
	"c": true, // cancel the inspected worker
	"v": true, // expand or collapse reading receipts
	"y": true, // copy the answer
	"Y": true, // copy the file the answer produced
	"j": true, // move the selection down
	"k": true, // move the selection up
	" ": true, // activate the selected row
	"[": true, // narrow the chat/task split
	"]": true, // widen the chat/task split
	",": true, // open settings
}

// editingKeys move the caret when there is a caret to move. Outside a field
// they are navigation — end returns the thread to now, and jumps a task's
// activity feed to its newest line.
var editingKeys = map[string]bool{
	"home": true,
	"end":  true,
}

// textEntryFocused reports whether some text field currently owns the letters:
// the composer — in the thread, or as a task's steer line — a Self drill-in's
// filter, or the settings sheet's inline editor.
func (m *Model) textEntryFocused() bool {
	switch {
	case m.inputFocused:
		return true
	case m.palette == paletteSettings && m.settingsEditing:
		return true
	case m.charterEditing:
		return true
	case m.selfVisible() && m.focus == focusSelf &&
		m.selfRoute != selfRouteRoot && m.selfFilterable():
		return true
	}
	return false
}

// commandKey answers the one question every bare-key binding used to answer for
// itself: may this key act right now, or is it a character someone is typing?
func (m *Model) commandKey(key string) bool {
	if actionRunes[key] {
		return !m.textEntryFocused()
	}
	if editingKeys[key] {
		// A caret key belongs to the caret only while there is text for it to
		// travel through. An empty field has nothing for end to do, so the
		// surface keeps "back to now" where it costs the writer nothing.
		return !m.textEntryFocused() || m.input.Value() == ""
	}
	return true
}
