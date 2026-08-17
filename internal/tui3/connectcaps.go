package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/connect"
)

// THE CONNECTIONS TAB: the sixth page of the settings sheet, and the only one
// that is not the settings registry.
//
// /connect (connectpanel.go) is the same subject asked in a hurry — a list you
// open, act on, and leave. This is the same subject asked at rest: what have I
// connected, and what may it do for me. It lives in the settings sheet because
// that is where a person goes to change a standing answer, and a standing answer
// about somebody's mail is a setting in every sense except which file it is
// written to.
//
//	  Session   Context   Workspace   Display   Providers  ▌Connections▐
//	  ─────────────────────────────────────────────────────────────────
//	  ✓ Google                                     jane@example.com
//	      read your mail                                        yes
//	      send mail as you                                ask first
//	      read your calendar                                    yes
//	      put things on your calendar                     ask first
//	      disconnect
//	  · Slack                                    your channels and DMs
//
// Six decisions, each the reason a row is shaped the way it is:
//
//   - THE ROW IS THE SETTINGS ROW. A name on the left, one fact on the right,
//     the same two-cell lead, the same selection band, the same two-line fold at
//     [tierPhone] — it is [overlayLines], which is what every list on this
//     surface is made of. The tab is a different QUESTION, not a different
//     grammar, and a person should not be able to tell where the sheet ends.
//   - ONE SERVICE IS OPEN AT A TIME. Expanding the second collapses the first,
//     because the column's calm is the whole reason a person can read down it —
//     and with three services expanded the capability rows outnumber the
//     accounts four to one.
//   - THE STATE IS A WORD AND THE WORD IS THE CONTROL. No checkbox, no bracket,
//     no [x]: `yes`, `ask first`, `off`, right-aligned where every other row on
//     this sheet puts its value, and enter walks them. A checkbox would have
//     been a third thing to draw for a state that has three positions, and a
//     bracketed widget is machinery on a row whose whole job is to be read.
//   - THE THREE WORDS ARE THREE WEIGHTS. yes is ink, ask first is muted, off is
//     dim — the typographic ladder styles.go already spends everywhere else, so
//     the column of answers can be scanned without being read. No colour: the
//     accent is spent on the tab bar, and a distinction drawn in colour on this
//     surface is drawn in text too.
//   - A CHANGE IS SAVED THE MOMENT IT IS MADE. There is no save step and no
//     pending state, because a settings sheet that holds your answer hostage to
//     a second keystroke is a sheet that loses it when you press esc. If the
//     engine refuses, the word goes back to what it was and the foot says why —
//     the sheet's own rule for a refusal (settings.go), applied to a row the
//     registry does not own.
//   - AN UNCONNECTED SERVICE CONNECTS FROM HERE. Enter starts the same sign-in
//     /connect starts — the same browser, the same waiting block in the
//     transcript, the same tick — because a tab that listed an account and then
//     sent you somewhere else to get it would be a tab that knows the answer and
//     will not say it. The row says "waiting in your browser…" while the trip is
//     out, and gains its tick, its account and its capabilities when it lands.

// tabConnections is the tab's name on the bar. It is LAST on purpose: the five
// before it are the settings registry, read five ways, and this one is not a
// registry at all. A person walking the bar left to right meets the knobs first
// and their accounts at the end, which is also the order they were asked to
// think about them in.
const tabConnections = "Connections"

// The three words. They are the surface's whole vocabulary for a capability's
// state, and they are words rather than symbols because a symbol here would be
// this surface inventing an iconography for a question it can answer in English.
const (
	capYesWord = "yes"
	capAskWord = "ask first"
	capOffWord = "off"
)

// browserWord is what a row says while its browser trip is out. It is the same
// sentence the transcript's own block says (connect.go), because it is the same
// wait — and a second wording for one fact is a second fact.
const browserWord = "waiting in your browser…"

