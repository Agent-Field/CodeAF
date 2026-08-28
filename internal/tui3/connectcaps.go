package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
//
//	  ✓ Google                                          jane@example.com
//	      read your mail                                yes
//	      send mail as you                              ask first
//	      read your calendar                            yes
//	      put things on your calendar                   ask first
//	      disconnect
//
//	  ✓ Stripe                                           from $STRIPE_KEY
//	      yes: read what is in this account · ask first: act in your name
//
//	  billing
//
//	  · Chargebee                                                    key
//	  · Recurly                                                      key
//
// Nine decisions, each the reason a row is shaped the way it is:
//
//   - THE ROW IS THE SETTINGS ROW. A name on the left, one fact on the right,
//     the same two-cell lead, the same selection band, the same two-line fold at
//     [tierPhone] — it is [overlayLines], which is what every list on this
//     surface is made of. The tab is a different QUESTION, not a different
//     grammar, and a person should not be able to tell where the sheet ends.
//   - AN ACCOUNT IS A BLOCK AND ITS NAME IS THE HEADING OF IT. A blank line of
//     air above each held account, the name on the row with the tick, and
//     everything the account may do INDENTED UNDER IT while it is open. It read
//     as a flat column of rows before this wave — the account and its four
//     capabilities at the same weight, one line apart — so "which of these lines
//     is a service" was a question a person had to answer by reading, on the one
//     screen whose whole job is to be scanned.
//   - A CLOSED ACCOUNT STILL SAYS WHAT IT MAY DO, in one dim line under its name
//     ([connSummary]). Auditing four accounts should not be four keystrokes and
//     four collapses: the answers are the point of the page, and a page that
//     hides all of them behind an expander is a page that has to be operated
//     before it can be read.
//   - ONE SERVICE IS OPEN AT A TIME. Expanding the second collapses the first,
//     because the column's calm is the whole reason a person can read down it —
//     and with three services expanded the capability rows outnumber the
//     accounts four to one.
//   - THE ANSWERS STAND IN ONE COLUMN. yes, ask first and off are padded to the
//     width of the longest of them, so their first letters line up under each
//     other instead of ending flush with the right edge at three different
//     places. A column of answers that a person can only read by reading is a
//     column that failed at the one thing it is for.
//   - A KEY IS GIVEN HERE. A service that wants a key opens the SAME masked box
//     /connect opens, on the row itself, with what to type above it and where to
//     find it below (connect.go's [keyBoxLines]). The tab listed those services,
//     said "enter connects", and then had nowhere to put the key — which made it
//     a page that knew the answer and would not say it.
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
//   - THE CATALOG IS QUIET AND THE CURSOR IS WHERE THE DETAIL IS. Past the point
//     where the list is browsed rather than read, an available row is a name and
//     one dim word saying what enter will ask for — `key` or `sign in` — and the
//     sentence about what the service is FOR is drawn under the row a person has
//     stopped on, and under no other. Two hundred rows each carrying a sentence
//     is not a catalog, it is a wall; and the sentence is worth reading for
//     exactly one row at a time.

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

