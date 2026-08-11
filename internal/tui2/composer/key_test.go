package composer

import "testing"

// --- send vs newline, under both key sets a real terminal can present ---

func TestSendKey_LegacyKeySet(t *testing.T) {
	// tmux, or any terminal without kitty-protocol disambiguation: only
	// alt+enter is bound, per tui2/caps.go's Capabilities.NewlineKeys() with
	// Disambiguation == false.
	var sent []string
	m := New(Options{
		OnSubmit:    func(s string) { sent = append(sent, s) },
		SendKey:     "enter",
		NewlineKeys: []string{"alt+enter"},
	})
	typeString(m, "line one")
	m.Key(altEnterKey())
	typeString(m, "line two")
	m.Key(enterKey())

	if len(sent) != 1 || sent[0] != "line one\nline two" {
		t.Fatalf("sent = %v, want one message with an embedded newline", sent)
	}
	if m.Value() != "" {
		t.Fatalf("draft not cleared after send: %q", m.Value())
	}
}

func TestSendKey_EnhancedKeySet(t *testing.T) {
	// A kitty-protocol terminal: both shift+enter and alt+enter must insert a
	// newline (10.1.2 — shift+enter is an enhancement, never the only door).
	var sent []string
	m := New(Options{
		OnSubmit:    func(s string) { sent = append(sent, s) },
		SendKey:     "enter",
		NewlineKeys: []string{"shift+enter", "alt+enter"},
	})
	typeString(m, "a")
	m.Key(shiftEnterKey())
	typeString(m, "b")
	m.Key(altEnterKey())
	typeString(m, "c")
	m.Key(enterKey())

	if len(sent) != 1 || sent[0] != "a\nb\nc" {
		t.Fatalf("sent = %v, want one message with two embedded newlines", sent)
	}
}

func TestSendKey_NeverFiresOnBlankDraft(t *testing.T) {
	var calls int
	m := New(Options{OnSubmit: func(string) { calls++ }})
	m.Key(enterKey())
	typeString(m, "   ")
	m.Key(enterKey())
	if calls != 0 {
		t.Fatalf("OnSubmit called %d times on a blank/whitespace draft, want 0", calls)
	}
}

func TestSendKey_TrimsBeforeSubmit(t *testing.T) {
	var got string
	m := New(Options{OnSubmit: func(s string) { got = s }})
	typeString(m, "  hello  ")
	m.Key(enterKey())
	if got != "hello" {
		t.Fatalf("OnSubmit got %q, want trimmed %q", got, "hello")
	}
}

// --- the esc law (8.2.21), both branches ---

func TestEsc_NonEmptyDraftStashesAndClears(t *testing.T) {
	m := New(Options{})
	typeString(m, "unsent thought")
	cmd := m.Key(escKey())
	if cmd != nil {
		t.Fatalf("esc against a non-empty draft must be fully consumed (nil cmd), got a command")
	}
	if m.Value() != "" {
		t.Fatalf("draft not cleared after esc-stash: %q", m.Value())
	}
	// Restorable: the next ↑ on the now-empty draft brings it back verbatim.
	if !m.recallOlder() {
		t.Fatalf("recallOlder() = false, want the stash to be reachable")
	}
	if m.Value() != "unsent thought" {
		t.Fatalf("restored draft = %q, want %q", m.Value(), "unsent thought")
	}
}

func TestEsc_EmptyDraftIsNotConsumed(t *testing.T) {
	m := New(Options{})
	cmd := m.Key(escKey())
	if cmd == nil {
		t.Fatalf("esc against an empty draft must not be silently swallowed — want a command carrying EscMsg")
	}
	msg := cmdMsg(cmd)
	if _, ok := msg.(EscMsg); !ok {
		t.Fatalf("esc against an empty draft produced %#v, want EscMsg", msg)
	}
}

func TestEsc_NeverDestroysTypedText(t *testing.T) {
	m := New(Options{})
	typeString(m, "do not eat this")
	m.Key(escKey())
	// Simulate the wiring interpreting the (empty-draft) esc that follows —
	// that must never happen while a stash is pending restoration, but even
	// if the composer regains focus and esc is pressed again on the now-
	// empty draft, the earlier stash must still be intact and reachable.
	m.Key(escKey())
	if !m.recallOlder() {
		t.Fatalf("stash lost after a second esc on the empty draft")
	}
	if m.Value() != "do not eat this" {
		t.Fatalf("stash corrupted: got %q", m.Value())
	}
}

func TestEsc_WhitespaceOnlyDraftIsNonEmptyAndStashed(t *testing.T) {
	// The contract's binary is NON-EMPTY (len > 0) vs EMPTY (len == 0), read
	// literally: three typed spaces are typed text, not nothing, so the law
	// takes the conservative side and stashes them rather than risk treating
	// keystrokes the user made as if they never happened. Whitespace is never
	// worth showing a person as "recalled" prose, but it is still protected
	// exactly like any other draft — the esc law does not get to decide a
	// space bar press does not count.
	m := New(Options{})
	typeString(m, "   ")
	cmd := m.Key(escKey())
	if cmd != nil {
		t.Fatalf("esc against a whitespace-only draft should be fully consumed (stashed), got a command")
	}
	if m.Value() != "" {
		t.Fatalf("draft not cleared after esc-stash: %q", m.Value())
	}
	if !m.recallOlder() || m.Value() != "   " {
		t.Fatalf("whitespace draft not restorable, got %q", m.Value())
	}
}

