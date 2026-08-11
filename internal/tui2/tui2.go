// Package tui2 is the v2 chat surface: the Bubble Tea v2 program shell, the
// cell compositor every pane is placed by, the layout solver, and the seams
// the sibling packages plug into.
//
// It grows beside internal/tui rather than inside it (11.1). The old surface
// keeps working, on Bubble Tea v1, for as long as it takes this one to pass
// the parity checklist; the two module paths coexist because charmbracelet's
// v2 line moved to charm.land, so both can be linked into one binary without
// either pretending to be the other.
//
// What lives where:
//
//	compositor.go  layers, geometry, hit testing — the only place that knows
//	               where anything is
//	layout.go      a pure function from a terminal size to a table of rectangles
//	metrics.go     the responsive numbers table (seam: internal/tui2/tokens)
//	pane.go        the renderer seam (seam: internal/tui2/blocks)
//	caps.go        what the terminal can do, asked rather than assumed
//	osc.go         the terminal protocols spoken outside the frame — the bytes
//	attention.go   the interruption budget (10.5.27) — who is allowed to say them
//	shell.go       the program model (seam: internal/tui2/golden, via Frame;
//	               seam: internal/tui2/chat, via the terminal hook contract)
package tui2

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Run opens the v2 surface and blocks until it closes.
//
// Input and Output exist so the surface can be booted without a terminal —
// which is how it is checked in CI, and the reason the shell never reaches for
// os.Stdin on its own.
type RunOptions struct {
	Options

	Input  io.Reader
	Output io.Writer
}

// Run builds the program and runs it. A cancelled context closes the surface
// the same way ctrl+c does, so a parent that is shutting down does not have to
// reach through the terminal to stop it.
func Run(ctx context.Context, opts RunOptions) error {
	program := []tea.ProgramOption{}
	if ctx != nil {
		program = append(program, tea.WithContext(ctx))
	}
	if opts.Input != nil {
		program = append(program, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		program = append(program, tea.WithOutput(opts.Output))
	}
	_, err := tea.NewProgram(NewShell(opts.Options), program...).Run()
	return err
}
