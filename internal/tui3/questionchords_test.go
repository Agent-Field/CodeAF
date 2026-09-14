package tui3

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE QUESTION'S OWN ANSWER BOX TAKES THE STANDARD CHORDS ─────────────────
//
// THE DEFECT (#1013). The `something else…` row is a box — the one place on a
// question a person types an answer of their own — and [app.questionOtherKey]
// routed its keys by hand, binding only a short list. So `cmd+←`/`cmd+→` never
// reached the ends of the line, `cmd+delete` never killed to the line's start,
// and `opt+delete`/`ctrl+delete` never deleted a word: the chords worked in the
// message box under the answer and were silently dead IN the answer. That is the
// exact defect editkeys.go was written for, said about home, the errand pane and
// the filters — a box that learned its own key map and forgot the shared one.
//
// THE FIX IS THE SHARED VOCABULARY (editkeys.go), not a second copy of it: the
// motions go through [editorMotion] and the word kill through [editorWordKill],
// and kill-to-start is read beside them under the two names the message box
// already binds (`ctrl+u`, `super+backspace`). `ctrl+w` is deliberately still
// NOT the word kill here — it shuts the tab in front everywhere on this surface
// (tabclosekey.go), the same bargain the message box makes — so the word kill on
// this box is spelled by the two names a hand actually presses.

// answerBoxLab is one question with the pointer standing on the
// `something else…` row, so that row is a box — settled, aimed at nothing, and
// with an empty composer under it, which is the law every letter here is held to
// ([app.questionKeyTaken]).
func answerBoxLab(t *testing.T) *questionLab {
	t.Helper()
	lab := newQuestionLab(t)
	lab.raise(session.Question{
		ID: 9201, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker: session.Asker{Kind: session.AskerModel}, Head: "which store should it use?",
		Reason: "two fit", Stakes: session.StakesReversible,
		Options: []session.AnswerOption{{Key: "1", Label: "sqlite"}, {Key: "2", Label: "postgres"}},
	})
	lab.tick(questionSettle * 2)
	lab.rows()
	// MOVED ONTO THE ROW DIRECTLY. Which key walks the pointer here is the
	// pointer's business and has its own tests; this file is about what the box
	// the row becomes does with a chord once somebody is standing on it.
	open := lab.a.questionHeld(lab.a.questions[0].token())
	open.pick = questionOtherAt(open.question)
	lab.a.touch()
	return lab
}

// box is the answer box the pointer is on.
func (l *questionLab) box() *editor {
	open := l.a.questionHeld(l.a.questions[0].token())
	if open == nil {
		l.t.Fatal("the question is not open any more")
	}
	return &open.other.words
}

// chord drives one key through the block's own router and reports whether the
// block took it — the same door a real keystroke goes through, command and all.
func (l *questionLab) chord(msg tea.KeyPressMsg) bool {
	cmd, taken := l.a.questionKey(msg)
	l.spend(cmd)
	return taken
}

// leftOf is what stands behind the caret.
func leftOf(e *editor) string { return string(e.value[:e.cursor]) }

// rightOf is what stands in front of the caret.
func rightOf(e *editor) string { return string(e.value[e.cursor:]) }

