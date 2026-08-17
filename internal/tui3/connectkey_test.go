package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// CONNECTING AN ACCOUNT THAT HAS NO SIGN-IN PAGE: the key, from the two sides a
// person meets it — the offer the session raises, and the catalog they open on
// purpose.

// askKeyEvent is the session's offer for a service connected by a pasted key.
func askKeyEvent(id, service, name string) session.Event {
	ev := askConnectEvent(id, service, name)
	ev.NeedsKey = true
	return ev
}

// theKey is long enough that the mask has to give way on a narrow frame, which
// is the case the count exists for.
const theKey = "secret-01234567890123456789012345678901234567890123456789"

// ── 1. the offer, and the box it opens ──────────────────────────────────────

// SAYING YES OPENS A BOX IN THE OFFER'S OWN ROW, and says nothing to the session
// yet: there is no browser to hand off to, so the answer is not given until the
// key is.
func TestAKeyOfferOpensABoxWhereTheAnswersWere(t *testing.T) {
	agent, a, opened := connectApp(t)
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")))

	// Before the yes it is the offer, unchanged: the question is the same
	// question whichever way the connecting happens.
	rows := connectBlock(a)
	if !strings.Contains(rows[connectOfferRow], "[enter]") {
		t.Fatalf("a key offer is not the offer: %q", rows[connectOfferRow])
	}

	drive(t, a, key("enter"))
	rows = connectBlock(a)
	if len(rows) != 3 {
		t.Fatalf("the box changed the block's height: %d rows\n%s", len(rows),
			strings.Join(rows, "\n"))
	}
	if got := a.connectAskHeight(); got != len(rows) {
		t.Fatalf("the block is %d rows and counts itself as %d", len(rows), got)
	}
	if !strings.Contains(rows[connectOfferRow], "paste your Notion key") {
		t.Fatalf("the box does not say what to put in it: %q", rows[connectOfferRow])
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("the session was answered before the key was given: %+v", agent.resolved)
	}
	if len(*opened) != 0 {
		t.Fatalf("a key service opened a browser: %v", *opened)
	}
	if !a.entering() {
		t.Fatal("the offer is not collecting a key")
	}
}

// THE KEY IS NEVER ON THE SCREEN. Not a character of it, typed or pasted — what
// is drawn is a bullet each and how many there are.
func TestAKeyNeverReachesTheScreen(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey + "\n"})

	row := connectBlock(a)[connectOfferRow]
	if strings.Contains(row, "secret") || strings.Contains(row, theKey) {
		t.Fatalf("the key is on screen: %q", row)
	}
	if !strings.Contains(row, "••") {
		t.Fatalf("the key was not masked at all: %q", row)
	}
	// The trailing newline the clipboard brought is dropped rather than kept: a
	// space inside a secret is a secret that does not work.
	if !strings.Contains(row, itoa(len(theKey))) {
		t.Fatalf("the count is not what was pasted: %q", row)
	}
	// And the whole frame, not just the row this test laid out.
	if screen := frame(a); strings.Contains(screen, theKey) {
		t.Fatal("the key reached the frame")
	}
}

// THE MASK GIVES WAY AND THE COUNT DOES NOT. A key is longer than any row this
// surface draws, so the bullets are cut and the number stays whole.
func TestALongKeyKeepsItsCountWhenTheMaskIsCut(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 40
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey})

	row := connectBlock(a)[connectOfferRow]
	if strings.Count(row, "•") >= len(theKey) {
		t.Fatalf("the mask was not cut to the frame: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), itoa(len(theKey))) {
		t.Fatalf("the count did not survive the cut: %q", row)
	}
}