// disconnectWord and disconnectArmedWord are the last row of an expanded
// service and the question it asks on the first press. Two presses, exactly as
// the /connect panel asks: an account another window may be using is not a
// one-keystroke decision.
const (
	disconnectWord      = "disconnect"
	disconnectArmedWord = "enter again"
)

// connRowKind is what one row of this tab is.
type connRowKind uint8

const (
	// connService is an account: a tick or a dim dot, the name, and whichever
	// fact the row actually has.
	connService connRowKind = iota
	// connCapability is one thing an open service may do, and the word standing
	// for it.
	connCapability
	// connDisconnect is the last row of an open service.
	connDisconnect
)

// connRow is one row of the tab, hung off [sheetItem] so that the cursor walk,
// the scroll, the pointer and the hover need to know nothing about this file.
type connRow struct {
	kind    connRowKind
	service string
	// name is the word a person owns the account by; account and blurb are the
	// two facts a service row can have and never both.
	name    string
	account string
	blurb   string
	// connected is the service row's tick.
	connected bool
	// waiting is a sign-in this tab started that has not come back.
	waiting bool
	// capID, phrase and state are the capability row's own.
	capID  string
	phrase string
	state  connect.CapabilityState
	// armed is the disconnect row with its question standing on it.
	armed bool
}

// connTab is what the tab remembers between builds: which service is open, the
// confirmation standing on its disconnect row, and the sign-in that is out.
//
// The open service is an ID rather than an index because the list is re-read
// from the engine on every build — an account connected in another window
// arrives between two keystrokes, and an index would then be pointing at
// somebody else's row.
type connTab struct {
	expanded string
	armed    bool
	pending  string
	// settled says the tab has been built at least once, which is what keeps the
	// single-service courtesy below from re-opening a service somebody closed.
	settled bool

	// catalog is the engine's whole answer as of the last read, and groups is
	// the grouping computed from it. BOTH ARE COMPUTED ONCE PER READ AND NOT PER
	// FRAME: the catalog is heading for a couple of hundred services, the filter
	// runs on every keystroke, and a sort of two hundred rows per repaint is the
	// difference between a filter that types and a filter that lags.
	catalog []connect.Status
	groups  []connGroup
	// loaded says the two above are worth trusting. It is cleared where the
	// facts change — an account connected, an account dropped — and nowhere
	// else, so a keystroke never re-reads a file.
	loaded bool
}

// connGroup is one heading and the services under it: the accounts a person
// HAS, flat and first, and then everything they could have, by category.
//
// It is a plain value over [connect.Status] and the two functions that make it
// are package-level for one reason: /connect's panel is the same list asked in a
// hurry, and when it grows the same catalog it should group it by calling
// [groupConnections] and [filterConnections] rather than growing its own.
type connGroup struct {
	// head is the dim lowercase label, or "" for a group that has none — the
	// held accounts, and a catalog whose services declare no category at all.
	head string
	// held marks the group of accounts this profile already has. It is pinned
	// first whatever the filter ranks, because "what I have" is not a search
	// result.
	held bool
	rows []connect.Status
}

// ── building the rows ───────────────────────────────────────────────────────

// onConnections reports whether the tab is the one on show.
//
// THE TYPED BOX BELONGS TO THE PAGE YOU ARE ON. Everywhere else on this sheet it
// searches the settings registry across all five tabs; here it filters the
// accounts, because at catalog scale — two hundred services and rising — the
// filter IS the navigation, and a box that jumped to the Providers tab instead
// of narrowing the list would be the one keystroke a person cannot afford.
func (s *sheet) onConnections() bool {
	return s.tab >= 0 && s.tab < len(settingTabs) && settingTabs[s.tab] == tabConnections
}

