// Package settings is the full-screen settings surface of chat-rebuild
// 8.2.19 — its grammar, our skin (7.1).
//
// The grammar, restated as the things this package actually does:
//
//	one page, four words the sheet is one scrolling list. Each group of rows is
//	                     announced by one faint lowercase word — 15's single
//	                     allowance — and separated by a blank line; the words are
//	                     [config.SettingCategories] and nothing is invented here
//	                     or hidden here. There is no tab bar: it was a header
//	                     naming structure, and it kept four fifths of the sheet
//	                     behind a keystroke most readers never found
//	type to search       ANY printable character starts a global fuzzy search
//	                     across the whole sheet. Results are navigation: the row
//	                     list becomes the matches, each carrying its own group
//	                     word, and the group headings stand down
//	arrows navigate      ↑↓ move the band, ←→ jump between group heads. Letters
//	                     cannot navigate, because letters search — that is the
//	                     price of type-to-search and it is on the hint line
//	enter edits          per row kind: a bool toggles, a choice cycles or
//	                     opens an inline picker, a number and a string open an
//	                     inline editor, a model row opens the models door
//	verb·key chips       every action word this surface draws is a
//	                     [registry.Chip]: the VERB first at the brighter of the
//	                     two dim tiers, the key after it one tier down —
//	                     `close esc`, never `esc close`. The hint line at the
//	                     foot and the action strip under the band are the two
//	                     places it lands, and the rule was found here: `esc
//	                     close` is two greys with nothing saying which word is
//	                     the label and which is the key
//	live apply           the value on screen changes at the keystroke; the
//	                     WRITE is debounced and lands through [config.Setting.Apply]
//	                     — the registry's own atomic write path, never a second
//	                     writer
//	provenance           every row says where its value came from: a built-in
//	                     default, something saved in this profile, or an
//	                     environment pin — which also renders the row read-only
//	                     with the variable named (5.20)
//	receipts             a row may carry one dim fact beside its value —
//	                     today's spend beside the day's ceiling, a price tier
//	                     beside a model (13). It comes from the registry
//	                     ([config.Setting.Receipt]) and never from a read this
//	                     package makes, and it is the first thing dropped on a
//	                     frame too narrow for all three columns
//
//	derivation           NOTHING here is a list. The rows are the registry's,
//	                     the groups are the registry's categories, and the
//	                     models group is one row per role [store.ModelRoles]
//	                     actually has. derivation_test.go fails the build if a
//	                     drawn row has no registry behind it, and internal/
//	                     config's fails if a registry row has no reader behind
//	                     it — a row cannot exist without both ends
//
// # The skin (5.16, 5.13, 5.17, 10.6)
//
// Grey tiers carry the hierarchy: a label and a value are primary, a hint is
// secondary, provenance and telemetry are tertiary. The five hues keep their
// one meaning each — coral for a refused write, amber for a row the
// environment has taken away from the user, and nothing else is coloured.
// Selection is a background band, not a foreground colour. There are no boxes:
// no border, no frame, no box inside a box. Two themes only, and no theme
// gallery — 10.6's refusal is a refusal, and this surface is where a theme
// gallery would otherwise arrive.
//
// # Live preview (8.2.19)
//
// A row that changes how the surface renders shows one sample line in its own
// hint area, so the user SEES the answer rather than reading about it. The
// table is [previews], keyed by registry key: today `linear_mode` and
// `split_pct`, and it is the seam 12.7's `nerd_font` row lands in — one entry,
// no new mechanism.
//
// # Condition-gated rows (8.2.19)
//
// The grammar asks for rows that appear the moment their parent flips. The
// registry has no dependencies between rows today — every row in
// internal/config stands alone — so [gates] is empty and every row is visible.
// The mechanism ships anyway, because the alternative is discovering at the
// first dependent row that the list, the search, the selection clamp and the
// scroll window all assumed a fixed set.
//
// # Mounting (the wiring lane's contract)
//
// A [Model] is a [tui2.Pane], a [tui2.PaneKeys], a [tui2.PaneFocus] and a
// [tui2.PaneMouse]. It is bound with Shell.SetPane like anything else, to
// LayerOverlay behind Shell.SetOverlay or to the main pane — this package does
// not know which, and nothing in it reserves a row or assumes a border, so
// both are the same code path. The registry entry and the palette row already
// exist in internal/registry; [EntryID] names the one to bind, so the wiring
// lane never authors a second row for the same verb (5.22).
//
// Section numbers in comments refer to audit-notes/chat-rebuild.md.
package settings
