package composer

import tea "charm.land/bubbletea/v2"

// Test helpers building the tea.KeyPressMsg shapes the rest of tui2's tests
// already use (see shell_test.go): Text set for printable runes so
// msg.String() and msg.Key().Text behave exactly as the real input reader
// would deliver them, Code plus Mod for named/chorded keys.

func charKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func enterKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func altEnterKey() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt} }
func shiftEnterKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift} }
func escKey() tea.KeyPressMsg        { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func backspaceKey() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyBackspace} }
func deleteKey() tea.KeyPressMsg     { return tea.KeyPressMsg{Code: tea.KeyDelete} }
func leftKey() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyLeft} }
func rightKey() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyRight} }
func upKey() tea.KeyPressMsg         { return tea.KeyPressMsg{Code: tea.KeyUp} }
func downKey() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyDown} }
func homeKey() tea.KeyPressMsg       { return tea.KeyPressMsg{Code: tea.KeyHome} }
func endKey() tea.KeyPressMsg        { return tea.KeyPressMsg{Code: tea.KeyEnd} }
func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// typeString sends each rune of s through Key as an individual printable
// keystroke, the way a person actually types.
func typeString(m *Model, s string) {
	for _, r := range s {
		m.Key(charKey(r))
	}
}

// cmdMsg runs a tea.Cmd and returns the tea.Msg it produced, or nil if the
// Cmd itself was nil (nothing to run) or produced nothing.
func cmdMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}