// buildConnections is the tab's half of [sheet.build].
//
// The list is READ FROM THE ENGINE HERE rather than held from the sheet
// opening, on the terms the /connect panel reads its own: an account connected
// in another window an hour ago is an account this tab has to know about, and
// asking costs a file read.
func (s *sheet) buildConnections() {
	if !s.conn.loaded {
		s.readConnections()
	}
	if s.conns == nil {
		return
	}
	// ONE SERVICE, ALREADY OPEN. A tab with a single connected account and
	// nothing expanded is a tab showing one line and hiding the only thing it
	// exists to say — and "one open at a time" costs nothing when there is one.
	// It happens once per opening: a person who closes it has closed it.
	if !s.conn.settled && s.conn.expanded == "" {
		s.conn.expanded = onlyConnected(s.conn.catalog)
	}
	s.conn.settled = true

	query := strings.ToLower(strings.TrimSpace(s.query.String()))
	groups := s.conn.groups
	if query != "" {
		groups = filterConnections(groups, query)
	}
	for _, group := range groups {
		if head := group.label(query); head != "" {
			s.items = append(s.items, sheetItem{head: head})
		}
		for _, row := range group.rows {
			s.appendConnService(row)
		}
	}
	s.cursor = s.clampCursor(s.cursor)
}

// appendConnService is one account and, where it is the open one, what it may
// do and the row that forgets it.
func (s *sheet) appendConnService(row connect.Status) {
	name := row.Name
	if name == "" {
		name = row.ID
	}
	s.items = append(s.items, sheetItem{conn: &connRow{
		kind: connService, service: row.ID, name: name,
		account: row.Account, blurb: row.Blurb,
		connected: row.Connected,
		waiting:   !row.Connected && s.conn.pending == row.ID,
	}})
	if !row.Connected || s.conn.expanded != row.ID {
		return
	}
	for _, may := range s.conns.Capabilities(row.ID) {
		s.items = append(s.items, sheetItem{conn: &connRow{
			kind: connCapability, service: row.ID,
			capID: may.ID, phrase: may.Phrase,
			state: s.conns.CapabilityState(row.ID, may.ID),
		}})
	}
	s.items = append(s.items, sheetItem{conn: &connRow{
		kind: connDisconnect, service: row.ID, name: name, armed: s.conn.armed,
	}})
}

// readConnections asks the engine once and groups what it said.
func (s *sheet) readConnections() {
	s.conn.catalog, s.conn.groups, s.conn.loaded = nil, nil, true
	if s.conns == nil {
		return
	}
	s.conn.catalog = s.conns.Services()
	s.conn.groups = groupConnections(s.conn.catalog)
}

// reloadConnections is what a CHANGE to the accounts themselves does — one
// connected, one dropped. Everything else on this tab re-reads nothing.
func (s *sheet) reloadConnections() { s.conn.loaded = false }

// ── the catalog: grouping, and the filter over it ───────────────────────────
//
// Both functions below are the /connect panel's as much as this tab's. They take
// [connect.Status] and answer [connGroup], so neither of them knows what a sheet
// is — which is the whole point: two surfaces listing one catalog must not
// disagree about which category Stripe is in.

// otherWord is where a service with nothing to say about itself lands. EMPTY IS
// NOT A CATEGORY, and a header reading "" would be a heading over a group whose
// only property is that nobody described it.
const otherWord = "other"

// groupConnections is the catalog as a person browses it: the accounts they HAVE,
// flat and first, and then the rest under one lowercase word each.
//
// THE CONNECTED GROUP IS NOT CATEGORIZED. A person scanning what they already
// have is scanning six rows and wants them together; a person browsing what they
// could have is scanning two hundred and needs the word that narrows it. Two
// questions, two shapes, one list.
//
// A catalog whose services declare NO category at all comes back as one
// unheaded group, which is the flat list this tab drew before categories
// existed — so the merge order between this branch and the one that fills the
// field cannot break anything.
func groupConnections(rows []connect.Status) []connGroup {
	held := connGroup{held: true}
	byCategory := map[string][]connect.Status{}
	categorized := false
	var loose []connect.Status
	for _, row := range rows {
		if row.Connected {
			held.rows = append(held.rows, row)
			continue
		}
		loose = append(loose, row)
		if strings.TrimSpace(row.Category) != "" {
			categorized = true
		}
	}
	out := make([]connGroup, 0, 8)
	if len(held.rows) > 0 {
		out = append(out, held)
	}
	if len(loose) == 0 {
		return out
	}
	if !categorized {
		return append(out, connGroup{rows: loose})
	}
	names := make([]string, 0, 8)
	for _, row := range loose {
		word := strings.ToLower(strings.TrimSpace(row.Category))
		if word == "" {
			word = otherWord
		}
		if _, seen := byCategory[word]; !seen {
			names = append(names, word)
		}
		byCategory[word] = append(byCategory[word], row)
	}
	// Alphabetical, with "other" last: it is the group that says the least, so
	// it is the group a person reaches last.
	sort.SliceStable(names, func(i, j int) bool {
		if (names[i] == otherWord) != (names[j] == otherWord) {
			return names[j] == otherWord
		}
		return names[i] < names[j]
	})
	for _, word := range names {
		out = append(out, connGroup{head: word, rows: byCategory[word]})
	}
	return out
}

