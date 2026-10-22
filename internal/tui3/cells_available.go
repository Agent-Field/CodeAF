package tui3

import "github.com/Agent-Field/codeaf/internal/cell"

// prepareCells removes every move-only door together. The engine's reason is
// supplied by startup; the platform and opt-in check itself is pure.
func prepareCells(opts *Options) {
	if opts.CellsUnavailable == "" {
		opts.CellsUnavailable = cell.DisabledReason()
	}
	if opts.CellsUnavailable == "" {
		return
	}
	opts.Pairing, opts.Approvals, opts.Fleet = nil, nil, nil
	opts.Machines, opts.Takeover = nil, nil
	opts.Branches = BranchActions{}
}

// sayCellsUnavailable answers an explicitly typed command or pressed chord
// without leaving a half-open card or starting an errand.
func (a *app) sayCellsUnavailable() bool {
	if a.cellsUnavailable == "" {
		return false
	}
	// This is an answer to a gesture on home too, where the transcript is
	// covered and only an explicitly echoed note is visible.
	echo := a.echoHome
	a.echoHome = true
	a.note(a.cellsUnavailable)
	a.echoHome = echo
	return true
}
