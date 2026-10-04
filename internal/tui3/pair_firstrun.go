package tui3

// ── THE FIRST-RUN SCREEN'S ONE OPTIONAL FIELD: A CODE FROM ANOTHER DEVICE ───
//
// A person setting up a second computer has already done the work of setting up
// the first: what they want is their chats, and this is the one place on a fresh
// install where that can be said. The field sits under the key box on the key
// step, with the line that names the terminal command for a relay of their own,
// and it joins through the same door `/pair <code>` does ([joinWork]).
//
// IT STEALS NO KEYSTROKE, which is the first-run screen's own rule
// (firstrun.go's header). The key box keeps the keyboard from the first frame;
// the field takes it only when a person presses tab, and gives it back on tab or
// esc. A door that cannot pair draws no field at all, and an empty field changes
// nothing about the flow that was there before it.
//
// The screen stays on the step it was on when the join ends: pairing does not
// bring a model key, so the questions that were missing are still missing and
// still asked, and the one line that says the chats arrived sits under the field.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// setupCodeLabel is the field's label; the blank after it is the field.
	setupCodeLabel = "have a code from another device? "
	// setupCodeBlank is the empty field, as wide as a code with its dashes.
	setupCodeBlank = "__________"
	// setupRelayWord is the one line under the field, for a person whose other
	// computer shows a code through a relay of their own.
	setupRelayWord = "use your own relay: codeaf pair <code> --relay <url>"
)

// setupRow is one row of the block a step adds to the screen: soft rows are the
// ones a short window gives up, and caretX is where the caret stands on the row
// or -1 when it does not.
type setupRow struct {
	line   string
	soft   bool
	caretX int
}

// setupCodeRows is the field, what the join has said so far, and the relay line,
// or nothing when this connection cannot pair or the step is not the key's.
//
// THE FIELD'S ROWS ARE SOFT UNLESS IT HAS THE KEYBOARD: a window too short for
// the whole block gives up an optional field before it gives up the key box, and
// never gives it up from under a caret that is standing in it.
func (a *app) setupCodeRows(inner int) []setupRow {
	if a.pairing == nil {
		return nil
	}
	s := &a.setup
	pal := a.pal
	held := s.onCode
	rows := []setupRow{{soft: !held, caretX: -1}}
	field := pal.dim(setupCodeLabel) + pal.dim(setupCodeBlank)
	caretX := -1
	if held || s.codeText != "" {
		field = pal.dim(setupCodeLabel) + pal.ink(s.codeText)
	}
	if held {
		caretX = ansi.StringWidth(setupCodeLabel + s.codeText)
	}
	rows = append(rows, setupRow{line: field, soft: !held, caretX: caretX})
	if line, dress := a.setupPairStatus(); line != "" {
		for _, wrapped := range wrap(line, inner) {
			rows = append(rows, setupRow{line: dress(wrapped), caretX: -1})
		}
	}
	return append(rows, setupRow{line: pal.dim(setupRelayWord), soft: !held, caretX: -1})
}

// setupPairStatus is what the join has said, and how to dress it: news when it
// worked, trouble when it did not.
func (a *app) setupPairStatus() (string, func(string) string) {
	p := &a.pair
	switch {
	case p.run == nil:
		return "", nil
	case p.done && p.ok:
		return p.result, a.pal.ink
	case p.done:
		return p.result, a.pal.accent
	case p.waiting != "":
		return p.waiting, a.pal.dim
	}
	return "", nil
}

// setupJoining says a join from this screen is in flight.
func (a *app) setupJoining() bool { return a.pair.run != nil && !a.pair.done }

// setupCodeKeysWord is the foot when the field matters, and empty when the
// key step's own foot should stand.
func (a *app) setupCodeKeysWord() string {
	switch {
	case a.pairing == nil:
		return ""
	case a.setupJoining():
		return "esc stop"
	case a.setup.onCode:
		return "enter joins · tab back to the key · esc back"
	}
	return ""
}

// setupTabHint adds the one key that reaches the field to the key step's foot,
// which is the only place a person can learn it, and adds nothing on a
// connection with no field.
func (a *app) setupTabHint(foot string) string {
	if a.pairing == nil {
		return foot
	}
	return foot + " · tab types a code from another device"
}

// setupCodeKey is the field's claim on the keyboard, and it is nothing until
// tab: it reports whether it took the key, and takes none while the key box
// has the keyboard, so a key typed on the first frame lands where it always did.
func (a *app) setupCodeKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	s := &a.setup
	if a.pairing == nil || s.step() != setupKey {
		return nil, false
	}
	name := msg.String()
	switch {
	case a.setupJoining():
		return a.setupJoinKey(name), true
	case name == "tab":
		s.onCode = !s.onCode
	case !s.onCode:
		return nil, false
	case name == "enter":
		return a.setupJoin(), true
	case name == "esc":
		s.onCode = false
	case name == "backspace":
		if runes := []rune(s.codeText); len(runes) > 0 {
			s.codeText = string(runes[:len(runes)-1])
		}
	case name == "ctrl+u":
		s.codeText = ""
	default:
		s.codeText += msg.Key().Text
	}
	a.touch()
	return nil, true
}

// setupJoinKey is the keyboard while a join is out: esc stops it, and nothing
// else means anything.
func (a *app) setupJoinKey(name string) tea.Cmd {
	if name == "esc" {
		a.pair.close()
		a.touch()
	}
	return nil
}

// setupJoin is enter in the field. An empty field hands the keyboard back to
// the key box, and a code is joined through the same door as /pair <code>.
func (a *app) setupJoin() tea.Cmd {
	s := &a.setup
	typed := strings.TrimSpace(s.codeText)
	if typed == "" {
		s.onCode = false
		a.touch()
		return nil
	}
	s.codeText, s.onCode = "", false
	cmd := a.startPair(pairJoiningWord, joinWork(a.pairing, typed, false))
	// The panel stays shut: this join reports on the first-run screen.
	a.pair.open = false
	return cmd
}

// cancelSetupPair puts away a join this screen started, and leaves a /pair
// panel that is open alone.
func (a *app) cancelSetupPair() {
	if !a.pair.open {
		a.pair.close()
	}
}
