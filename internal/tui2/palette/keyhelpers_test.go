package palette

import (
	"image"

	tea "charm.land/bubbletea/v2"
)

// Key and mouse constructors, spelled the same way the composer's and the
// settings sheet's own tests spell them, so a reader moving between packages
// does not have to learn a second dialect.

func typeRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func namedKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// keyer is what both components are, for a helper that drives either.
type keyer interface {
	Key(tea.KeyPressMsg) tea.Cmd
}

// typeText types a whole string one keypress at a time, which is the only way
// a user can produce one.
func typeText(k keyer, s string) {
	for _, r := range s {
		k.Key(typeRune(r))
	}
}

func clickAt(y int) (tea.MouseMsg, image.Point) {
	return tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: y}, image.Pt(0, y)
}

func wheelAt(button tea.MouseButton) (tea.MouseMsg, image.Point) {
	return tea.MouseWheelMsg{Button: button}, image.Pt(0, 0)
}

// drain runs a command and returns the message it produced, or nil.
func drain(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
