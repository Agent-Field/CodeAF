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

	// Surface is the KIND of room this catalog is being drawn in, and it decides
	// which spelling of a key every row teaches. The ZERO VALUE IS
	// [registry.SurfaceDefault], which is what a surface with a free keyboard
	// wants and what every non-chat host gets by saying nothing.
	//
	// IT EXISTS BECAUSE THE SHEET WAS TEACHING KEYS THAT DO NOT FIRE. This list
	// used to ask the registry for SurfaceDefault unconditionally, so in a
	// composer-first room — where every printable character belongs to the draft
	// — it taught `t` for the thread switcher, `v` for the receipts fold and `y`
	// for copy-answer, none of which that room can bind. A reader who pressed
	// them typed letters into their sentence and concluded the feature was
	// missing. The registry has known the honest answer all along
	// ([registry.Entry.KeyOn]); the sheet simply was not asking it.
	Surface registry.Surface

	// Rooms are the live and settled rooms, in the order the wiring wants
	// them shown within their own group. Order is preserved for equally-good
	// matches; ranking never reorders across the live/history split.
	Rooms []Room

	// Jobs is every piece of work the store holds, live and finished, in the
	// order the wiring read them. It is deliberately NOT the rail's window: the
	// rail draws the scope on screen and caps what it draws, and a palette whose
	// search could only find what was already visible would be a filter over the
	// rail rather than the one summon-everything surface. The wiring reads
	// wider — see internal/tui2/chat/overlay.go's catalogJobs — and hands the
	// whole list here once, when the door opens.
	//
	// A job whose id a [Room] already carries is dropped rather than listed
	// twice: the rail's own rows ARE jobs, and one piece of work appearing under
	// both `rooms` and `work` would make the same enter key look like two
	// different doors.
	Jobs []Job

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

	// Drill answers "does choosing this row open a SUB-LIST rather than finish
	// the surface" — an option's choices, a role's models. A result it claims
	// does not close the palette: [Options.OnChoose] is called as usual and the
	// wiring answers it with [Palette.Push], one level deeper, with a word for
	// the trail. Nil means nothing here drills, which is the ordinary case.
	//
	// It is a predicate on the RESULT rather than a flag on the row because the
	// wiring is the only side that knows: this package holds ids and never
	// learns what they mean, so "does opening this show more rows" is a question
	// it is structurally unable to answer for itself.
	Drill func(Result) bool

	// Title names the room the `?` overlay is describing ("wisp-parity",
	// "aforge"). Empty falls back to "this room". The summon palette does not
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