// --- history ring ---

func TestHistory_UpOnEmptyDraftRecallsSentLines(t *testing.T) {
	m := New(Options{OnSubmit: func(string) {}})
	typeString(m, "first")
	m.Key(enterKey())
	typeString(m, "second")
	m.Key(enterKey())

	m.Key(upKey())
	if m.Value() != "second" {
		t.Fatalf("after one ↑, Value() = %q, want %q", m.Value(), "second")
	}
	m.Key(upKey())
	if m.Value() != "first" {
		t.Fatalf("after two ↑, Value() = %q, want %q", m.Value(), "first")
	}
	// Oldest line: further ↑ stays put rather than losing the draft.
	m.Key(upKey())
	if m.Value() != "first" {
		t.Fatalf("↑ past the oldest entry changed Value() to %q", m.Value())
	}
	m.Key(downKey())
	if m.Value() != "second" {
		t.Fatalf("after ↓, Value() = %q, want %q", m.Value(), "second")
	}
	m.Key(downKey())
	if m.Value() != "" {
		t.Fatalf("↓ back to the present should clear the draft, got %q", m.Value())
	}
}

func TestHistory_TypingBreaksOutOfRecall(t *testing.T) {
	m := New(Options{OnSubmit: func(string) {}})
	typeString(m, "sent")
	m.Key(enterKey())
	m.Key(upKey()) // recall "sent"
	m.Key(charKey('!'))
	if m.Value() != "sent!" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "sent!")
	}
	// historyStep reset to 0: a further ↑ now navigates the (single-line)
	// draft rather than continuing the recall walk — no line above to move
	// to, so it is a no-op, and the edited draft must survive untouched.
	m.Key(upKey())
	if m.Value() != "sent!" {
		t.Fatalf("Value() = %q after ↑ post-edit, want unchanged %q", m.Value(), "sent!")
	}
}

func TestHistory_UpOnEmptyDraftWithNoHistoryIsNoop(t *testing.T) {
	m := New(Options{})
	cmd := m.Key(upKey())
	if cmd != nil {
		t.Fatalf("↑ with nothing to recall should produce no command")
	}
	if m.Value() != "" {
		t.Fatalf("Value() = %q, want empty", m.Value())
	}
}

func TestHistory_DuplicateConsecutiveSendsCollapse(t *testing.T) {
	m := New(Options{OnSubmit: func(string) {}})
	typeString(m, "again")
	m.Key(enterKey())
	typeString(m, "again")
	m.Key(enterKey())
	if len(m.history) != 1 {
		t.Fatalf("history = %v, want one collapsed entry", m.history)
	}
}

func TestKey_DownMovesCursorWhenDraftNonEmptyAndNotRecalling(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	typeString(m, "first")
	m.Key(altEnterKey())
	typeString(m, "second")
	m.moveHome() // cursor to column 0 of "second"
	m.Key(downKey())
	if m.Value() != "first\nsecond" {
		t.Fatalf("Value() = %q, want unchanged (down on the last line is a no-op)", m.Value())
	}
	m.cursor = 0 // column 0 of "first"
	m.Key(downKey())
	m.Key(charKey('!'))
	if m.Value() != "first\n!second" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "first\n!second")
	}
}

func TestKey_UnboundCtrlChordNeverInsertsText(t *testing.T) {
	// A real terminal never populates Key.Text for a ctrl combo (doc.go,
	// key.go: Text is only set for printable characters) — ctrlKey mirrors
	// that. An unbound chord like ctrl+j must be a harmless no-op, not a
	// stray "j" landing in the draft.
	m := New(Options{})
	m.Key(ctrlKey('j'))
	if m.Value() != "" {
		t.Fatalf("Value() = %q after an unbound ctrl chord, want empty", m.Value())
	}
}

// --- basic editing, exercised because paste/cursor tests build on it ---

func TestEditing_BackspaceAndDelete(t *testing.T) {
	m := New(Options{})
	typeString(m, "abc")
	m.Key(leftKey())
	m.Key(backspaceKey())
	if m.Value() != "ac" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "ac")
	}
	m.Key(deleteKey())
	if m.Value() != "a" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "a")
	}
}

func TestEditing_HomeEndMultiLine(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	typeString(m, "ab")
	m.Key(altEnterKey())
	typeString(m, "cd")
	m.Key(homeKey())
	m.Key(charKey('X'))
	if m.Value() != "ab\nXcd" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "ab\nXcd")
	}
	m.Key(endKey())
	m.Key(charKey('Y'))
	if m.Value() != "ab\nXcdY" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "ab\nXcdY")
	}
}

func TestEditing_UpDownMoveCursorWhenDraftNonEmpty(t *testing.T) {
	m := New(Options{NewlineKeys: []string{"alt+enter"}})
	typeString(m, "first")
	m.Key(altEnterKey())
	typeString(m, "second")
	// Cursor is at the end of "second". ↑ should move it to column 5 of
	// "first" (its own length), not trigger recall — the draft is non-empty
	// and we are not mid-recall.
	m.Key(upKey())
	m.Key(charKey('!'))
	if m.Value() != "first!\nsecond" {
		t.Fatalf("Value() = %q, want %q", m.Value(), "first!\nsecond")
	}
}
