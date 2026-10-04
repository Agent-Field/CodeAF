package atlas

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Run opens the atlas in this terminal and blocks until the person leaves it
// with q or ctrl+c. The view asks for the alternate screen, so the terminal is
// handed back whole on every road out — including a panic, which Bubble Tea
// catches, restores the terminal for and reports as [tea.ErrProgramPanic] (the
// one exit every command shares turns that into the plain fault sentence).
func Run() error {
	m := New(Atlas, 0, 0)
	_, err := tea.NewProgram(m).Run()
	if err != nil {
		return fmt.Errorf("atlas: %w", err)
	}
	return nil
}
