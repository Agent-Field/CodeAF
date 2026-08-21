package tui3

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE AMBIENT BAND ON HOME: WHAT IS KEEPING AN EYE ON THINGS, UNDER THE PROJECT
// IT BELONGS TO.
//
// docs/AMBIENT.md Part 5 first proposed that home list a CONVERSATION by what
// it came to — a session that produced a reminder would be drawn as that
// reminder. That is superseded here, and the reason is a person's own reading
// of the screen: a conversation and the thing it left behind are two objects
// with two lives. The conversation goes quiet and folds away like every other;
// the item goes on firing for a year. Filing one under the other means home
// either hides a live item behind a fold about a dead chat, or keeps a chat
// alive on the list because something it made is still running.
//
// SO AN ITEM IS ITS OWN ROW, under its own project, in one band. It is the
// smallest thing that can be true of it: a glyph, the person's words, and one
// rollup saying where it stands. Opening it opens the conversation that asked
// for it, which is the provenance rule Part 5 got right — "why did I get this?"
// must always have an answer, and the answer is always a door.
//
// TRIAGE IS ONE ORDER ACROSS BOTH KINDS. Home's whole job is what wants you
// first, and a screen that sorted sessions by urgency and then stapled a band
// of items underneath would put an item that needs somebody below four
// conversations that do not. So an item that needs you, or is firing right now,
// sits WITH the conversations that do — above them, because a conversation is
// the bigger object and a person reads down into the small ones — and everything
// still waiting for its time sits under them, above the quiet fold. That is the
// same shape [session.sortSessions] already gives one project's conversations,
// applied to the two kinds together.
//
// AND THE BAND IS BOUNDED. Three rows, then one door saying how many more there
// are. Density on this screen is omission and never compression (home.go's
// header), and a project with eleven watches on it is a project whose watches
// are not the news.

// homeItemsShown is how many items a project draws before the rest collapse.
// Three and not [homeShown]'s four: the item band sits under a list of
// conversations that already spent four rows, and a section that takes as much
// room as the thing it is a footnote to has stopped being a footnote.
//
// IT IS A FLOOR AND NOT A CEILING, exactly as [homeShown] is. An item that needs
// somebody or is firing right now is drawn whatever the count says — those are
// the rows this screen exists for — and the collapse takes only the ones still
// waiting for their time.
const homeItemsShown = 3

// The sentences the band says. Each is quoted in internal/manual/chat/home.md
// exactly as it is spelled here.
const (
	// homeItemsFoldWord is the door at the foot of the band, with the count
	// before it. It says WHAT IS BEHIND IT in the words the product uses for the
	// thing — not "3 more items", which is a word for a row in a database.
	homeItemsFoldWord = " more keeping an eye"
	// homeItemsFewerWord is the same line holding the band open.
	homeItemsFewerWord = " fewer"
	// homeItemActions is the dim line at the foot of an item's card: the three
	// things this screen can do to one.
	homeItemActions = "enter open where it was asked · p pause · s stop"
	// homeItemNoDoor is what enter says on an item that was made at home and
	// never became a conversation ([standing.Origin.Exchange]). It is a fact and
	// not a refusal: there genuinely is no transcript to open, and saying so is
	// more use than a door that does nothing.
	homeItemNoDoor = "made from home — no conversation to open"
	// homeItemNoStore is what p and s say on a surface whose door wired no way
	// to write ([StandingSeam.Save] is nil).
	homeItemNoStore = "this window cannot change it"
	// homeItemPaused and homeItemStopped are the receipts for the two keys.
	homeItemPaused  = "paused"
	homeItemStopped = "stopped"
	// homeStoppedWhy is what a stopped item's document records as the reason,
	// in the person's own terms ([standing.Item.RetiredWhy] names this exact
	// spelling as one of its cases).
	homeStoppedWhy = "stopped by you"
	// homeKeepingWord is the status line's segment, with the count after it.
	homeKeepingWord = " keeping an eye on "
	// homeWatchLabel is /status's line, and the three things it can say.
	homeWatchLabel     = "keeping watch"
	homeWatchInstalled = "installed"
	homeWatchWindow    = "while a window is open"
	homeWatchLastWord  = "last check "
)

