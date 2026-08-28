package tui3

// ── ONE WORD-AND-LINE VOCABULARY, FOR EVERY BOX ON THIS SURFACE ─────────────
//
// THE DEFECT THIS FILE EXISTS FOR. `option+←` and `cmd+←` "did not work", and
// the reason was not the terminal: iTerm2's own Natural Text Editing mappings
// send `esc b` for ⌥← and the byte 0x01 for ⌘← — which arrive here as `alt+b`
// and `ctrl+a`, both of them bound in the message box (input.go) and neither
// of them bound anywhere else. So the jumps worked in the conversation and
// were silently dead in home's box, the errand pane, the settings filter, the
// task filter, the rewind search, and every filterable overlay. A person who
// meets the surface on home meets the dead one first.
//
// THE FIX IS ONE VOCABULARY RATHER THAN SEVEN COPIES OF IT. The [editor] type
// already carries the motions — wordLeft, wordRight, home, end — in one place;
// what was scattered was the KEY MAP, hand-rolled per box, and a hand-rolled
// map is a map that forgets. This is the ONE SOURCE OF TRUTH for which
// keystrokes move a caret by a word or to a line's end, and every box on the
// surface asks it before it reads its own switch.
//
// WHAT IS DELIBERATELY NOT HERE. `home`, `end`, `left`, `right` and `ctrl+e`
// are spent on something else on at least one surface — `home`/`end` walk the
// LIST on the settings, task and rewind sheets, the arrows are navigation over
// an empty box in the conversation, and `ctrl+e` sets a row aside on home. A
// shared map that claimed them would quietly take a working key off a screen,
// which is the opposite of the defect it is fixing. Those stay with the box
// that owns them, and each one is read AFTER this.

// editorMotion moves e's caret if key is one of the word-and-line jumps, and
// reports whether it took the key. It never changes the text, so a caller with
// a filtered list behind the box does not have to re-rank after it.
//
// EVERY CHORD IS HERE UNDER EVERY NAME A TERMINAL SENDS IT BY, because the
// name is the terminal's choice and not the person's:
//
//	alt+left / alt+b / ctrl+left     a word back
//	alt+right / alt+f / ctrl+right   a word forward
//	super+left / meta+left / ctrl+a  the start of the line
//	super+right / meta+right         the end of the line
//
// `alt+left` is what ⌥← is on a terminal that keeps option a modifier; `alt+b`
// is the same gesture from a profile that sends `esc b` instead — iTerm2's
// Natural Text Editing preset does, and it is readline's own word-back besides;
// `ctrl+left` is Windows' and Linux's. `super+` and `meta+` are ONE key under
// two names for the reason input.go's own case says: a modified arrow and a
// modified letter travel by different roads, `CSI 1;9D` reads as `meta` and
// `CSI 127;9u` reads as `super`. `ctrl+a` is here rather than with `home`
// because it is the one line-start spelling no list on this surface spends on
// anything else — and because it is the byte ⌘← actually sends on the most
// common Mac profile there is.
func editorMotion(e *editor, key string) bool {
	switch key {
	case "alt+left", "alt+b", "ctrl+left":
		e.wordLeft()
	case "alt+right", "alt+f", "ctrl+right":
		e.wordRight()
	case "super+left", "meta+left", "ctrl+a":
		e.home()
	case "super+right", "meta+right":
		e.end()
	default:
		return false
	}
	return true
}

// editorWordKill deletes the word behind e's caret if key is one of the word
// kill's names, and reports whether it took the key. It DOES change the text,
// so a caller with a list behind the box re-ranks after it.
//
// `ctrl+w` is left with each box rather than claimed here, because every box
// already binds it and each one has its own thing to do afterwards; what was
// missing everywhere but the message box is the other two names — the ones a
// Mac hand and a Windows hand actually press. ⌥⌫ arrives as `alt+backspace`
// under iTerm2's preset (it sends `esc del`) and as itself everywhere option is
// a modifier; `ctrl+backspace` is what the kitty-protocol terminals send.
func editorWordKill(e *editor, key string) bool {
	switch key {
	case "alt+backspace", "ctrl+backspace":
		e.deleteWord()
		return true
	}
	return false
}
