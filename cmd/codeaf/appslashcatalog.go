package main

// ── codeaf app: THE SLASH CATALOGUE'S PROGRAM ROWS ──────────────────────────
//
// slash.catalog (internal/appwire/slash_catalog.go) serves the terminal's one
// command table itself; what only this engine knows is which programs the
// running build carries. They are internal/delegate/builtin's list, folded by the
// same function the terminal's rows are (session.DelegateRowsFor), so a program
// is a command on the desktop exactly when it is one in the chat. Nothing here
// reads a file, a conversation or a model: the list is code in this binary.

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The app's backend serves slash.catalog.
var _ appwire.SlashCataloger = (*appBackend)(nil)

// SlashPrograms is the programs this build carries, by name.
func (b *appBackend) SlashPrograms() []appwire.SlashProgram {
	rows := session.DelegateRowsFor(builtin.All())
	out := make([]appwire.SlashProgram, 0, len(rows))
	for _, r := range rows {
		out = append(out, appwire.SlashProgram{
			Name: r.Name, Description: r.Description, Args: r.Args, BriefOptional: r.BriefOptional, Lands: r.Lands,
		})
	}
	return out
}