// StandingItemView is one item as a row or a card needs it: the document, plus
// the two facts the document does not hold.
//
// RUNNING AND NEWS ARE BOTH ABOUT NOW AND NEITHER IS ON THE ITEM. Whether a
// firing is in flight lives in the process doing it ([StandingSeam.Running]),
// and whether there is news for THIS person is a comparison against when they
// last spoke in the conversation that asked for the thing ([standNews]). Both
// are resolved once, where the row is built, so a card and the row it belongs
// to can never disagree.
type StandingItemView struct {
	Item    standing.Item
	Running bool
	News    bool
}

// standNews reports whether an item has fired since the person last spoke in
// the conversation that asked for it — home's `◆`.
//
// IT IS DERIVED AND NEVER ASSERTED, and the derivation is deliberately narrow:
// [session.SessionRow.At] is when the PERSON last spoke, which is the ordering
// law everywhere in this codebase, and an item that fired after that is
// something they have not been in the room for. An item whose origin
// conversation is not on this machine's list at all answers FALSE rather than
// guessing — there is nothing to compare against, and a glyph that meant "new"
// for everything with no provenance would be a mark that means nothing.
func (h *homeView) standNews(item standing.Item) bool {
	if item.LastFired.IsZero() {
		return false
	}
	transcript := strings.TrimSpace(item.Origin.Transcript)
	id := strings.TrimSpace(item.Origin.SessionID)
	if transcript == "" && id == "" {
		return false
	}
	for _, project := range h.world.Projects {
		for _, row := range project.Sessions {
			if (transcript != "" && row.Transcript == transcript) || (id != "" && row.ID == id) {
				return item.LastFired.After(row.At)
			}
		}
	}
	return false
}

// ── reading the store ───────────────────────────────────────────────────────