// filterConnections narrows the groups to what a typed word reaches, and ranks
// what it found.
//
// THE CATEGORY IS PART OF THE MATCH, which is the whole reason this exists:
// typing "billing" has to surface Stripe and Chargebee and Recurly, none of
// which contain the word. A NAME HIT OUTRANKS A CATEGORY HIT — somebody typing
// "stripe" wants Stripe, not the eleven other things filed beside it — so the
// group holding the best name hit is the group that comes first, and inside a
// group the name hits lead.
//
// The held accounts stay pinned at the top whatever the ranking says. What a
// person already has is not a search result.
func filterConnections(groups []connGroup, query string) []connGroup {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return groups
	}
	type ranked struct {
		group connGroup
		best  int
		order int
	}
	kept := make([]ranked, 0, len(groups))
	for at, group := range groups {
		type hit struct {
			row   connect.Status
			score int
			order int
		}
		hits := make([]hit, 0, len(group.rows))
		best := 0
		for i, row := range group.rows {
			score := connMatch(row, query)
			if score == 0 {
				continue
			}
			hits = append(hits, hit{row: row, score: score, order: i})
			if score > best {
				best = score
			}
		}
		if len(hits) == 0 {
			continue
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].order < hits[j].order
		})
		narrowed := connGroup{head: group.head, held: group.held, rows: make([]connect.Status, 0, len(hits))}
		for _, one := range hits {
			narrowed.rows = append(narrowed.rows, one.row)
		}
		kept = append(kept, ranked{group: narrowed, best: best, order: at})
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].group.held != kept[j].group.held {
			return kept[i].group.held
		}
		if kept[i].best != kept[j].best {
			return kept[i].best > kept[j].best
		}
		return kept[i].order < kept[j].order
	})
	out := make([]connGroup, 0, len(kept))
	for _, one := range kept {
		out = append(out, one.group)
	}
	return out
}

// connMatch scores one service against a folded query: a name it starts with
// beats a name it is inside, which beats the category it is filed under, which
// beats nothing at all.
func connMatch(row connect.Status, query string) int {
	name := strings.ToLower(strings.TrimSpace(row.Name))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(row.ID))
	}
	switch {
	case strings.HasPrefix(name, query):
		return 3
	case strings.Contains(name, query):
		return 2
	case strings.Contains(strings.ToLower(strings.TrimSpace(row.Category)), query) &&
		strings.TrimSpace(row.Category) != "":
		return 1
	}
	return 0
}

// label is the group's heading as it is drawn: the word, and — only while a
// filter is on — how many of its services the filter left. A count on a list
// nobody narrowed is a number that answers a question nobody asked.
func (g connGroup) label(query string) string {
	if g.head == "" {
		return ""
	}
	if strings.TrimSpace(query) == "" {
		return g.head
	}
	return g.head + " · " + itoa(len(g.rows))
}

// onlyConnected is the id of the one connected service, or "" where there is
// any other number of them.
func onlyConnected(rows []connect.Status) string {
	found := ""
	for _, row := range rows {
		if !row.Connected {
			continue
		}
		if found != "" {
			return ""
		}
		found = row.ID
	}
	return found
}