// checkingWord is what a KEY row says while its answer is out. Nothing opened
// and nothing is being waited on in another window: the far end is being asked
// whether the key is any good, which is a different sentence because it is a
// different wait.
const checkingWord = "checking your key…"

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
	// name is the word a person owns the account by; account, keyEnv, tag and
	// blurb are the facts a service row can have and it draws exactly one of
	// them ([connNote]).
	name    string
	account string
	blurb   string
	// keyEnv is the environment variable a key connection reads its key from,
	// without the dollar ([connect.Status.KeyEnv]). It is the one thing a key
	// connection has to say about itself and it is drawn where an account is.
	keyEnv string
	// tag is what an available row says about how it connects — `key` or `sign
	// in` — and it is EMPTY ON A LIST SHORT ENOUGH TO READ, where the blurb is
	// the more useful fact and there is room for it. It is the /connect panel's
	// own rule ([connectTag]), applied to the same catalog on the other surface.
	tag string
	// keyed says enter on this row opens a box rather than a browser.
	keyed bool
	// asks says a browser row needs one typed answer before it can open.
	asks bool
	// summary is what a CLOSED account may do, in one dim line ([connSummary]).
	summary string
	// air asks for a blank line above this row: an account is a block, and a
	// block has air over it. See [sheet.listLines], which is the one place on
	// this sheet that spends a line on nothing.
	air bool
	// connected is the service row's tick.
	connected bool
	// waiting is a sign-in this tab started that has not come back.
	waiting bool
	// entry is the key box standing open on this row, or nil. It is the tab's
	// own [connTab.entry] handed to the row that owns it, so that drawing a row
	// needs to know nothing about which one that is.
	entry *keyEntry
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
	// pendingKey says the trip that is out is a key being checked rather than a
	// browser somebody is signing in to. It changes two sentences — what the row
	// says while it waits, and what the foot says if it did not work — and
	// nothing else: "the connection didn't complete" is a true sentence about a
	// browser nobody came back from and a false one about a key that was refused.
	pendingKey bool
	// entry is the key box standing open on a row of this tab, or nil. It is
	// the /connect panel's own box (connect.go's [keyEntry]) and not a second
	// one: one way of giving a key on this surface, drawn in two places.
	entry *keyEntry
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
	open := row.Connected && s.conn.expanded == row.ID
	entry := s.conn.entry
	if entry == nil || entry.id != row.ID || row.Connected {
		// A box open on an account that has since been connected — in another
		// window, or by the answer this box itself sent — belongs to nothing.
		// The row it was asking about is answered.
		entry = nil
	}
	tag := ""
	if !row.Connected && s.connBrowsing() {
		tag = connectTag(row.Service)
	}
	summary := ""
	if row.Connected && !open {
		summary = s.connSummary(row.ID)
	}
	s.items = append(s.items, sheetItem{conn: &connRow{
		kind: connService, service: row.ID, name: name,
		account: row.Account, blurb: row.Blurb, keyEnv: row.KeyEnv,
		tag: tag, keyed: keyService(row.Service), asks: row.Service.Blank != "", summary: summary,
		// AN ACCOUNT IS A BLOCK AND A BLOCK HAS AIR OVER IT — and so does a row
		// with a box standing open on it, which is a block for as long as it is
		// open. A blank line over every row of a two-hundred-row catalog would
		// be a catalog twice as long as it needs to be.
		air:       row.Connected || entry != nil,
		connected: row.Connected,
		waiting:   !row.Connected && s.conn.pending == row.ID,
		entry:     entry,
	}})
	if !open {
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

// connSummary is what a CLOSED account may do, in one line.
//
// ── IT IS THE OPEN ROWS, GROUPED BY THEIR ANSWER ──
//
// The words are the tab's own three — yes, ask first, off — and the sentences
// are the capabilities' own, so the closed line and the open list are visibly
// the same information said twice as short. NOTHING IS REPHRASED. It is
// tempting to write "reads mail, asks before sending" and it would be prettier;
// it would also be this surface inventing a second vocabulary for the answers a
// person set in the first one, and the day a plug declares a sentence nobody
// anticipated the summary would be a grammar exercise instead of a fact.
//
// A group with nothing in it is not drawn — the emptiness law, on the line most
// likely to grow "off: none" — and a service with no capabilities at all says
// nothing whatever.
func (s *sheet) connSummary(service string) string {
	if s.conns == nil {
		return ""
	}
	said := map[connect.CapabilityState][]string{}
	for _, may := range s.conns.Capabilities(service) {
		state := s.conns.CapabilityState(service, may.ID)
		said[state] = append(said[state], may.Phrase)
	}
	parts := make([]string, 0, 3)
	// THE LADDER'S ORDER, which is the order the open rows walk and the order
	// [nextCapState] cycles: the widest answer first, then the careful one, then
	// none.
	for _, state := range []connect.CapabilityState{connect.StateYes, connect.StateAsk, connect.StateOff} {
		phrases := said[state]
		if len(phrases) == 0 {
			continue
		}
		parts = append(parts, capWord(state)+": "+strings.Join(phrases, ", "))
	}
	return strings.Join(parts, " · ")
}

// connBrowsing reports whether this list is one a person BROWSES rather than
// reads, which is the one thing that decides what an available row says on the
// right: the tag on a catalog, the blurb on a list of six.
//
// It is [sheet.filterWorth] and not a second reading of the same fact. A list
// worth typing at is a list too long to read the sentences of, and two thresholds
// for one question would eventually put a tab in the state where it advertises a
// filter and still draws two hundred sentences.
func (s *sheet) connBrowsing() bool { return s.filterWorth() }

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
func (s *sheet) connRowLines(row *connRow, selected, hovered bool, width, boxRows int, pal palette) []string {
	switch row.kind {
	case connCapability:
		// The phrase is indented under the account it belongs to, and the answer
		// is tinted rather than left to the row's ordinary dim: the three words
		// are three weights, which is what makes the column scannable.
		return overlayLinesTinted(connIndent+row.phrase, capNote(row.state), capStateInk,
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
	// A HELD ACCOUNT'S NAME IS A HEADING and is drawn as one: bold ink, whatever
	// the row's own tier would have been. Every other list on this surface is
	// dim until the cursor reaches it, and that rule is right for a list of
	// things you are choosing BETWEEN — it is wrong for the two or three rows
	// that are the whole answer to "what have I connected", which a person came
	// to this page to find rather than to pick from.
	note := connNote(row)
	if note == row.tag && phoneList(width) {
		// AT [tierPhone] THE TAG COSTS A WHOLE LINE, because a row with a tail is
		// two lines there (palette.go) — and two hundred catalog rows at two lines
		// each is a list nobody can reach the bottom of. The word it was carrying
		// is one the foot line says about the row a person is actually on, so
		// what is lost is a fact that was already somewhere better.
		note = ""
	}
	head := overlayLines(mark+" "+row.name, note, selected, false, hovered, width, pal)
	if row.connected && !selected && !hovered {
		head = connHeadInk(mark+" "+row.name, note, width, pal)
	}
	// And under it, whichever of the two things belongs to this row: the box
	// somebody is answering, or the one line saying what the account may do.
	// Never both — a box is open on a row that is not connected.
	if row.entry != nil {
		// The box hangs at the same indent an open account's capabilities do, so
		// a row that has opened into a question occupies the same interior column
		// as a row that has opened into a list of answers.
		lines, _, _ := keyBoxLines(row.entry, pal, width, overlayIndent, boxRows)
		// AND ONE LINE OF AIR UNDER IT. A row that has opened into a question is
		// a block, and the row after it is the catalog resuming; without the
		// blank the next service reads as the fourth line of somebody's key.
		return append(append(head, lines...), "")
	}
	if row.summary != "" {
		return append(head, connUnder(row.summary, selected, hovered, width, pal))
	}
	return head
}

// connHeadInk is the held account's row, in the one weight this tab spends that
// the settings list does not.
//
// It is built rather than handed to [overlayLines] because that function's whole
// contract is the sheet's tiering — dim at rest, ink and bold on the cursor —
// and the exception here is deliberate and is exactly one row kind wide. The
// geometry is the same geometry: two cells of lead, the name, the fact flush
// right, so a held row and a catalog row still start in the same column.
func connHeadInk(label, note string, width int, pal palette) []string {
	// THE TWO-LINE LAW AT [tierPhone] HOLDS HERE TOO. A held account's address is
	// the half of the row a person came to read, and a header that kept one line
	// on a phone would be cutting it in half to make room for a name it already
	// fits (palette.go's [overlayLines]).
	if overlayItemLines(width, note) == 2 {
		return []string{
			"  " + pal.bold(pal.ink(fit(label, width-2))),
			strings.Repeat(" ", overlayIndent) + pal.dim(fit(note, width-overlayIndent)),
		}
	}
	room := width - 2
	if note != "" {
		room -= ansi.StringWidth(note) + 1
	}
	label = fit(label, room)
	line := "  " + pal.bold(pal.ink(label))
	if note != "" {
		gap := width - 2 - ansi.StringWidth(label) - ansi.StringWidth(note)
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + pal.dim(note)
	}
	return []string{line}
}

// connUnder is a line hanging under the row above it — the summary, and nothing
// else today.
//
// It keeps the row's own backgrounds, because it is part of the row: the
// selection band spans it, the hover spans it, and the pointer over it is over
// the service ([sheet.listLines] gives both lines one owner). On the band it
// lifts from dim to muted for the reason the tail does (palette.go): dim grey on
// the selection background is grey on grey.
func connUnder(text string, selected, hovered bool, width int, pal palette) string {
	paint := pal.dim
	if selected {
		paint = pal.muted
	}
	line := strings.Repeat(" ", overlayIndent) + paint(fit(text, width-overlayIndent))
	// It takes the row's own step, and the row's own step is THE GROUND LADDER's
	// cursor one: `selected` here is the sheet's keyboard cursor and `hovered` is
	// the pointer, which the ladder holds to be ONE fact reached by two hands.
	// Nothing in a settings list is OPEN, so the selected step has no occupant on
	// this page.
	if selected || hovered {
		return pal.cursor(line, width)
	}
	return line
}

// connIndent is how far a capability sits under its account: four cells on top
// of the row's own lead, which puts the phrase two past the account's NAME —
// inside the block whose heading that name is, and clear of the glyph in front
// of it. It was two, which put a capability's first letter exactly under its
// account's, and made an open account read as a run of rows rather than as one.
const connIndent = "    "

// connNote is the one fact a service row carries on the right: the trip that is
// out, the account it is held as, or the line saying what connecting it buys.
//
// THE EMPTINESS LAW, twice. A connected account nobody named says nothing at all
// — not "connected", which the tick already said, and not a placeholder for a
// fact this surface does not have. A service with no blurb says nothing either.
func connNote(row *connRow) string {
	switch {
	case row.waiting && row.keyed:
		// No browser was opened for a key, so the row does not claim one. What
		// is happening is the far end being asked whether the key is any good —
		// the transcript's own sentence for the same wait (connect.go).
		return checkingWord
	case row.waiting:
		return browserWord
	case row.connected:
		if row.account != "" {
			return row.account
		}
		// A KEY CONNECTION HAS NO ACCOUNT AND SAYS SO BY SAYING NOTHING —
		// unless the key was named rather than pasted, and then the name of the
		// variable is the one honest fact the row has. It is not a secret: it is
		// a fact about the person's own machine, and it is the thing they need
		// when the connection stops working because they renamed it.
		return envRefWord(row.keyEnv)
	case row.entry != nil:
		// A box is open on this row and the box is the row's whole business. A
		// tag on the right saying "key" over a box asking for one is the surface
		// answering a question it has already been asked.
		return ""
	case row.tag != "":
		return row.tag
	}
	return row.blurb
}

// envRefWord is a named key as the row says it: "from $STRIPE_KEY", or nothing
// at all where the key was pasted whole.
func envRefWord(variable string) string {
	if strings.TrimSpace(variable) == "" {
		return ""
	}
	return "from $" + variable
}

// connAbout is the one sentence the CURSOR'S row explains itself with, drawn by
// the sheet's own description machinery ([sheet.listLines]) under the row and
// under no other.
//
// It is the blurb, and only on a catalog row that has given its right-hand side
// over to the tag. A held account explains nothing — it is connected, which is
// the whole of what a person wanted to know — and a short list is already
// drawing its blurbs where they belong.
func connAbout(row *connRow) string {
	if row.kind != connService || row.connected || row.entry != nil || row.tag == "" {
		return ""
	}
	return row.blurb
}

// capNote is the answer as the column draws it: the word, padded to the width of
// the longest of the three.
//
// THE PADDING IS WHAT MAKES IT A COLUMN. [overlayRow] sets a note flush against
// the right edge, which is right for a fact and wrong for an answer out of a
// closed set — `yes` and `ask first` ending in the same column start six cells
// apart, and a person scanning four rows for the one that says off has to read
// all four. Padded, the first letters line up and the column can be scanned
// without being read.
func capNote(state connect.CapabilityState) string {
	word := capWord(state)
	if word == "" {
		return ""
	}
	if pad := capWordCells - len(word); pad > 0 {
		word += strings.Repeat(" ", pad)
	}
	return word
}

// capWordCells is the width of that column: the longest of the three words.
const capWordCells = len(capAskWord)

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
// The note arrives PADDED to the answer column's width ([capNote]), so the word
// is read out of it rather than compared against it: a switch on the padded
// string would fall through to dim for every answer but the longest.
func capStateInk(pal palette, note string, selected bool) string {
	word := strings.TrimSpace(note)
	if selected {
		if word == capOffWord {
			return pal.muted(note)
		}
		return pal.ink(note)
	}
	switch word {
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
// THE THREE VERBS ARE TAUGHT ONE AT A TIME, on the row each of them belongs to
// — connect it, change what it may do, forget it — because a foot line that
// listed all three at once would be a legend a person reads past on their way to
// the one that applies to the row they are on. It is the hint slot's own bargain
// everywhere else on this surface: the sentence follows the cursor.
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
	if item.conn.entry != nil {
		if !item.conn.entry.secret {
			return connectKeyHint(item.conn.entry.name, item.conn.entry.blank)
		}
		// The one place this surface says the second thing a box will take. It
		// is said HERE and not in the box, because the box's own line has to say
		// the ordinary thing to the ordinary person, and this is the line that
		// exists to say the thing they could not have guessed.
		return "paste the key — or name a variable you keep it in, like " +
			envExampleFor(item.conn.entry.name)
	}
	if !item.conn.connected {
		switch {
		case item.conn.waiting:
			return "finish the sign-in in your browser"
		case item.conn.keyed:
			return "enter opens a box for the key you already hold"
		case item.conn.asks:
			return "enter opens a box for the site your account is on"
		}
		return "enter signs you in, in your browser"
	}
	if s.conn.expanded == item.conn.service {
		return "what this account may do, and the row that forgets it"
	}
	return "enter opens what this account may do"
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
	// A BOX HAS TAKEN THE KEYBOARD, so the legend is the box's two keys and not
	// the list's five. A line still offering ↑↓ and the tabs would be offering
	// keys that are going into a secret.
	if s.conn.entry != nil {
		return "enter connect · esc cancel"
	}
	act := "enter act"
	if item, ok := s.current(); ok && item.conn != nil {
		switch {
		case item.conn.kind == connCapability:
			act = "enter " + capYesWord + " · " + capAskWord + " · " + capOffWord
		case item.conn.kind == connDisconnect:
			act = "enter twice to disconnect"
		case item.conn.connected && s.conn.expanded == item.conn.service:
			act = "enter closes"
		case item.conn.connected:
			act = "enter opens"
		case item.conn.keyed:
			act = "enter takes your key"
		case item.conn.asks:
			act = "enter asks one thing"
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
		if row.keyed || row.asks {
			// THE BOX OPENS ON THE ROW, which is the whole of what this service
			// needed from this tab and did not have. Nothing leaves the process
			// yet: the person has not given an answer, and a browser trip's worth
			// of machinery for a question nobody has answered would be this tab
			// acting before it was told to.
			s.conn.entry = a.newConnEntry(row)
			s.msg = ""
			s.rebuildConnAt(row)
			return nil
		}
		// The sheet STAYS UP. /connect closes itself here because it is a list
		// that had one job; this is a page a person is reading, and closing the
		// whole settings sheet under somebody who asked for one account would be
		// answering a question by taking the room away. The row says it is
		// waiting, and the transcript's block is there when they leave.
		s.conn.pending, s.conn.pendingKey = row.service, false
		s.msg = ""
		s.rebuildConnAt(row)
		return a.beginConnect(row.service, a.serviceName(row.service, row.name), "")
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

// newConnEntry opens the key box for one row, over what the ENGINE says about
// that service: what to type, and where to find it. Both are read off the
// catalog this tab is already holding rather than asked for again — the list is
// re-read when the accounts change and never when a key is pressed.
func (a *app) newConnEntry(row *connRow) *keyEntry {
	name := a.serviceName(row.service, row.name)
	for _, status := range a.sheet.conn.catalog {
		if status.ID == row.service {
			return newKeyEntry(status.Service, name)
		}
	}
	return newKeyEntry(connect.Service{ID: row.service}, name)
}

// connEntryKey drives the key box while it is open on a row of this tab.
//
// It is the /connect panel's own two keys ([app.connectEntryKey]) with one
// difference, and the difference is what the two surfaces ARE: the panel closes
// itself on a submitted key because it is a list that had one job, and this
// leaves the sheet exactly where it was, because it is a page somebody is
// reading and the account they just connected is about to gain four rows on it.
//
// AN EMPTY BOX IS NOT AN ANSWER AND NOT A COMPLAINT. Nothing is waiting on this
// box — no session, no question — so backing out of it declines nothing and the
// cursor goes back on the row it was opened from.
func (a *app) connEntryKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.sheet
	entry := s.conn.entry
	back := &connRow{kind: connService, service: entry.id}
	switch msg.String() {
	case "esc":
		s.conn.entry = nil
		s.rebuildConnAt(back)

	case "enter":
		answer, id, name, secret := entry.value(), entry.id, entry.name, entry.secret
		s.conn.entry = nil
		if answer == "" {
			s.rebuildConnAt(back)
			return nil
		}
		// The row says it is being checked from here until the answer lands on
		// [app.connTabSettled], which is the same bargain the sign-in makes with
		// the same two fields.
		s.conn.pending, s.conn.pendingKey, s.msg = id, secret, ""
		s.rebuildConnAt(back)
		if secret {
			return a.beginConnectKey(id, name, answer)
		}
		return a.beginConnect(id, name, answer)

	default:
		entry.typeInto(msg)
	}
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
// THE KEY BOX IS NOT A RUNG HERE and is a nearer one than any of these: while it
// is open it owns every key on this sheet, esc included, and it backs itself out
// ([app.connEntryKey]). A second reading of that here would be a second place
// deciding what esc means to a box.
//
// It reports whether it took the key. Nothing here leaves the TAB — esc on a
// settings sheet closes the sheet, and a key that walked back to the previous
// page would be this one tab inventing a meaning for it.
func (a *app) connEsc() bool {
	s := &a.sheet
	if !a.at(pageSettings) || !s.onConnections() {
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
	keyed := s.conn.pendingKey
	s.conn.pending, s.conn.pendingKey = "", false
	if !mine || !a.at(pageSettings) || !s.onConnections() {
		return
	}
	s.reloadConnections()
	s.build()
	if why != "" {
		s.msg = why
		// A visible answer that was refused stays beside the sentence that says
		// why, but only while its row is still on this page and nobody has opened
		// another box since. The fresh box is empty, so the wrong site is not
		// presented as an answer the person still has to erase before trying again.
		if !keyed && (s.conn.entry == nil || s.conn.entry.id == service) && s.hasConnService(service) {
			entry := a.newConnEntry(&connRow{kind: connService, service: service})
			if entry.blank != "" {
				s.conn.entry = entry
			}
		}
	}
	back := service
	if s.conn.entry != nil {
		back = s.conn.entry.id
	}
	s.rebuildConnAt(&connRow{kind: connService, service: back})
	a.touch()
}

// hasConnService reports whether this build draws one service's row. A catalog
// row narrowed away by the filter cannot own a box or the keys that would type
// into it.
func (s *sheet) hasConnService(service string) bool {
	for _, item := range s.items {
		if item.conn != nil && item.conn.kind == connService && item.conn.service == service {
			return true
		}
	}
	return false
}

// connTabSettled is the outcome landing on the tab: a connected account gains
// its tick, its address and its capabilities, OPEN — a person who just signed in
// is a person about to look at what they signed up for. One that did not
// complete says so once, quietly, and the row goes back to a dim dot.
// why is what the engine said about a failure, and it is preferred to any
// sentence written here: a key the far end refused comes back with the far end's
// own words in it, and this tab is the only screen a person can read them on
// while a fullscreen sheet is up.
func (a *app) connTabSettled(service, name string, connected bool, why string) {
	s := &a.sheet
	mine := s.conn.pending == service
	keyed := s.conn.pendingKey
	s.conn.pending, s.conn.pendingKey = "", false
	if !a.at(pageSettings) || !s.onConnections() {
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
	switch {
	case connected:
		s.conn.expanded, s.conn.armed, s.msg = service, false, ""
	case strings.TrimSpace(why) != "":
		s.msg = why
	case keyed:
		// The key path's honest sentence. Nothing about the far end is claimed —
		// it may have refused the key, it may not have answered at all — and
		// "the connection didn't complete" is a true sentence about a browser
		// nobody came back from and a false one about this.
		s.msg = "that " + a.serviceName(service, name) + " key didn't work"
	default:
		s.msg = a.serviceName(service, name) + " connection didn't complete"
	}
	s.rebuildConnAt(&connRow{kind: connService, service: service})
	a.touch()
}