// standItems is one project's band, read through the seam and put in triage
// order. It answers nothing at all for a surface with the ambient side off,
// which is what makes the band absent rather than empty.
func (a *app) standItems(workspace string) []StandingItemView {
	if a.stands.Items == nil {
		return nil
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	items := a.stands.Items(workspace)
	if len(items) == 0 {
		return nil
	}
	views := make([]StandingItemView, 0, len(items))
	for _, item := range items {
		// A RETIRED ITEM IS NOT KEEPING AN EYE ON ANYTHING. It fired and went, or
		// it was stopped; either way it is a thing that HAPPENED, and home is a
		// glance at what is true now. The conversation that made it still holds
		// the whole record.
		if item.Status == standing.StatusRetired {
			continue
		}
		views = append(views, StandingItemView{
			Item:    item,
			Running: a.stands.Running != nil && a.stands.Running(item.ID),
			News:    a.home.standNews(item),
		})
	}
	standTriage(views)
	return views
}

// standTriage puts one project's items in the order home reads them: what needs
// somebody, then what is moving, then what has news, then everything else by
// when it last did anything.
//
// It is [session.sortSessions]'s ladder said about the other kind of row, and
// the two have to agree: the glyph and the row's position are one claim made
// twice, and an item sorted under `▲` below a row wearing `◦` is the screen
// arguing with itself.
func standTriage(views []StandingItemView) {
	sort.SliceStable(views, func(i, j int) bool {
		return standRank(views[i]) > standRank(views[j])
	})
}

// standRank is what one item's situation is worth. The bands are far enough
// apart that nothing inside one can climb into another, which is the same
// arrangement [homeState] uses for a conversation.
func standRank(view StandingItemView) int {
	switch {
	case view.Item.NeedsPerson != "":
		return 400
	case view.Running:
		return 300
	case view.News:
		return 200
	case view.Item.Status != standing.StatusActive:
		return 0
	}
	return 100
}

// standHot reports whether this item belongs ABOVE the conversations rather
// than under them — it needs somebody, or it is firing right now. Those are the
// two rows home exists to put in front of a person, and an item wearing either
// is exactly as urgent as a conversation wearing it.
func standHot(view StandingItemView) bool {
	return view.Item.NeedsPerson != "" || view.Running
}

// standSplit divides a project's items into the ones drawn and the ones counted.
// Every hot item is drawn whatever the cap says; the cold ones fill what is left
// of [homeItemsShown] and the remainder is the fold's number.
func standSplit(views []StandingItemView, open bool) (shown []StandingItemView, folded int) {
	if open {
		return views, len(views) - standDrawn(views)
	}
	for _, view := range views {
		if standHot(view) || len(shown) < homeItemsShown {
			shown = append(shown, view)
			continue
		}
		folded++
	}
	return shown, folded
}

// standDrawn is how many rows [standSplit] would draw with the band closed. An
// opened band still has to say how many it opened, because that line is the way
// back: a fold with no label is a fold nobody can find again (home.go's
// [homeView.split] states the same law about conversations).
func standDrawn(views []StandingItemView) int {
	drawn := 0
	for _, view := range views {
		if standHot(view) || drawn < homeItemsShown {
			drawn++
		}
	}
	return drawn
}

// ── the row ─────────────────────────────────────────────────────────────────

// StandingItemRow draws ONE standing item as one line of home's left column:
//
//	◦ every Monday at 9, draft the weekly update   Mondays 9am · last Mon
//	▲ keep main green                    needs your look · the fix touches …
//	● check the deploy                                        running · 4m
//
// IT IS PACKAGE-LEVEL AND EXPORTED ON PURPOSE, for [StandingCardRows]'s reason:
// home's errand box is a different lane's work, and the one thing that must not
// happen is a second row growing there. Anything in this package can draw the
// row with a width and a view.
//
// The row wears the calm every other row on this column wears — dim except
// under the cursor — with one exception, and it is the same exception the
// conversation rows make: `needs your look` is brought up out of the dim,
// because a screen whose whole job is triage cannot render its most urgent fact
// in the same grey as an age.
func StandingItemRow(a *app, view StandingItemView, width int, now time.Time, sel, hover bool) string {
	pal := a.pal
	label := standGlyph(view.Item, view.Running, view.News, pal.ascii) + " " + strings.TrimSpace(view.Item.Words)
	note := standRollup(view, now)
	// THE WORDS OUTRANK THE ROLLUP, and this is the one place on the column
	// where that has to be enforced. A conversation's tail is two or three words
	// (`waiting on you`, `12m`); an item's can be a whole sentence a run stopped
	// on, and [overlayRowTinted] gives the tail whatever it asks for and cuts the
	// label with what is left — which drew a row that was ALL rollup and no
	// words at all. The tail is clipped first; the card beside it has the
	// sentence in full.
	if room := width - standWordsFloor - 3; room > 0 && ansi.StringWidth(note) > room {
		note = fit(note, room)
	}
	return overlayRowTinted(label, note, standRowInk(view), sel, false, hover, width, pal)
}

// standWordsFloor is how many cells a row keeps for the person's own words
// whatever the rollup wants. Eighteen is about three words and an ellipsis —
// enough to tell two watches apart, which is the only job the label has on a
// column this narrow.
const standWordsFloor = 18

// standRowInk is how an item row's trailing fact is painted: the ordinary rule
// for everything, and the accent for the one row somebody has to do something
// about ([homeNoteInk] is the conversation half of exactly this).
func standRowInk(view StandingItemView) noteInk {
	if view.Item.NeedsPerson == "" {
		return nil
	}
	return func(pal palette, note string, selected bool) string {
		if selected {
			return pal.ink(note)
		}
		return pal.accent(note)
	}
}

// standRollup is an item's dim tail: where it stands, in one clause or two.
//
// WHICH TWO FACTS IT PICKS IS DECIDED BY THE KIND, and that is the honest cut. A
// reminder and a routine are not examined between now and Monday — they are DUE
// — so their tail is the cadence and when they last went off. A probe, a file
// watch and an idle watch ARE examined, on a clock, and the thing a person wants
// to know about one is that it looked and what it found: a watch that ran for
// thirty mornings and found nothing must read differently from one that never
// ran ([standing.Item]'s quiet half exists for exactly this line).
//
// THE EMPTINESS LAW REACHES EVERY CLAUSE. An item that has never fired says
// nothing about firing; one that has never been checked says nothing about
// checking; and an item with nothing at all to report is its cadence and no more.
func standRollup(view StandingItemView, now time.Time) string {
	item := view.Item
	switch {
	case item.NeedsPerson != "":
		return "needs your look · " + item.NeedsPerson
	case view.Running:
		if age := sinceAt(item.LastFired, now); age != "" {
			return "running · " + age
		}
		return "running"
	case item.Status == standing.StatusPaused:
		return homeItemPaused
	}
	words := strings.TrimSpace(item.When.Words)
	switch item.When.Kind {
	case standing.WhenProbe, standing.WhenFile, standing.WhenIdle:
		if item.LastChecked.IsZero() {
			return words
		}
		found := strings.TrimSpace(item.LastCheckLine)
		if found == "" {
			// "nothing" IS A FINDING and the most common one there is. It is the
			// difference between a watch that is working and a watch that never
			// ran, and it is the one clause on this row that must never be
			// dropped for being empty.
			found = "nothing"
		}
		return "checked " + sinceAt(item.LastChecked, now) + " ago · " + found
	}
	if item.LastFired.IsZero() {
		return words
	}
	return joinDot(words, "last "+standDayWord(item.LastFired, now))
}

// standDayWord is when something last happened, in the words a person uses for
// it: the weekday inside the last week — "last Mon" is a thing somebody says —
// and [sinceAt]'s ordinary age past that, where the weekday has stopped being a
// distinguishing fact.
func standDayWord(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if !now.IsZero() {
		if age := now.Sub(at); age >= 0 && age < 7*24*time.Hour {
			if age < 24*time.Hour {
				return sinceAt(at, now)
			}
			return at.Format("Mon")
		}
	}
	return sinceAt(at, now)
}

// joinDot joins two clauses with the separator this whole surface uses, and
// drops either of them when it is not there.
func joinDot(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	}
	return left + " · " + right
}