// Job is one piece of work in the store as the palette lists it — every job
// ever, not the window the rail happens to be drawing.
//
// It is a [Room] plus the two telemetry cells a job has and a room does not:
// how long it has been going and what it has spent. Both arrive ALREADY
// FORMATTED, for the same reason [SettingRow.Value] does — a duration is
// formatted against a clock, and a render that reads a clock is a render whose
// output is not a pure function of its state (pane.go's contract). The wiring
// formats them once, at the moment the door opens.
//
// Choosing a job yields [JumpToRoom] with its id, which is the same result a
// room row yields, because opening a job IS opening its room — the palette
// must not grow a second door onto a place the rail already reaches.
type Job struct {
	// ID is the rail scope id of this job's room, and what [JumpToRoom]
	// carries back.
	ID string
	// Title is the job's task word. It is the fuzzy-matched text and the row's
	// verb column.
	Title string
	// Seed is the identity seed (5.16) — the top-level task id — picking the
	// row's pastel glyph and its selection-band tint.
	Seed string
	// Attention is what the job's glyph says, and — through its terminal
	// states — whether the job is listed under `work` or under the dim
	// `history` group (5.18).
	Attention rail.Attention
	// Questions is the count of open questions blocking on a human, rendered
	// as 5.21's amber `?N` chip. Zero draws nothing.
	Questions int
	// Summary is the job's one-line description. It is fuzzy-matched alongside
	// Title.
	Summary string
	// Receipt is what the job last accounted for — the receipt line the room
	// would show. It is drawn behind the summary in the description column, so
	// the selected row previews enough to choose on without opening anything.
	// Empty is common and draws nothing, not an empty separator.
	Receipt string
	// Age is how long this job has been going, already formatted ("14m",
	// "2h"). See the type doc for why it is a string.
	Age string
	// Cost is what it has spent, already formatted ("$1.24"). Empty for a job
	// that has spent nothing worth a column.
	Cost string
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
// order (5.22 rule 2 and 5.18's ordering, taken together).
//
// ACTIONS LEAD. That is the correction this wave made, and it follows from what
// the surface became: once the catalog holds every job the store has ever run,
// rooms-first meant that typing three letters put the verb you asked for below
// however many rooms happened to fuzzy-match those same letters. The palette is
// summoned to DO something; the thing being done is the answer, and the places
// it could be done are the context. Live work comes next because a job needing
// a human is the most expensive state on the list, then the rooms that reach it,
// then the two dim groups — finished work, then settings — that are reference
// rather than attention.
type section uint8

const (
	sectionActions section = iota
	sectionWork
	sectionRooms
	sectionHistory
	sectionSettings
	// The two groups of the thread switcher (5.3's `switcher` row). They are
	// last because they are never built into the summon palette's catalog:
	// [buildRows] does not emit them and [Switcher] builds only them, so the two
	// surfaces share the list and share no rows.
	sectionThreads
	sectionNewThread
	sectionCount
)

// title is the group header drawn above a section's rows. `history` is 5.18's
// own word for the settled group and is reused verbatim so the `@` filter and
// the palette name the same thing the same way.
func (s section) title() string {
	switch s {
	case sectionActions:
		return "actions"
	case sectionWork:
		return "work"
	case sectionRooms:
		return "rooms"
	case sectionHistory:
		return "history"
	case sectionSettings:
		return "settings"
	case sectionThreads:
		return "threads"
	case sectionNewThread:
		// The door at the foot of the switcher titles nothing: it is one row and
		// its own words say what it is. The switcher draws no headers at all
		// ([list.noHeaders]), so this is never rendered — it is spelled so the
		// vocabulary is total and a future caller cannot get "" by accident.
		return "new"
	}
	return ""
}

// dim reports whether the whole section renders one tier down. History and
// settings are reference material a user scrolls to on purpose; actions, work
// and rooms are what the palette opened for.
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
	// lowerTags is the row's hidden matchable text, already lower-cased and
	// joined. It is a THIRD haystack for [filter] and it is never drawn: a
	// thread's subjects make it findable without making its row noisier, which
	// is the whole of why tags are a filter feature and not a chip strip. Empty
	// for every row that has none, which is every row that is not a thread.
	lowerTags string
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
	claimed := roomIDs(c.Rooms)
	dst = appendActions(dst, c)
	dst = appendJobs(dst, c.Jobs, claimed, false)
	dst = appendRooms(dst, c.Rooms, false)
	dst = appendRooms(dst, c.Rooms, true)
	dst = appendJobs(dst, c.Jobs, claimed, true)
	dst = appendSettings(dst, c.Settings)
	return dst
}

// roomIDs is the set of ids the rail already offers as rooms, so a job the
// wiring read from the store does not get listed a second time under its own
// heading. It is built once per SetCatalog — never per keystroke — and a
// catalog with no jobs in it pays nothing at all.
func roomIDs(rooms []Room) map[string]struct{} {
	if len(rooms) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(rooms))
	for i := range rooms {
		out[rooms[i].ID] = struct{}{}
	}
	return out
}

// appendJobs emits one of the two work groups: the live jobs under `work`, or
// the finished ones under the dim `history` group beside the settled rooms.
// Both passes walk the caller's slice in the caller's order, so a wiring that
// sorted its jobs newest-first keeps that order inside each group.
func appendJobs(dst []row, jobs []Job, claimed map[string]struct{}, history bool) []row {
	sec := sectionWork
	if history {
		sec = sectionHistory
	}
	for i := range jobs {
		j := &jobs[i]
		if terminalRoom(j.Attention) != history {
			continue
		}
		if _, taken := claimed[j.ID]; taken {
			continue
		}
		dst = append(dst, newJobRow(sec, j))
	}
	return dst
}

