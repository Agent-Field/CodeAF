package consentui

import (
	"github.com/Agent-Field/aforge-v2/internal/tui2"
)

// The shell seam, pinned at compile time.
//
// [Model] is mounted with Shell.SetPane(tui2.LayerOverlay, dialog) and raised
// with Shell.SetOverlay — the z discipline, the geometry and the click routing
// are all the compositor's, and this package never learns where on screen it
// is. That is the whole of pane.go's contract, and these three lines are what
// stop a rename on either side from becoming a runtime surprise.
//
// Deliberately NOT implemented: tui2.PaneMouse. Routing a click to an answer
// would mean Render recording which row each option landed on, and a render
// that writes down what it drew is exactly the render-mutates-model habit 4.4
// rebuilt away from. Every action this dialog has is on the key strip and
// reachable from the keyboard, which is what 5.22 actually asks for — no
// typed-only actions, not a mouse target for each one.
var (
	_ tui2.Pane      = (*Model)(nil)
	_ tui2.PaneKeys  = (*Model)(nil)
	_ tui2.PaneFocus = (*Model)(nil)
)