// standFoldWord is the band's door: how many are behind it, or how many it is
// holding open.
func standFoldWord(count int, folded bool) string {
	if !folded {
		return "…" + itoa(count) + homeItemsFewerWord
	}
	return "…" + itoa(count) + homeItemsFoldWord
}

// ── the card ────────────────────────────────────────────────────────────────

// StandingItemCard is home's right column when the cursor is on an item: the
// same card the conversation preview is, about the other kind of object.
//
//	every Monday at 9, draft the weekly update
//
//	aforge-v2 · ~/src/aforge-v2
//
//	Mondays at 9am
//	last went off Mon · the weekly update is in notes/week-34.md
//
//	4 runs · spent $0.08
//
//	enter open where it was asked · p pause · s stop
//
// IT IS BANDS AND NOT A FORM. The title is the person's own words and is the
// brightest text on the screen, matching the highlighted row across the gutter;
// everything under it is dim; a short frame drops whole bands from the bottom
// and never touches the title ([homeBands] does the assembling for both cards).
//
// THE EMPTINESS LAW IS THE WHOLE OF THE ARITHMETIC. An item that has never
// fired says nothing about firing, one that has spent nothing says nothing about
// money, and a card with nothing but a title and a place is exactly what a
// reminder made ten seconds ago is.
func StandingItemCard(a *app, view StandingItemView, project, dir string, width, room int, now time.Time) []string {
	if a == nil || width < 1 {
		return nil
	}
	pal := a.pal
	item := view.Item
	bands := [][]string{{pal.bold(pal.ink(fit(strings.TrimSpace(item.Words), width)))}}

	// THE BAND IS A PLACE, SO THE BAND IS A DOOR (pathlink.go) — the same anchor
	// the conversation card hangs on the same pair of words.
	place := project
	if dir != "" && dir != place {
		place = joinDot(place, dir)
	}
	if place != "" {
		bands = append(bands, []string{pal.dim(a.pathLink(dir, fit(place, width)))})
	}

	var state []string
	if item.NeedsPerson != "" {
		// The one thing on this card that is not a fact about the past. It is
		// somebody's to do, and it is the only line here that is not dim.
		for _, line := range wrap("needs your look · "+item.NeedsPerson, width) {
			state = append(state, pal.accent(line))
		}
	}
	if words := strings.TrimSpace(item.When.Words); words != "" {
		state = append(state, pal.dim(fit(words, width)))
	}
	for _, line := range standHistory(item, now) {
		for _, wrapped := range wrap(line, width) {
			state = append(state, pal.dim(wrapped))
		}
	}
	bands = append(bands, state)

	if facts := standFacts(item); facts != "" {
		bands = append(bands, []string{pal.dim(fit(facts, width))})
	}
	bands = append(bands, []string{pal.dim(fit(homeItemActions, width))})
	return homeBands(bands, room)
}

