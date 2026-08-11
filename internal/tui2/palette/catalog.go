package palette

import (
	"github.com/Agent-Field/aforge-v2/internal/registry"
	"github.com/Agent-Field/aforge-v2/internal/sanitize"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/rail"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The input contract. The wiring fills a [Catalog] whenever the facts move —
// a task settles, the scope changes, a setting is written — and hands it over
// with SetCatalog. It is not consulted per keystroke and it is never consulted
// per frame: everything a render needs is precomputed at that moment, which is
// what lets the filter path stay allocation-lean.

// Catalog is everything both surfaces draw from.
//
// The zero value is meaningful and renders an empty, honest palette: no rooms,
// every registry entry (see [Catalog.Scope]), no settings.
type Catalog struct {
	// Scope is the registry predicate for the room the user is in — the same
	// [registry.Scope] the footer and the key router ask with. The ZERO VALUE
	// IS TREATED AS [registry.ScopeAny]: an unscoped palette shows everything,
	// because a caller that has not said where it is has not said "nowhere",
	// and a palette that silently shows no actions is the discoverability
	// failure 5.22 exists to remove.
	Scope registry.Scope

	// Rooms are the live and settled rooms, in the order the wiring wants
	// them shown within their own group. Order is preserved for equally-good
	// matches; ranking never reorders across the live/history split.
	Rooms []Room

	// Actions overrides the registry lookup. A nil Actions means "derive from
	// the registry for Scope", which is the normal case and costs the wiring
	// nothing. A non-nil Actions is used VERBATIM and is NOT re-filtered by
	// Scope — the wiring that bothered to build the slice has already decided,
	// and second-guessing it here would make the override useless for the one
	// thing an override is for.
	Actions []Action

	// Reason answers "why can this room not do that right now", by registry
	// entry id, and returns the empty string for an entry that is available.
	// It is consulted once per SetCatalog, never per keystroke.
	//
	// The words are the wiring's: this package will not invent "settled — ask
	// aforge" on a caller's behalf, because only the caller knows whether that
	// is the true reason. A nil Reason means every entry is available, which
	// is a claim — make it deliberately.
	Reason func(entryID string) string

	// Settings are the settings rows, already read. See [SettingRow] for why
	// this is not []config.Setting.
	Settings []SettingRow

	// Title names the room the `?` overlay is describing ("wisp-parity",
	// "aforge"). Empty falls back to "this room". The ctrl+k palette does not
	// use it — it is a catalog over every room, not a view of one.
	Title string
}

// Room is one conversational surface the palette can jump to: the home room,
// a top-level task's room, a worker's room. It is deliberately a flat value
// and not a [rail.Row] — the palette needs five fields and a rail row carries
// twenty, and a surface that accepted the bigger type would be tempted to
// start rendering telemetry it has no column for.
//
// The three fields that matter for grouping and colour map one-for-one off a
// rail row the wiring already holds:
//
//	Attention: row.Attention()
//	Seed:      row.Seed
//	ID:        row.ID
type Room struct {
	// ID is the rail scope id this row jumps to. The empty string is home
	// ([rail.HomeScopeID]), which is a legitimate value and not a missing one.
	ID string
	// Title is the room's name — the task word, or `aforge` at home. It is
	// the fuzzy-matched text and the row's verb column.
	Title string
	// Seed is the identity seed (5.16) — the top-level task id. It picks the
	// row's pastel glyph and its selection-band tint. Empty means no identity,
	// which is home's honest state and not a gap.
	Seed string
	// Attention is what the room's glyph says. It decides the glyph, its hue,
	// and — through its terminal states — whether the room is listed live or
	// under the dim history group (5.18).
	Attention rail.Attention
	// Questions is the count of open questions blocking on a human in this
	// room, rendered as 5.21's amber `?N` count chip. Zero draws nothing:
	// amber only ever means a human is actually needed (5.16).
	Questions int
	// Summary is the room's one-line description — the live summary the rail
	// card already carries, or whatever sentence best answers "what is this
	// room". It is fuzzy-matched alongside Title.
	Summary string
	// Key is the accelerator that reaches this room right now, if one does
	// ("1".."9" under the digit-precedence rule). Empty is common and fine:
	// most rooms are reached by selection, not by a chord.
	Key string
}

// Action is one registry entry as this catalog offers it, plus the one fact
// the registry cannot know — whether this room can actually do it now.
type Action struct {
	// Entry is the registry row. Verb, Description, Key and Slash are
	// rendered; ID is what [RunEntry] carries back.
	Entry registry.Entry
	// Disabled is the reason the action is unavailable here, in the wiring's
	// own words ("settled — ask aforge"). Empty means available. A disabled
	// row still renders — 5.20 rule 3 is explicit that the user never has to
	// guess — but it advertises no accelerator and enter does nothing on it.
	Disabled string
}

// SettingRow is one settings row as the palette shows it.
//
// It is this package's own shape rather than internal/config's [config.Setting]
// for two reasons, both structural. The palette is a leaf of the TUI tree and
// importing config would pull a profile directory, an env-pin reader and a
// file writer behind a surface whose whole job is to not have side effects.
// And config's row carries live read/write closures: holding one across
// frames would mean a render could read the filesystem, which is exactly the
// per-keystroke cost this package is built to avoid. The wiring reads the
// values once, at SetCatalog time, from `(*config.Settings).Rows()`:
//
//	for _, s := range settings.Rows() {
//	    rows = append(rows, palette.SettingRow{
//	        Key: s.Key, Label: s.Label, Hint: s.Hint, Value: s.Value(),
//	    })
//	}
type SettingRow struct {
	// Key is the config setting key, and what [OpenSetting] carries back.
	Key string
	// Label is the setting's name — the verb column. config.Setting.Label.
	Label string
	// Hint is the one-line description. config.Setting.Hint.
	Hint string
	// Value is the current reading, already formatted, from
	// config.Setting.Value(). It occupies the row's right-aligned column: for
	// a setting, what it is set to IS the thing worth teaching, and `/settings`
	// is the door every one of these rows shares anyway.
	Value string
	// Pinned names the environment variable holding this row read-only, from
	// config.Setting.PinnedBy(). A pinned row renders disabled with that
	// variable as its reason, which is the capability-honesty rule applied to
	// a setting the user cannot in fact change.
	Pinned string
}

// section is which group a row belongs to, and the constants are in DISPLAY
// order (5.22 rule 2 and 5.18's ordering, taken together): live rooms first
// because a room needing a human is the most expensive state on the list, then
// the actions available where the user is, then the two dim groups — settled
// rooms, then settings — that are reference rather than attention.
type section uint8

const (
	sectionRooms section = iota
	sectionActions
	sectionHistory
	sectionSettings
	sectionCount
)

// title is the group header drawn above a section's rows. `history` is 5.18's
// own word for the settled group and is reused verbatim so the `@` filter and
// the palette name the same thing the same way.
func (s section) title() string {
	switch s {
	case sectionRooms:
		return "rooms"
	case sectionActions:
		return "actions"
	case sectionHistory:
		return "history"
	case sectionSettings:
		return "settings"
	}
	return ""
}

// dim reports whether the whole section renders one tier down. History and
// settings are reference material a user scrolls to on purpose; rooms and
// actions are what the palette opened for.
func (s section) dim() bool { return s == sectionHistory || s == sectionSettings }

// row is one built, sanitized, immutable list row. Everything expensive —
// lowercasing for the filter, sanitizing model prose, resolving a token — is
// done once here, at SetCatalog, and never again on a keystroke or a frame.
type row struct {
	sec section

	verb  string
	desc  string
	accel string

	// glyph is the room state glyph, empty for everything else. Its token is
	// resolved at build time because the resolution reads the identity wheel,
	// and a render must not hash a task id per frame.
	glyph    string
	glyphTok tokens.Token
	// band is the selection band this row draws when selected — identity-
	// tinted for a room with a seed, plain otherwise (5.16).
	band tokens.Token
	// questions is 5.21's `?N` count chip, zero for no chip.
	questions int

	// disabled is the reason this row cannot be chosen, empty when it can.
	disabled string

	result Result

	lowerVerb string
	lowerDesc string
}

// enabled reports whether choosing this row yields anything.
func (r *row) enabled() bool { return r.disabled == "" && r.result != nil }

// sanitizeTable is the same chokepoint the rail uses on the same class of
// input. Room titles and summaries are model-written prose, and prose that can
// move the cursor or repaint the screen would outrank the palette from inside
// a row the palette drew itself. Benign text returns from the scan unallocated.
var sanitizeTable = sanitize.Table(tokens.ANSI16Remap)

// clean is the one door prose takes onto a row: sanitized, then newline-
// flattened so a summary carrying a stray "\n" cannot smuggle a second line
// into a list whose height is already budgeted.
func clean(s string) string {
	return blocks.Flatten(sanitize.TextWithPalette(s, sanitizeTable))
}

// buildRows turns a catalog into the flat, section-ordered row slice the list
// filters and renders. It appends into dst so a component that already has a
// slice from the previous catalog reuses its capacity.
// The append order IS the display order, and [filter] depends on it: the
// filter ranks only within a run of like-sectioned rows and never reorders
// across one, so a section built out of order would render out of order.
func buildRows(dst []row, c Catalog) []row {
	dst = dst[:0]
	dst = appendRooms(dst, c.Rooms, false)
	dst = appendActions(dst, c)
	dst = appendRooms(dst, c.Rooms, true)
	dst = appendSettings(dst, c.Settings)
	return dst
}

// appendRooms emits one of the two room groups: the live ones under `rooms`,
// or the settled ones under the dim `history` group (5.18). Both passes walk
// the same slice in the caller's order, so a wiring that sorted its rooms
// keeps its sort inside each group.
func appendRooms(dst []row, rooms []Room, history bool) []row {
	sec := sectionRooms
	if history {
		sec = sectionHistory
	}
	for i := range rooms {
		r := &rooms[i]
		if terminalRoom(r.Attention) != history {
			continue
		}
		dst = append(dst, newRoomRow(sec, r))
	}
	return dst
}

func newRoomRow(sec section, r *Room) row {
	title := clean(r.Title)
	if title == "" {
		title = "(untitled)"
	}
	summary := clean(r.Summary)
	out := row{
		sec:       sec,
		verb:      title,
		desc:      summary,
		accel:     r.Key,
		glyph:     r.Attention.Glyph(),
		glyphTok:  roomGlyphToken(r),
		band:      roomBand(r.Seed),
		questions: r.Questions,
		result:    JumpToRoom{ID: r.ID},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// roomGlyphToken resolves a room's glyph colour once. A room whose attention
// carries a semantic word keeps that word's hue — amber for a blocked human,
// coral for broken, green for settled, cyan for alive (5.16) — and a room with
// no word spends the slot on its identity pastel, the same trade
// [rail.Row.GlyphHue] makes and for the same reason: a rail of running work
// has to be tellable apart at a glance.
func roomGlyphToken(r *Room) tokens.Token {
	hue := r.Attention.Hue()
	if hue == tokens.HueNone && r.Seed != "" {
		return tokens.IdentityFor(r.Seed)
	}
	state := tokens.StateSettled
	if r.Attention.Live() {
		state = tokens.StateLive
	}
	return tokens.ResolveToken(hue, state)
}

// roomBand is the identity-tinted selection band of 5.16, or the plain band
// for a room with no identity (home).
func roomBand(seed string) tokens.Token {
	if seed == "" {
		return tokens.Band
	}
	return tokens.BandFor(tokens.IdentityFor(seed))
}

// terminalRoom reports whether a room's work has stopped for good, which is
// what puts it under `history`. It mirrors [rail.Lifecycle.Terminal] over the
// attention axis, because attention is the field a wiring already has in hand
// from [rail.Row.Attention].
func terminalRoom(a rail.Attention) bool {
	switch a {
	case rail.AttnSettled, rail.AttnFailed, rail.AttnCancelled:
		return true
	}
	return false
}

// appendActions emits the action rows: the wiring's own slice if it supplied
// one, else the registry filtered to the catalog's scope.
func appendActions(dst []row, c Catalog) []row {
	if c.Actions != nil {
		for i := range c.Actions {
			a := c.Actions[i]
			if a.Disabled == "" && c.Reason != nil {
				a.Disabled = c.Reason(a.Entry.ID)
			}
			dst = append(dst, newActionRow(a))
		}
		return dst
	}
	scope := c.Scope
	if scope == 0 {
		scope = registry.ScopeAny
	}
	for _, e := range registry.All() {
		if !e.Scope.Has(scope) {
			continue
		}
		a := Action{Entry: e}
		if c.Reason != nil {
			a.Disabled = c.Reason(e.ID)
		}
		dst = append(dst, newActionRow(a))
	}
	return dst
}

func newActionRow(a Action) row {
	out := row{
		sec:      sectionActions,
		verb:     a.Entry.Verb,
		desc:     a.Entry.Description,
		accel:    accelOf(a.Entry),
		band:     tokens.Band,
		disabled: clean(a.Disabled),
		result:   RunEntry{ID: a.Entry.ID},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// accelOf is the accelerator column for a registry entry: the key if it has
// one, else the slash alias, else the word `ask`.
//
// `ask` is not filler. A [registry.ScopeTalk] entry is reached only through the
// user's own words to the head — it has no key and no alias by construction —
// and the honest thing to print in the column that teaches doors is the door
// that exists. Printing nothing there would read as "no way to do this",
// which is the opposite of true.
func accelOf(e registry.Entry) string {
	switch {
	case e.Key != "":
		return e.Key
	case e.Slash != "":
		return "/" + e.Slash
	}
	return askAccel
}

// askAccel names the prose door of a belt-only verb. See [accelOf].
const askAccel = "ask"

// appendSettings emits the settings group. A pinned row is disabled with the
// environment variable as its reason: a setting the environment holds is a
// setting this surface cannot change, and offering it anyway would be the
// affordance lying (5.20 rule 3, 5.22 rule 5).
func appendSettings(dst []row, settings []SettingRow) []row {
	for i := range settings {
		s := &settings[i]
		label := clean(s.Label)
		if label == "" {
			label = s.Key
		}
		out := row{
			sec:    sectionSettings,
			verb:   label,
			desc:   clean(s.Hint),
			accel:  clean(s.Value),
			band:   tokens.Band,
			result: OpenSetting{Key: s.Key},
		}
		if s.Pinned != "" {
			out.disabled = "set by " + clean(s.Pinned)
		}
		out.lowerVerb = lower(out.verb)
		out.lowerDesc = lower(out.desc)
		dst = append(dst, out)
	}
	return dst
}