// ENTER SENDS THE KEY BACK, once, with the token the offer came with — and the
// conversation says what is happening while the far end is asked.
func TestSubmittingAKeyAnswersTheSessionAndWaits(t *testing.T) {
	agent, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("enter"))

	if a.asksConnect() {
		t.Fatal("the block survived the key it asked for")
	}
	if len(agent.resolved) != 1 {
		t.Fatalf("the session heard %d answers", len(agent.resolved))
	}
	got := agent.resolved[0]
	if !got.keyed || got.id != "c1" || got.key != theKey {
		t.Fatalf("the session was answered %+v", got)
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "checking your Notion key") {
		t.Fatalf("nothing says the key is being checked:\n%s", screen)
	}
	// It does not claim a browser it never opened.
	if strings.Contains(screen, "browser") {
		t.Fatalf("a key flow talked about a browser:\n%s", screen)
	}
	if !a.connectAnimating() {
		t.Fatal("a key in flight does not ask for frames")
	}
}

// AN EMPTY BOX IS A DECLINE and not a complaint, and it writes nothing down —
// the same law the browser offer's "not now" keeps.
func TestAnEmptyKeyBoxIsADecline(t *testing.T) {
	agent, a, _ := connectApp(t)
	before := len(a.entries)
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"), key("enter"))

	if a.asksConnect() {
		t.Fatal("the block is still up")
	}
	if len(agent.resolved) != 1 || !agent.resolved[0].keyed || agent.resolved[0].key != "" {
		t.Fatalf("an empty box answered %+v", agent.resolved)
	}
	if len(a.entries) != before {
		t.Fatalf("a decline wrote %d rows into the transcript", len(a.entries)-before)
	}
}

// ESC BACKS OUT TO NOT NOW, in one press, and what was typed goes with it.
func TestEscBacksOutOfTheKeyBox(t *testing.T) {
	agent, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("esc"))

	if a.asksConnect() {
		t.Fatal("esc left the offer on screen")
	}
	if len(agent.resolved) != 1 || !agent.resolved[0].keyed || agent.resolved[0].key != "" {
		t.Fatalf("esc answered %+v, want a decline with no key", agent.resolved)
	}
	if len(a.entries) != 0 {
		t.Fatal("backing out wrote something down")
	}
}

// THE BOX TAKES THE LETTERS THE OFFER ANSWERED WITH. y and n are two characters
// of a key, and a box that read them as answers would decline halfway through
// one somebody typed by hand.
func TestTheKeyBoxTakesTheOffersOwnLetters(t *testing.T) {
	agent, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, key("y"), key("n"), key("z"))

	if !a.entering() {
		t.Fatal("a letter of the key answered the offer")
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("the session was answered while a key was being typed: %+v", agent.resolved)
	}
	drive(t, a, key("enter"))
	if len(agent.resolved) != 1 || agent.resolved[0].key != "ynz" {
		t.Fatalf("the box collected %+v", agent.resolved)
	}
}

// A KEY THAT DID NOT WORK SAYS SO, in the key's own words: nothing was
// abandoned in a browser, so nothing claims one was.
func TestAKeyThatDidNotWorkSaysSoQuietly(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("enter"))
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectDone, Service: "notion", Failed: true,
	}))

	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "the Notion key didn't work") {
		t.Fatalf("a refused key said something else:\n%s", screen)
	}
	if strings.Contains(screen, glyphBad) {
		t.Fatalf("a refused key is drawn as a broken call:\n%s", screen)
	}
	if a.connectAnimating() {
		t.Fatal("a settled key still asks for frames")
	}
}

// AND A KEY THAT WORKED SETTLES INTO THE ONE TICK LINE, in place — the same
// sentence a browser sign-in leaves behind, because the outcome is the same
// outcome.
func TestAKeyThatWorkedSettlesIntoTheTickLine(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, askKeyEvent("c1", "notion", "Notion")), key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("enter"))
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectDone, Service: "notion", Account: "jane@example.com",
	}))

	if blocks := connectEntries(a); len(blocks) != 1 {
		t.Fatalf("the outcome wrote %d blocks", len(blocks))
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Notion connected as jane@example.com") {
		t.Fatalf("the tick line is not what settled:\n%s", screen)
	}
	if strings.Contains(screen, "checking your") {
		t.Fatalf("the waiting line survived the outcome:\n%s", screen)
	}
}

// ── 2. the catalog ──────────────────────────────────────────────────────────