// standHistory is the two sentences an item can tell about itself: the last time
// it looked and what it found, and the last time it went off and what came of
// it. Either is omitted whole when it never happened.
func standHistory(item standing.Item, now time.Time) []string {
	var out []string
	if !item.LastChecked.IsZero() {
		found := strings.TrimSpace(item.LastCheckLine)
		if found == "" {
			found = "nothing"
		}
		out = append(out, "checked "+sinceAt(item.LastChecked, now)+" ago · "+found)
	}
	if !item.LastFired.IsZero() {
		out = append(out, joinDot("last went off "+standDayWord(item.LastFired, now),
			strings.TrimSpace(item.LastOutcome)))
	}
	return out
}

// standFacts is the dim arithmetic: how many times it has gone off and what that
// has cost. Zero draws NOTHING — not "0 runs", not "$0.00" — which is the
// emptiness law at its most literal.
func standFacts(item standing.Item) string {
	var parts []string
	if item.Runs > 0 {
		parts = append(parts, itoa(item.Runs)+plural(" run", item.Runs))
	}
	if item.SpentUSD > 0 {
		parts = append(parts, "spent "+dollars(item.SpentUSD))
	}
	return strings.Join(parts, " · ")
}

// ── the keys ────────────────────────────────────────────────────────────────

// homeItemEnter opens the conversation an item was asked for in — the same door
// a session row walks through, under the same rule about which project this
// window may open.
//
// PROVENANCE IS THE WHOLE POINT (docs/AMBIENT.md Part 5): "why did I get this?"
// must open the conversation that made it, and an item made at home that never
// became a conversation SAYS SO rather than offering a door onto nothing.
func (a *app) homeItemEnter(line homeLine) tea.Cmd {
	h := &a.home
	transcript := strings.TrimSpace(line.item.Origin.Transcript)
	if transcript == "" {
		h.say(homeItemNoDoor, "")
		return nil
	}
	if a.resume == nil || filepath.Clean(homeBucketOf(transcript)) != h.bucket {
		// The same limit a conversation in another project meets, said in the
		// same words and naming the same place to go (home.go's header).
		h.say(homeElsewhereWord+" · "+standWhere(line), strings.TrimSpace(line.item.Workspace))
		return nil
	}
	if transcript == a.file {
		a.closeHome()
		return nil
	}
	cmd, refusal := a.openSession(Session{File: transcript})
	if refusal != "" {
		h.say(refusal, "")
		return nil
	}
	a.closeHome()
	return cmd
}

// standWhere is where a person has to be to open an item's conversation.
func standWhere(line homeLine) string {
	if path := strings.TrimSpace(line.item.Workspace); path != "" {
		return path
	}
	return line.project
}

// homeItemWrite is `p` and `s` on an item row: pause it, or stop it for good.
//
// IT GOES THROUGH THE STORE AND THEN REDRAWS FROM THE STORE. The row is not
// repainted from what this function wishes were true — the write is attempted,
// the world is read again, and what the person sees is what the disk says. A row
// that showed `paused` over a store that refused the write would be the screen
// lying about the machine, which is the one thing this surface may not do.
func (a *app) homeItemWrite(line homeLine, status standing.Status) tea.Cmd {
	h := &a.home
	if a.stands.Save == nil {
		h.say(homeItemNoStore, "")
		return nil
	}
	item := line.item
	item.Status = status
	if status == standing.StatusRetired {
		item.RetiredWhy = homeStoppedWhy
	}
	if err := a.stands.Save(item); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	word := homeItemPaused
	if status == standing.StatusRetired {
		word = homeItemStopped
	}
	h.say(word+" · "+strings.TrimSpace(item.Words), "")
	a.refreshHome()
	return nil
}

// ── the status line ─────────────────────────────────────────────────────────