// connEmptyWord is what the tab says instead of rows. The two sentences are the
// /connect panel's own, for the reason every other word here is: one fact, one
// wording.
func (s *sheet) connEmptyWord() string {
	switch {
	case s.conns == nil:
		return connectUnavailableWord
	case len(s.conn.catalog) == 0:
		return noServicesWord
	}
	// There are services; this filter reached none of them. That is the
	// sheet's own sentence, and it is the true one here.
	return "nothing matches"
}

// ── drawing them ────────────────────────────────────────────────────────────

// connRowLines is one row of the tab, drawn through the same [overlayLines]
// every other row on this sheet is drawn through.
func (s *sheet) connRowLines(row *connRow, selected, hovered bool, width int, pal palette) []string {
	switch row.kind {
	case connCapability:
		// The phrase is indented under the account it belongs to, and the answer
		// is tinted rather than left to the row's ordinary dim: the three words
		// are three weights, which is what makes the column scannable.
		return overlayLinesTinted(connIndent+row.phrase, capWord(row.state), capStateInk,
			selected, false, hovered, width, pal)

	case connDisconnect:
		note := ""
		if row.armed {
			note = disconnectArmedWord
		}
		return overlayLines(connIndent+disconnectWord, note, selected, false, hovered, width, pal)
	}

	// A service: the state glyph, the name, and whichever fact the row has.
	mark := glyphIdle
	if row.connected {
		mark = glyphConnected
	}
	if pal.linear {
		mark = glyphIdleASCII
		if row.connected {
			mark = glyphConnectedASCII
		}
	}
	return overlayLines(mark+" "+row.name, connNote(row), selected, false, hovered, width, pal)
}

// connIndent is how far a capability sits under its account. Two cells on top of
// the row's own lead, which is the hanging indent this surface already uses for
// a fact that belongs to the row above it (palette.go's overlayIndent).
const connIndent = "  "

// connNote is the one fact a service row carries on the right: the trip that is
// out, the account it is held as, or the line saying what connecting it buys.
//
// THE EMPTINESS LAW, twice. A connected account nobody named says nothing at all
// — not "connected", which the tick already said, and not a placeholder for a
// fact this surface does not have. A service with no blurb says nothing either.
func connNote(row *connRow) string {
	switch {
	case row.waiting:
		return browserWord
	case row.connected:
		return row.account
	}
	return row.blurb
}

// capWord is the state as a person reads it. A state this surface does not know
// draws NOTHING rather than its raw value: a word nobody wrote for a reader is
// machinery on the one row that must not carry any.
func capWord(state connect.CapabilityState) string {
	switch state {
	case connect.StateYes:
		return capYesWord
	case connect.StateAsk:
		return capAskWord
	case connect.StateOff:
		return capOffWord
	}
	return ""
}

// capStateInk is the ladder: yes is ink, ask first is muted, off is dim.
//
// On the SELECTED row the ladder flattens to ink, and off is lifted to muted
// rather than left dim — dim grey on the selection band is grey on grey
// (palette.go), and the answer is the half of the row a person stopped on the
// row to read. The band has already said which row this is; it does not need
// the ladder to say it twice.
func capStateInk(pal palette, note string, selected bool) string {
	if selected {
		if note == capOffWord {
			return pal.muted(note)
		}
		return pal.ink(note)
	}
	switch note {
	case capYesWord:
		return pal.ink(note)
	case capAskWord:
		return pal.muted(note)
	}
	return pal.dim(note)
}

// connFootNote is what the sheet's foot line says while the cursor is on this
// tab. It answers the one question the rows cannot: where a change goes, and —
// on a row that is not connected — what enter would do, which is the only thing
// on this tab a person could not guess.
func (s *sheet) connFootNote() string {
	item, ok := s.current()
	if !ok || item.conn == nil {
		return "the accounts aforge may reach for you"
	}
	switch item.conn.kind {
	case connCapability:
		return "saved the moment you change it"
	case connDisconnect:
		return "the account stays yours — aforge forgets its keys"
	}
	if !item.conn.connected {
		if item.conn.waiting {
			return "finish the sign-in in your browser"
		}
		return "enter signs you in, in your browser"
	}
	return "saved the moment you change it"
}

