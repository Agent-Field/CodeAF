package composer

import "github.com/Agent-Field/aforge-v2/internal/tui2"

// Compile-time proof that Model keeps the contract the shell (internal/tui2)
// and the sibling wiring (internal/tui2/chat) build against: Pane, PaneKeys
// and PaneFocus from tui2/pane.go. A signature drift on either side fails
// this package's build instead of surfacing as a silent interface miss at
// SetPane time.
var (
	_ tui2.Pane      = (*Model)(nil)
	_ tui2.PaneKeys  = (*Model)(nil)
	_ tui2.PaneFocus = (*Model)(nil)
)