// keepingCount is how many items are keeping an eye on THIS WINDOW's project,
// and whether one of them is firing at this instant.
//
// IT IS THIS PROJECT AND NOT THE MACHINE. The status line is about the window
// (render.go's header: identity left, telemetry right, and both about the
// session), and a count that included another project's watches would be a
// number nobody could act on from here.
func (a *app) keepingCount() (int, bool) {
	if a.stands.Items == nil {
		return 0, false
	}
	workspace := strings.TrimSpace(a.workspace)
	if workspace == "" {
		return 0, false
	}
	// A SEGMENT IS ASKED ON EVERY FRAME AND THE STORE IS A DIRECTORY OF
	// DOCUMENTS. This is the status row's half of the law [app.homeHeld] states
	// about the lock: the frame turns thirty times a second while a turn is
	// running and the layout asks twice per frame, so the answer is kept and
	// re-read on home's own clock. A count that is three seconds old is a count
	// that is right — nothing standing changes faster than that, and the glyph
	// beside it is the part that moves.
	if now := a.now(); !a.keepAt.IsZero() && now.Sub(a.keepAt) < keepEvery {
		return a.keepN, a.keepFiring
	}
	count, firing := 0, false
	for _, item := range a.stands.Items(workspace) {
		if item.Status != standing.StatusActive {
			continue
		}
		count++
		if a.stands.Running != nil && a.stands.Running(item.ID) {
			firing = true
		}
	}
	a.keepN, a.keepFiring, a.keepAt = count, firing, a.now()
	return count, firing
}

// keepEvery is how stale that answer is allowed to be. It is home's own beat
// ([homeEvery]) and not a second number: both are "how often is it worth walking
// the disk to redraw something that changes on the order of minutes".
const keepEvery = homeEvery

// keepingSegment is the status line's ambient segment for the standing side:
//
//	◦ keeping an eye on 2
//
// NOTHING AT ALL WHEN THERE IS NOTHING, which is the emptiness law applied to a
// whole segment and the same call [app.ambientSegment] makes about jobs: a line
// that permanently reads "keeping an eye on 0" is a permanent reminder of the
// absence of a thing.
//
// The TEXT is stable while the count is; only the glyph moves, and it moves in
// [app.keepingWord] rather than here — a segment whose text changed thirty times
// a second would be a segment the fade ramp painted bright forever
// ([app.freshen] keys on exactly this string).
func (a *app) keepingSegment() string {
	count, _ := a.keepingCount()
	if count == 0 {
		return ""
	}
	glyph := standWaitGlyph
	if a.pal.ascii {
		glyph = standWaitASCII
	}
	return glyph + homeKeepingWord + itoa(count)
}

// keepingWord is that segment as it is DRAWN: the same width, with the glyph
// replaced by the spinner while a firing is actually in flight.
//
// PRESENCE, FELT AND NOT SEEN (docs/AMBIENT.md Part 3). The segment exists at
// all only when items do, and it MOVES only while one of them is doing
// something — which is the whole of what a person needs from it out of the
// corner of their eye. It turns on [spinnerStep]'s own grid, so it never beats
// against the state word at the other end of the line.
func (a *app) keepingWord() string {
	count, firing := a.keepingCount()
	if count == 0 {
		return ""
	}
	if !firing {
		return a.keepingSegment()
	}
	// The linear tier's objection to a spinner is the one it makes everywhere: a
	// claim repeated thirty times a second is heard thirty times a second by a
	// surface being read aloud. A still mark makes it once.
	glyph := tokens.Spinner(a.paints / spinnerStep)
	if a.linear || a.pal.ascii {
		glyph = glyphRunASCII
	}
	return glyph + homeKeepingWord + itoa(count)
}

// watchLine is /status's `keeping watch` fact, derived and never asserted.
//
//	keeping watch   installed · last check 4m
//	keeping watch   while a window is open
//
// WHAT IT WILL NOT SAY IS "off", AND THAT IS DELIBERATE.
// [standing.WatchStatus] carries whether the OS timer's definition on disk
// still matches this build, and nothing else — so "installed" and "not
// installed" are the two things that can be read off it honestly. A machine with
// no timer is still checked by any window that is open (docs/AMBIENT.md Part 3:
// a window takes the lock and runs the pass), so that is what the second answer
// says. A seam that reports it has no answer at all prints NO LINE, which is
// the honest third state rather than a word invented for it.
func (a *app) watchLine() (string, bool) {
	if a.stands.Watch == nil {
		return "", false
	}
	status, ok := a.stands.Watch()
	if !ok {
		return "", false
	}
	word := homeWatchWindow
	if status.Installed {
		word = homeWatchInstalled
	}
	if age := since(status.LastWake); age != "" {
		word += " · " + homeWatchLastWord + age
	}
	return word, true
}