// newJobRow builds one work row: the task word, the summary with its receipt
// behind it, and the two telemetry cells in the accelerator column.
//
// The right column carries age and cost rather than a key, and that is not the
// teaching rule bending. A job HAS no accelerator — it is reached by being
// chosen, which is what the palette is for — so the column that would have
// taught a chord is spent on the two facts that decide which of forty jobs the
// reader meant. [list.renderRight] already gives that column up before the verb
// under width pressure, which is the right drop order for a fact that is not a
// door.
func newJobRow(sec section, j *Job) row {
	title := clean(j.Title)
	if title == "" {
		title = untitledRow
	}
	out := row{
		sec:       sec,
		verb:      title,
		desc:      jobDesc(j),
		accel:     jobTelemetry(j),
		glyph:     j.Attention.Glyph(),
		glyphTok:  glyphToken(j.Attention, j.Seed),
		band:      roomBand(j.Seed),
		questions: j.Questions,
		result:    JumpToRoom{ID: j.ID},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// jobDesc is the preview: what the job is doing, and what it last accounted
// for, behind 5.17's telemetry separator. Both halves are fuzzy-matched, which
// is what makes a search for a file name in a receipt find the job that wrote
// it. Either half may be missing, and a missing half leaves no dangling
// separator behind it.
func jobDesc(j *Job) string {
	return join(clean(j.Summary), clean(j.Receipt), " "+tokens.GlyphSeparator+" ")
}

// jobTelemetry is the age and the cost as one cell. Age leads: how long
// something has been going is what a reader scanning live work is asking, and
// cost is the second question.
func jobTelemetry(j *Job) string {
	return join(clean(j.Age), clean(j.Cost), " ")
}

// join puts sep between a and b, and returns whichever one exists when the
// other does not.
func join(a, b, sep string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + sep + b
}

// untitledRow is what a row with no name says. A blank verb column would read
// as a rendering fault; the parenthesis says the name is missing on purpose.
const untitledRow = "(untitled)"

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
		title = untitledRow
	}
	summary := clean(r.Summary)
	out := row{
		sec:       sec,
		verb:      title,
		desc:      summary,
		accel:     r.Key,
		glyph:     r.Attention.Glyph(),
		glyphTok:  glyphToken(r.Attention, r.Seed),
		band:      roomBand(r.Seed),
		questions: r.Questions,
		result:    JumpToRoom{ID: r.ID},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// glyphToken resolves a row's state-glyph colour once — for a room and for a
// job alike, because the two answer the same question with the same glyph and
// two resolutions of one fact is how a surface starts disagreeing with itself.
// A row whose attention carries a semantic word keeps that word's hue — amber
// for a blocked human, coral for broken, green for settled, cyan for alive
// (5.16) — and a row with no word spends the slot on its identity pastel, the
// same trade [rail.Row.GlyphHue] makes and for the same reason: a rail of
// running work has to be tellable apart at a glance.
func glyphToken(a rail.Attention, seed string) tokens.Token {
	hue := a.Hue()
	if hue == tokens.HueNone && seed != "" {
		return tokens.IdentityFor(seed)
	}
	state := tokens.StateSettled
	if a.Live() {
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
			dst = append(dst, newActionRow(a, c.Surface))
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
		dst = append(dst, newActionRow(a, c.Surface))
	}
	return dst
}

func newActionRow(a Action, surface registry.Surface) row {
	out := row{
		sec:      sectionActions,
		verb:     a.Entry.Verb,
		desc:     a.Entry.Description,
		accel:    accelOf(a.Entry, surface),
		band:     tokens.Band,
		disabled: clean(a.Disabled),
		result:   RunEntry{ID: a.Entry.ID},
	}
	out.lowerVerb = lower(out.verb)
	out.lowerDesc = lower(out.desc)
	return out
}

// accelOf is the accelerator column for a registry entry, which is the KEY HALF
// of [registry.Chip] and nothing else — the ladder (key, else slash alias) lives
// in the registry now, because this file, the footer and the chat surface's
// empty state were each keeping a copy of it and two of them had drifted.
//
// The verb half is not read here: this list draws the verb in its own left
// column, so the row IS the chip, laid out across the sheet instead of packed
// into one run. Same grammar, same order, same two tiers — verb first and
// brighter, key behind it and dimmer (see [rowTokens]).
//
// `ask` is not filler. A [registry.ScopeTalk] entry is reached only through the
// user's own words to the head — it has no key and no alias by construction —
// and the honest thing to print in the column that teaches doors is the door
// that exists. Printing nothing there would read as "no way to do this",
// which is the opposite of true.
// THE SEAM IS CLOSED, AND surface IS HOW. This function used to pass
// [registry.SurfaceDefault] unconditionally and said so in a comment calling
// itself "a REQUESTED SEAM"; the request is now honoured. A host declares the
// kind of room it is ([Catalog.Surface]) and every row teaches the key that room
// can really bind — so a composer-first chat draws `ctrl+r` where it used to
// draw a bare `v` that typed a letter into the reader's sentence.
//
// A row with no accelerator ON THIS SURFACE falls to `ask` rather than to the
// other surface's key, which is the honest answer: the door exists and the way
// through it here is words.
func accelOf(e registry.Entry, surface registry.Surface) string {
	if key := registry.ChipOn(e, surface).Key; key != "" {
		return key
	}
	return askAccel
}

// askAccel names the prose door of a belt-only verb. See [accelOf].
const askAccel = registry.AskKey

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