// connFilterFloor is the shortest list worth telling somebody they can type at.
// Under it the filter still works and simply is not advertised: a legend that
// offers a filter for six rows is teaching a keyboard instead of a choice.
const connFilterFloor = 10

// filterWorth reports whether this catalog is one a person navigates by typing —
// long, or carrying categories, which is the same thing said twice at the scale
// categories arrive at.
func (s *sheet) filterWorth() bool {
	if len(s.conn.catalog) > connFilterFloor {
		return true
	}
	for _, group := range s.conn.groups {
		if group.head != "" {
			return true
		}
	}
	return false
}

// connKeysLine is the tab's key legend, in the sheet's own grammar.
func (s *sheet) connKeysLine() string {
	act := "enter act"
	if item, ok := s.current(); ok && item.conn != nil {
		switch {
		case item.conn.kind == connCapability:
			act = "enter " + capYesWord + " · " + capAskWord + " · " + capOffWord
		case item.conn.kind == connDisconnect:
			act = "enter twice to disconnect"
		case item.conn.connected:
			act = "enter opens"
		default:
			act = "enter connects"
		}
	}
	line := "↑↓ move · ←→ tabs · " + act
	if s.filterWorth() {
		line += " · type to filter"
	}
	return line + " · esc close"
}

// ── acting on them ──────────────────────────────────────────────────────────

// connAct is enter on a row of this tab, and the second click that means the
// same thing.
func (a *app) connAct(row *connRow) tea.Cmd {
	s := &a.sheet
	if s.conns == nil {
		return nil
	}
	switch row.kind {
	case connCapability:
		return a.cycleCapability(row)
	case connDisconnect:
		return a.disconnectService(row)
	}

	if !row.connected {
		// The sheet STAYS UP. /connect closes itself here because it is a list
		// that had one job; this is a page a person is reading, and closing the
		// whole settings sheet under somebody who asked for one account would be
		// answering a question by taking the room away. The row says it is
		// waiting, and the transcript's block is there when they leave.
		s.conn.pending = row.service
		s.msg = ""
		s.rebuildConnAt(row)
		return a.beginConnect(row.service, a.serviceName(row.service, row.name))
	}
	if s.conn.expanded == row.service {
		s.conn.expanded = ""
	} else {
		s.conn.expanded = row.service
	}
	s.conn.armed = false
	s.rebuildConnAt(row)
	return nil
}

// cycleCapability walks yes → ask first → off → yes and WRITES AT ONCE.
//
// A refusal is shown and the word goes back: the state is read from the engine
// on every build, so a Set that failed leaves the row saying exactly what it
// said before — which is the honest reading of "nothing was changed".
func (a *app) cycleCapability(row *connRow) tea.Cmd {
	s := &a.sheet
	next := nextCapState(row.state)
	if err := s.conns.SetCapabilityState(row.service, row.capID, next); err != nil {
		s.msg = err.Error()
	} else {
		s.msg = ""
	}
	s.rebuildConnAt(row)
	return nil
}

// nextCapState is the cycle, and it is the order the words are read in: the
// widest answer first, then the careful one, then none.
func nextCapState(state connect.CapabilityState) connect.CapabilityState {
	switch state {
	case connect.StateYes:
		return connect.StateAsk
	case connect.StateAsk:
		return connect.StateOff
	}
	return connect.StateYes
}

// disconnectService asks once and forgets the account on the second press.
func (a *app) disconnectService(row *connRow) tea.Cmd {
	s := &a.sheet
	if !s.conn.armed {
		s.conn.armed = true
		s.rebuildConnAt(row)
		return nil
	}
	s.conn.armed = false
	if err := s.conns.Disconnect(row.service); err != nil {
		s.msg = err.Error()
		s.rebuildConnAt(row)
		return nil
	}
	// The rows under it are gone with the account, so the service row is where
	// the cursor lands — the row a person was looking at, still on screen. The
	// catalog is re-read because this is one of the two things that change it.
	s.conn.expanded = ""
	s.msg = ""
	s.reloadConnections()
	s.rebuildConnAt(&connRow{kind: connService, service: row.service})
	return nil
}

