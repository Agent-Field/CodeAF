// Package palette renders the two discoverability surfaces 5.22 names, as
// panes the shell raises on its overlay plane: the ctrl+k command palette
// (rule 2) and the `?` capability overlay (rule 4, which is also 5.20 rule 3's
// capability-honesty surface).
//
// Both are ONE list over ONE catalog. That is the whole point of 5.22: six
// surfaces render FROM the registry so discoverability holds by construction
// and can never drift. A second list renderer here — one for the palette, one
// for `?` — would be the drift arriving from inside the fix, so the two
// components differ only in what they put in the catalog and what they draw
// above it. Everything below the header line is [list], shared verbatim.
//
// What this package does NOT do, deliberately:
//
//   - It never executes anything. A chosen row yields a typed [Result] —
//     [JumpToRoom], [RunEntry], [OpenSetting] — and the wiring interprets it.
//     A palette that could run a command would need the command authority, the
//     journal and the head belt behind it, and then "the palette" would be the
//     application.
//   - It never reads the store, the config or the rail model. Data arrives as
//     a [Catalog] the wiring fills, once, when the facts move. Filtering a
//     catalog is a per-keystroke path and must not be able to touch a database.
//   - It never decides that it is open. [tui2.Shell.SetOverlay] raises the
//     plane and the shell routes the keys; esc here means "close me", said
//     through [Options.OnClose].
//
// The teaching duty is the reason every row carries three columns and not two.
// A row is `verb · one-line description · accelerator`, right-aligned — the
// macOS Help-menu-search idiom 5.22 rule 2 cites. A user who reaches an action
// through the palette has, by the time they release enter, also read the key
// that would have got them there directly. The accelerator column therefore
// outranks the description under width pressure (see [list.renderRow]): a
// palette that drops the thing it exists to teach has become a menu.
//
// Capability honesty (5.20 rule 3) is the same list with the availability
// column filled in. A disabled action is NOT hidden — hiding it answers "can
// it do that?" with silence, which is the exact failure the rule names. It
// renders in the dimmest tier with its reason in place of its description
// ("settled — ask aforge"), it advertises no accelerator, and enter on it does
// nothing. Dim is honest here and only here: 5.22's checklist forbids an
// INTERACTIVE control from living permanently in the dimmest tier, and a
// disabled row is the one row that is not interactive.
package palette