func TestTheQuestionAnswerBoxMovesTheCaretToTheEndsOfTheLine(t *testing.T) {
	lab := answerBoxLab(t)
	const sentence = "keep the sensors optional"

	// THE WORD BACK, under every name a terminal spells it with — the same three
	// editkeys.go hands every other box, and the same three the message box
	// reads. Before the fix only `alt+left` reached this box at all.
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"alt+left", arrowChord(tea.KeyLeft, tea.ModAlt)},
		{"alt+b", macKey('b', tea.ModAlt)},
		{"ctrl+left", arrowChord(tea.KeyLeft, tea.ModCtrl)},
	} {
		e := lab.box()
		e.setText(sentence)
		if !lab.chord(tc.msg) {
			t.Fatalf("%s was not taken by the question's own answer box", tc.name)
		}
		if got := leftOf(e); got != "keep the sensors " {
			t.Fatalf("%s left %q behind the caret, want %q", tc.name, got, "keep the sensors ")
		}
	}

	// THE LINE START, under `cmd+←`'s two real spellings. super+ is what the
	// kitty protocol reports and meta+ the CSI form — the same pair the message
	// box binds, and the pair that was dead here.
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"super+left", arrowChord(tea.KeyLeft, tea.ModSuper)},
		{"meta+left", arrowChord(tea.KeyLeft, tea.ModMeta)},
	} {
		e := lab.box()
		e.setText(sentence)
		if !lab.chord(tc.msg) {
			t.Fatalf("%s did not reach the line start in the answer box", tc.name)
		}
		if e.cursor != 0 {
			t.Fatalf("%s left the caret at %d, want the line start", tc.name, e.cursor)
		}
	}

	// THE WORD FORWARD, and then the line end.
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"alt+right", arrowChord(tea.KeyRight, tea.ModAlt)},
		{"ctrl+right", arrowChord(tea.KeyRight, tea.ModCtrl)},
	} {
		e := lab.box()
		e.setText(sentence)
		e.cursor = len("keep ")
		if !lab.chord(tc.msg) {
			t.Fatalf("%s was not taken by the answer box", tc.name)
		}
		if got := rightOf(e); got != " sensors optional" {
			t.Fatalf("%s left %q in front of the caret, want %q", tc.name, got, " sensors optional")
		}
	}
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"super+right", arrowChord(tea.KeyRight, tea.ModSuper)},
		{"meta+right", arrowChord(tea.KeyRight, tea.ModMeta)},
	} {
		e := lab.box()
		e.setText(sentence)
		e.cursor = 0
		if !lab.chord(tc.msg) {
			t.Fatalf("%s did not reach the line end in the answer box", tc.name)
		}
		if e.cursor != len(e.value) {
			t.Fatalf("%s left the caret at %d, want the line end", tc.name, e.cursor)
		}
	}
}

func TestTheQuestionAnswerBoxKillsToTheStartOfTheLineAndByTheWord(t *testing.T) {
	lab := answerBoxLab(t)

	// KILL TO THE LINE'S START, under `cmd+delete`'s two names (`ctrl+u` and
	// `super+backspace`), the same pair the message box binds (input.go).
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"ctrl+u", key("ctrl+u")},
		{"super+backspace", key("super+backspace")},
	} {
		e := lab.box()
		e.setText("keep the sensors optional")
		if !lab.chord(tc.msg) {
			t.Fatalf("%s did not kill to the start of the line in the answer box", tc.name)
		}
		if e.String() != "" {
			t.Fatalf("%s left %q, want an empty line", tc.name, e.String())
		}
		if e.cursor != 0 {
			t.Fatalf("%s left the caret at %d, want the line start", tc.name, e.cursor)
		}
	}

	// IT IS THE LINE AND NOT THE DRAFT, which is the whole distinction
	// [editor.killToStart] draws and the same promise the message box makes.
	e := lab.box()
	e.setText("keep the sensors\noptional extras")
	if !lab.chord(key("ctrl+u")) {
		t.Fatal("ctrl+u was not taken on the second line")
	}
	if e.String() != "keep the sensors\n" {
		t.Fatalf("ctrl+u killed across the newline, leaving %q", e.String())
	}

	// THE WORD BEHIND THE CARET, under the two names that reach this box —
	// `alt+backspace` (⌥⌫, which both macOS and every GTK/Qt field agree on) and
	// `ctrl+backspace`.
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"alt+backspace", key("alt+backspace")},
		{"ctrl+backspace", key("ctrl+backspace")},
	} {
		e := lab.box()
		e.setText("keep the sensors optional")
		if !lab.chord(tc.msg) {
			t.Fatalf("%s did not delete a word in the answer box", tc.name)
		}
		if got := e.String(); got != "keep the sensors " {
			t.Fatalf("%s left %q, want %q", tc.name, got, "keep the sensors ")
		}
	}

	// AND `ctrl+w` IS STILL NOT THE WORD KILL, because it shuts the tab in front
	// everywhere on this surface (tabclosekey.go) — the same bargain the message
	// box makes, kept here so a question's answer box cannot steal a global
	// chord. The box hands it back rather than eating it.
	e = lab.box()
	e.setText("keep the sensors optional")
	if lab.chord(key("ctrl+w")) {
		t.Fatal("the answer box claimed ctrl+w, which belongs to the tab strip")
	}
	if got := e.String(); got != "keep the sensors optional" {
		t.Fatalf("ctrl+w edited the answer box: %q", got)
	}
}