// bigCatalog is a service list at the scale this wave is about: one connected
// account, and enough on offer that the list stops being walkable.
func bigCatalog(n int) []connect.Status {
	rows := []connect.Status{{
		Service:   connect.Service{ID: "slack", Name: "Slack", Blurb: "your channels"},
		Connected: true, Account: "jane@example.com",
	}}
	for i := 0; i < n; i++ {
		id := "svc" + itoa(i)
		auth := authBrowser
		if i%2 == 1 {
			auth = authKey
		}
		rows = append(rows, connect.Status{Service: connect.Service{
			ID: id, Name: "Service " + itoa(i), Blurb: "what " + id + " is for", Auth: auth,
		}})
	}
	// One row with a name worth searching for, and a key rather than a sign-in.
	rows = append(rows, connect.Status{Service: connect.Service{
		ID: "notion", Name: "Notion", Blurb: "your pages", Auth: authKey,
	}})
	return rows
}

// CONNECTED FIRST, THEN A BLANK, THEN THE REST — whatever order the engine hands
// the list over in.
func TestTheConnectPanelPutsConnectedAccountsFirst(t *testing.T) {
	_, a, _ := panelApp(t, []connect.Status{
		{Service: connect.Service{ID: "a", Name: "Ay"}},
		{Service: connect.Service{ID: "b", Name: "Bee"}, Connected: true, Account: "b@x"},
		{Service: connect.Service{ID: "c", Name: "Cee"}},
		{Service: connect.Service{ID: "d", Name: "Dee"}, Connected: true, Account: "d@x"},
	})
	a.width = 100
	typeLine(t, a, "/connect")

	lines := plainOverlay(a)
	if len(lines) < 5 {
		t.Fatalf("the panel drew %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for at, want := range []string{"Bee", "Dee", "", "Ay", "Cee"} {
		if want == "" {
			if strings.TrimSpace(lines[at]) != "" {
				t.Fatalf("line %d is not the gap between the sections: %q", at, lines[at])
			}
			continue
		}
		if !strings.Contains(lines[at], want) {
			t.Fatalf("line %d is %q, want %s:\n%s", at, lines[at], want,
				strings.Join(lines, "\n"))
		}
	}
	// The blank belongs to no service, so a press on it does nothing at all.
	if a.connPanel.owner[2] != -1 {
		t.Fatalf("the gap answers to row %d", a.connPanel.owner[2])
	}
	// And the cursor opens on the first row of the list, which is an account
	// this profile holds.
	if row, ok := a.connPanel.choice(); !ok || row.ID != "b" {
		t.Fatalf("the cursor opened on %+v", row)
	}
}

// A SHORT LIST IS READ AND A LONG ONE IS SEARCHED. Under the floor there is no
// filter box at all; over it the box under the list becomes one.
func TestTheConnectPanelCollapsesToAFilterAtCatalogScale(t *testing.T) {
	_, a, _ := panelApp(t, twoServices)
	a.width = 100
	typeLine(t, a, "/connect")
	if a.connPanel.filtering {
		t.Fatal("a list of two grew a filter box")
	}
	drive(t, a, key("esc"))

	_, a, _ = panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	if !a.connPanel.filtering {
		t.Fatal("a catalog of forty is still a list you walk")
	}
	box, _, _ := a.inputBlock(a.width)
	if len(box) != 1 || !strings.Contains(plain(box[0]), "filter") {
		t.Fatalf("the box under the list is not a filter: %q", box)
	}
	// It never draws more than the ceiling, however many services there are.
	if lines := plainOverlay(a); len(lines) > connectRowsMax {
		t.Fatalf("the panel drew %d lines over three hundred rows", len(lines))
	}
}

// AN AVAILABLE ROW ON A CATALOG SAYS HOW IT IS CONNECTED, and a connected one
// still says the account. One fact per row, and the one that is true of it.
func TestTheConnectPanelSaysHowEachServiceConnects(t *testing.T) {
	_, a, _ := panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	for _, r := range "notion" {
		drive(t, a, key(string(r)))
	}

	lines := plainOverlay(a)
	screen := strings.Join(lines, "\n")
	if !strings.Contains(screen, "Notion") || !strings.Contains(screen, keyTag) {
		t.Fatalf("the key row does not say it wants a key:\n%s", screen)
	}
	// A browser row carries the other tag. "s" matches every service in the
	// catalog, so the sign-in half is on screen too.
	drive(t, a, key("ctrl+u"))
	for _, r := range "service 0" {
		drive(t, a, key(string(r)))
	}
	if screen = strings.Join(plainOverlay(a), "\n"); !strings.Contains(screen, signInTag) {
		t.Fatalf("a browser row does not say it opens a sign-in:\n%s", screen)
	}
}

// TYPING NARROWS THE LIST, and enter acts on the row the narrowing left under
// the cursor — not on the row that was there before it was typed.
func TestTheConnectFilterNarrowsAndEnterTakesWhatIsLeft(t *testing.T) {
	_, a, conns := panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	wide := len(a.connPanel.hits)

	for _, r := range "notion" {
		drive(t, a, key(string(r)))
	}
	if narrow := len(a.connPanel.hits); narrow >= wide || narrow != 1 {
		t.Fatalf("the filter left %d of %d rows, want one", narrow, wide)
	}
	if row, ok := a.connPanel.choice(); !ok || row.ID != "notion" {
		t.Fatalf("the cursor is on %+v after narrowing", row)
	}

	// Enter on it opens the key box, in the filter's own place — and the panel
	// stays up under it, because the key is given here.
	drive(t, a, key("enter"))
	if a.connPanel.entry == nil || !a.connPanel.open {
		t.Fatal("enter on a key row did not open the box over the list")
	}
	box, _, _ := a.inputBlock(a.width)
	if !strings.Contains(plain(box[0]), "paste your Notion key") {
		t.Fatalf("the box does not say what to put in it: %q", plain(box[0]))
	}

	drive(t, a, tea.PasteMsg{Content: theKey})
	if line := plain(box[0]); strings.Contains(line, "secret") {
		t.Fatal("the box drew the key")
	}
	if box, _, _ = a.inputBlock(a.width); strings.Contains(plain(box[0]), theKey) {
		t.Fatalf("the box drew the key: %q", plain(box[0]))
	}
	drive(t, a, key("enter"))

	if len(conns.keyed) != 1 || conns.keyed[0].id != "notion" || conns.keyed[0].key != theKey {
		t.Fatalf("the panel handed over %+v", conns.keyed)
	}
	if a.connPanel.open {
		t.Fatal("the panel stayed up over the connection it started")
	}
	// The waiting block goes up first and the answer settles it IN PLACE — the
	// harness runs the command before it draws, so what is left is one block,
	// which is the whole of what "in place" means.
	if blocks := connectEntries(a); len(blocks) != 1 {
		t.Fatalf("the key path wrote %d blocks", len(blocks))
	}
	if !strings.Contains(strings.Join(plainRows(a), "\n"), glyphConnected+" Notion connected") {
		t.Fatalf("the key did not settle into the tick line:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// AN EMPTY BOX IN THE PANEL IS NOT AN ANSWER, and esc puts the person back on
// the row they opened it from: nothing is waiting on this box, so backing out of
// it declines nothing.
func TestBackingOutOfThePanelsKeyBoxKeepsTheList(t *testing.T) {
	_, a, conns := panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	for _, r := range "notion" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"), key("enter"))
	if len(conns.keyed) != 0 {
		t.Fatalf("an empty box connected %+v", conns.keyed)
	}
	if !a.connPanel.open || a.connPanel.entry != nil {
		t.Fatal("an empty box did not put the list back")
	}

	drive(t, a, key("enter"), tea.PasteMsg{Content: theKey}, key("esc"))
	if len(conns.keyed) != 0 {
		t.Fatalf("esc connected %+v", conns.keyed)
	}
	if !a.connPanel.open || a.connPanel.entry != nil {
		t.Fatal("esc did something other than close the box")
	}
	if row, ok := a.connPanel.choice(); !ok || row.ID != "notion" {
		t.Fatalf("esc left the cursor on %+v", row)
	}
}

// ESC UNDOES ONE THING AT A TIME: the query first, the panel second.
func TestEscClearsTheConnectFilterBeforeItCloses(t *testing.T) {
	_, a, _ := panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	for _, r := range "notion" {
		drive(t, a, key(string(r)))
	}

	drive(t, a, key("esc"))
	if !a.connPanel.open {
		t.Fatal("the first esc closed the panel instead of the query")
	}
	if a.connPanel.filter.String() != "" {
		t.Fatalf("the query survived the esc: %q", a.connPanel.filter.String())
	}
	if len(a.connPanel.hits) < 40 {
		t.Fatalf("the list did not widen back: %d rows", len(a.connPanel.hits))
	}
	drive(t, a, key("esc"))
	if a.connPanel.open {
		t.Fatal("the second esc did not close the panel")
	}
}

// THE POINTER REACHES THE SAME ROWS THE CURSOR DOES, gap and all: a press on a
// key row opens the box, and a press on the blank between the sections does
// nothing rather than acting on whichever row it was nearest.
func TestTheConnectPanelAnswersThePointerAtCatalogScale(t *testing.T) {
	_, a, _ := panelApp(t, bigCatalog(40))
	a.width = 100
	typeLine(t, a, "/connect")
	for _, r := range "notion" {
		drive(t, a, key(string(r)))
	}
	// Lay the frame out, so the panel's rows have a place on it.
	_ = frame(a)

	y, gap := -1, -1
	_, marks, _, _ := a.chrome(a.width)
	for at, mark := range marks {
		if mark.kind != chromeOverlay {
			continue
		}
		row := a.height - len(marks) + at
		switch {
		case mark.index < len(a.connPanel.owner) && a.connPanel.owner[mark.index] >= 0 && y < 0:
			y = row
		case mark.index < len(a.connPanel.owner) && a.connPanel.owner[mark.index] < 0 && gap < 0:
			gap = row
		}
	}
	if y < 0 {
		t.Fatal("no row of the frame belongs to a service")
	}
	a.connectPanelPress(y)
	if a.connPanel.entry == nil {
		t.Fatal("a press on a key row did not open the box")
	}
	drive(t, a, key("esc"))
	if gap >= 0 {
		a.connectPanelPress(gap)
		if a.connPanel.entry != nil || !a.connPanel.open {
			t.Fatal("a press on a line belonging to nothing acted anyway")
		}
	}
}

// THE FILTERED SLICE IS BUILT WHEN THE QUERY CHANGES AND NEVER WHEN THE FRAME IS
// PAINTED. A catalog is three hundred rows and a paint runs thirty times a
// second; a panel that re-ranked on the way to the screen would be doing that
// arithmetic nine thousand times a second to draw ten rows.
func TestTheConnectPanelDoesNotRefilterOnEveryPaint(t *testing.T) {
	_, a, _ := panelApp(t, bigCatalog(300))
	a.width = 100
	typeLine(t, a, "/connect")

	// A sentinel the panel could only have overwritten by ranking again.
	a.connPanel.hits = []int{0, 1, 2}
	a.connPanel.cursor, a.connPanel.top = 0, 0
	for i := 0; i < 5; i++ {
		_ = frame(a)
		_ = a.overlayHeight()
	}
	if got := a.connPanel.hits; len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("painting re-filtered the list: %v", got)
	}
}

// AND THE PANEL IS EXACTLY AS TALL AS IT SAID IT WOULD BE, gap included: a block
// a line short of its own count leaves the frame a line short of the terminal.
func TestTheConnectPanelDrawsTheHeightItAsksFor(t *testing.T) {
	for _, width := range []int{100, 44} {
		_, a, _ := panelApp(t, bigCatalog(40))
		a.width = width
		typeLine(t, a, "/connect")
		want := a.overlayHeight()
		if got := len(a.overlayRows(a.width, want)); got != want {
			t.Fatalf("at width %d the panel asked for %d lines and drew %d", width, want, got)
		}
	}
}
