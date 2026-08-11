package chat

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/composer"
)

// The composer seam.
//
// internal/tui2/composer owns the draft: the rune buffer, the caret, the send
// and newline chords the shell negotiated, the recall ring, bracketed paste,
// and the esc law's first half — a non-empty draft is stashed into the ring,
// never destroyed. This package owns the second half: an esc the composer did
// not consume arrives here as [composer.EscMsg], and only this side knows
// whether the room is streaming and therefore what that esc means (8.2.21).
//
// The seam is an interface and a constructor rather than a direct call so the
// app is written against a shape rather than against a package. That shape is
// exactly [tui2.Pane] + [tui2.PaneKeys] + [tui2.PaneFocus] — the shell's own
// vocabulary, which is the right one: a composer is a pane, and everything the
// app needs from it is something the shell already knows how to ask.

// composerPane is what the app binds into the composer region.
type composerPane interface {
	tui2.Pane
	tui2.PaneKeys
	tui2.PaneFocus
}

// composerOptions is the composer's own option struct, named here so the app
// reads as one package rather than two.
type composerOptions = composer.Options

// newComposer builds the composer the app binds.
func newComposer(opts composerOptions) composerPane { return composer.New(opts) }