// readStandBands re-reads every project's items into the view, keyed by bucket
// directory.
//
// IT IS ONE WALK PER READING OF THE WORLD and never one per frame. The column is
// drawn on every keystroke and every pointer movement, and the store is a
// directory of documents ([app.homeHeld] states the same law about the lock).
func (a *app) readStandBands() {
	// phone lane: the two facts the column's own build needs and cannot ask the
	// app for — which shape it is drawn in, and where the standing store lives
	// (homephone.go). They are settled here because this is the one call that
	// runs before every build of the list.
	a.home.phone, a.home.standRoot = a.homePhone(), a.standingHome()
	if a.stands.Items == nil {
		a.home.items, a.home.bare = nil, nil
		return
	}
	bands := make(map[string][]StandingItemView, len(a.home.world.Projects))
	known := make(map[string]bool, len(a.home.world.Projects))
	for _, project := range a.home.world.Projects {
		// THE PROJECT'S REAL PATH IS THE KEY THE STORE ANSWERS TO
		// ([standing.Item.Workspace] is the resolved workspace, never the bucket),
		// and a project nothing ever recorded a path for has nothing to ask about.
		// Machine-wide items — a reminder that belongs to no project — carry the
		// person's home directory as their workspace, which is the `~` project's
		// own path, so they land under `~` with no special case here.
		path := strings.TrimSpace(project.Path)
		if path == "" {
			continue
		}
		known[filepath.Clean(path)] = true
		if views := a.standItems(path); len(views) > 0 {
			bands[project.Dir] = views
		}
	}
	a.home.items = bands
	a.home.bare = a.readBareBands(bands, known)
}

// readBareBands is the OTHER kind of project: a workspace this machine holds
// standing things for and NO conversation at all.
//
// A watch is content. Home is read off the projects root, so a workspace whose
// only content is something keeping an eye on it had no heading, no band and no
// row — the person set a thing up and the screen that exists to show them what
// is true showed them nothing. So the two workspaces that can be in that state
// without anybody having spoken in them are asked about by name, and each one
// that answers with items becomes a heading of its own ([homeBare]).
//
// IT IS TWO NAMES AND NOT EVERY WORKSPACE ON THE MACHINE, and that is the seam
// rather than a choice: [StandingSeam.Items] answers for ONE workspace and
// nothing enumerates them, so the honest thing is to ask about the places an
// item can be made from a window that never held a conversation there — the
// home directory, which is where a machine-wide reminder's work runs
// ([standing.Item.Workspace]), and the directory THIS window is standing in.
func (a *app) readBareBands(bands map[string][]StandingItemView, known map[string]bool) []homeBare {
	var out []homeBare
	for _, path := range []string{errandHomeDir(), strings.TrimSpace(a.workspace)} {
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if known[clean] {
			continue
		}
		known[clean] = true
		views := a.standItems(path)
		if len(views) == 0 {
			continue
		}
		// THE KEY IS THE WORKSPACE ITSELF, because there is no bucket to key it
		// by — nothing was ever opened here. It is a path and a bucket is a path,
		// so the two can never collide: buckets live under the state root and a
		// workspace is where somebody works.
		bands[clean] = views
		out = append(out, homeBare{
			project: session.Project{
				Dir: clean, Path: path, Name: standBareName(path),
			},
			at: standBareAt(views),
		})
	}
	return out
}

// standBareName is what such a heading says: the workspace's last element, and
// `~` for the home directory itself. It is [session.projectName]'s answer said
// again on this side of the seam, because that function is unexported and a
// heading that named the same directory two different ways on two rows of one
// screen would be the screen arguing with itself.
func standBareName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if house, err := os.UserHomeDir(); err == nil && filepath.Clean(house) == filepath.Clean(path) {
		return "~"
	}
	if name := filepath.Base(path); name != "" && name != "." && name != string(filepath.Separator) {
		return name
	}
	return path
}

// standBareAt is where such a project sits in the recency order: the newest
// thing any of its items has done. It is the same question
// [session.Project.At] answers for a project with conversations in it — when
// was anything last true here — asked of the only rows this one has.
func standBareAt(views []StandingItemView) time.Time {
	var newest time.Time
	for _, view := range views {
		for _, at := range []time.Time{view.Item.LastChecked, view.Item.LastFired, view.Item.Created} {
			if at.After(newest) {
				newest = at
			}
		}
	}
	return newest
}