// connEsc is the tab's rung of the sheet's esc ladder: the confirmation
// standing on a row, then the service that is open, and only then the sheet
// itself (settings.go backs the search out above both).
//
// It reports whether it took the key. Nothing here leaves the TAB — esc on a
// settings sheet closes the sheet, and a key that walked back to the previous
// page would be this one tab inventing a meaning for it.
func (a *app) connEsc() bool {
	s := &a.sheet
	if !s.open || !s.onConnections() {
		return false
	}
	switch {
	case s.conn.armed:
		s.conn.armed = false
	case s.conn.expanded != "":
		was := s.conn.expanded
		s.conn.expanded = ""
		s.rebuildConnAt(&connRow{kind: connService, service: was})
		return true
	default:
		return false
	}
	if item, ok := s.current(); ok && item.conn != nil {
		s.rebuildConnAt(item.conn)
		return true
	}
	s.build()
	return true
}

// rebuildConnAt re-reads the tab and puts the cursor back on the row it was on.
//
// The rows are rebuilt from the engine after every act, so "the row it was on"
// is an IDENTITY and not an index: a service that vanished, a capability list
// that grew, an account connected in another window — each of them moves the
// row a number would have been pointing at.
func (s *sheet) rebuildConnAt(want *connRow) {
	s.build()
	if want == nil {
		return
	}
	for i, item := range s.items {
		row := item.conn
		if row == nil || row.kind != want.kind || row.service != want.service {
			continue
		}
		if row.kind == connCapability && row.capID != want.capID {
			continue
		}
		s.cursor = i
		return
	}
	s.cursor = s.clampCursor(s.cursor)
}

// ── what the browser trip comes back with ───────────────────────────────────

// connTabStopped is a sign-in that never got as far as a browser. The sentence
// is the engine's own, on the sheet's foot line, because the transcript's copy
// of it is behind a fullscreen sheet nobody can see past.
func (a *app) connTabStopped(service, why string) {
	s := &a.sheet
	// MINE is the whole of the question. A sign-in the session asked for, or one
	// somebody started in another window, is not this tab's news to report — and
	// a foot line about a service nobody here pressed is a sheet talking to
	// itself.
	mine := s.conn.pending == service
	s.conn.pending = ""
	if !mine || !s.open || !s.onConnections() {
		return
	}
	if why != "" {
		s.msg = why
	}
	s.reloadConnections()
	s.rebuildConnAt(&connRow{kind: connService, service: service})
	a.touch()
}

// connTabSettled is the outcome landing on the tab: a connected account gains
// its tick, its address and its capabilities, OPEN — a person who just signed in
// is a person about to look at what they signed up for. One that did not
// complete says so once, quietly, and the row goes back to a dim dot.
func (a *app) connTabSettled(service, name string, connected bool) {
	s := &a.sheet
	mine := s.conn.pending == service
	s.conn.pending = ""
	if !s.open || !s.onConnections() {
		return
	}
	s.reloadConnections()
	if !mine {
		// Somebody else's sign-in, landing while this page happens to be open.
		// The list is re-read so the row is honest, and nothing moves: a cursor
		// that jumped would be this tab acting on news the person did not ask
		// for.
		if item, ok := s.current(); ok && item.conn != nil {
			s.rebuildConnAt(item.conn)
		} else {
			s.build()
		}
		a.touch()
		return
	}
	if connected {
		s.conn.expanded, s.conn.armed, s.msg = service, false, ""
	} else {
		s.msg = a.serviceName(service, name) + " connection didn't complete"
	}
	s.rebuildConnAt(&connRow{kind: connService, service: service})
	a.touch()
}
