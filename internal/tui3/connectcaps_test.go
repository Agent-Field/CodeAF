package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE CONNECTIONS TAB (connectcaps.go), from the four sides a person meets it:
// the rows it draws, the one service it opens at a time, the word that is the
// control, and the two things that leave the process — a sign-in and a
// disconnect.

// googleCaps is the set the surface is built against, in the contract's own
// words and with the contract's own defaults (looking is yes, acting asks).
var googleCaps = []connect.Capability{
	{ID: "mail-read", Phrase: "read your mail"},
	{ID: "mail-send", Phrase: "send mail as you", Acts: true},
	{ID: "calendar-read", Phrase: "read your calendar"},
	{ID: "calendar-write", Phrase: "put things on your calendar", Acts: true},
}

// capsApp is a settings sheet over a scripted set of connections, opened on the
// Connections tab.
func capsApp(t *testing.T, rows []connect.Status) (*app, *fakeConnections) {
	t.Helper()
	a, _ := sheetApp(t)
	a.width = 90
	// The rows are COPIED. The fake writes back into them when a service is
	// disconnected, and a table shared between tests is a test reading the
	// account another one dropped.
	conns := &fakeConnections{
		rows: append([]connect.Status(nil), rows...),
		caps: map[string][]connect.Capability{"google": googleCaps},
	}
	a.conns = conns
	a.openSettings()
	toConnections(t, a)
	return a, conns
}

// toConnections walks the tab bar onto the last tab, the way → does.
func toConnections(t *testing.T, a *app) {
	t.Helper()
	for i := 0; i < len(settingTabs); i++ {
		if settingTabs[a.sheet.tab] == tabConnections {
			return
		}
		drive(t, a, key("right"))
	}
	t.Fatalf("the tab bar never reached %s", tabConnections)
}

// twoAccounts is one connected service with capabilities and one that is not.
var twoAccounts = []connect.Status{
	{
		Service:   connect.Service{ID: "google", Name: "Google", Blurb: "your calendar and mail"},
		Connected: true, Account: "jane@example.com",
	},
	{Service: connect.Service{ID: "slack", Name: "Slack", Blurb: "your channels"}},
}

// capRowAt is the tab's row for a capability, or -1.
func capRowAt(a *app, service, capability string) int {
	for i, item := range a.sheet.items {
		if item.conn != nil && item.conn.kind == connCapability &&
			item.conn.service == service && item.conn.capID == capability {
			return i
		}
	}
	return -1
}

// serviceRowAt is the tab's row for a service, or -1.
func serviceRowAt(a *app, service string) int {
	return kindRowAt(a, connService, service)
}

func kindRowAt(a *app, kind connRowKind, service string) int {
	for i, item := range a.sheet.items {
		if item.conn != nil && item.conn.kind == kind && item.conn.service == service {
			return i
		}
	}
	return -1
}

// ── 1. the tab itself ───────────────────────────────────────────────────────

// THE TAB IS ON THE BAR, it is last, and it belongs to no registry row — which
// is what keeps the completeness gate honest about the five that do.
func TestTheConnectionsTabIsOnTheBarAndOwnsNoRegistryRow(t *testing.T) {
	found, at := false, -1
	for i, tab := range settingTabs {
		if tab == tabConnections {
			found, at = true, i
		}
	}
	if !found {
		t.Fatalf("the tab bar has no %s tab: %v", tabConnections, settingTabs)
	}
	if at != len(settingTabs)-1 {
		t.Fatalf("%s sits at %d of %d, want the end of the bar", tabConnections, at, len(settingTabs))
	}
	registry := config.NewSettings(config.SettingsOptions{ProfileDir: t.TempDir()})
	for _, row := range registry.Rows() {
		if meta, ok := settingMetaFor(row); ok && meta.tab == tabConnections {
			t.Fatalf("registry row %q was placed on the accounts tab", row.Key)
		}
	}
	// It is on the bar a person can actually read, and it is drawn as a chip
	// like every other tab.
	bar := plain(sheetTabBar(90, len(settingTabs)-1, newPalette(tokens.ANSI256, false)))
	if !strings.Contains(bar, tabConnections) {
		t.Fatalf("the tab bar does not carry the tab: %q", bar)
	}
}

// EACH ROW IS A GLYPH, A NAME AND THE ONE FACT IT HAS: the account where there
// is one, the blurb where there is not — the /connect panel's own reading, in
// the settings sheet's own rows.
func TestTheConnectionsTabDrawsWhatEachAccountHas(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	screen := strings.Join(sheetLabels(a), "\n")

	if !strings.Contains(screen, glyphConnected+" Google") ||
		!strings.Contains(screen, "jane@example.com") {
		t.Fatalf("the connected account is not a tick, a name and an address:\n%s", screen)
	}
	if !strings.Contains(screen, glyphIdle+" Slack") || !strings.Contains(screen, "your channels") {
		t.Fatalf("the unconnected service is not a dot, a name and its line:\n%s", screen)
	}
	// The connected row says the account and NOT the blurb: one fact per row,
	// and the one that is true of this profile.
	if strings.Contains(screen, "your calendar and mail") {
		t.Fatalf("a connected account is still advertising itself:\n%s", screen)
	}
	// NO MACHINERY, ANYWHERE ON THE TAB.
	for _, banned := range []string{"OAuth", "oauth", "token", "scope", "grant", "redirect", "API"} {
		if strings.Contains(screen, banned) {
			t.Fatalf("the tab says %q:\n%s", banned, screen)
		}
	}
}

// A SURFACE WITH NO DOOR ONTO CONNECTIONS SAYS SO, and a build with nothing to
// connect says the other sentence — the two the /connect panel already says.
func TestTheConnectionsTabSaysWhenThereIsNothingToShow(t *testing.T) {
	a, _ := sheetApp(t)
	a.openSettings()
	toConnections(t, a)
	if !sheetHas(a, connectUnavailableWord) {
		t.Fatalf("a surface with no accounts door said nothing:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}

	b, _ := capsApp(t, nil)
	if !sheetHas(b, noServicesWord) {
		t.Fatalf("a build with nothing to connect said nothing:\n%s",
			strings.Join(sheetLabels(b), "\n"))
	}
}

// ── 2. one open at a time ───────────────────────────────────────────────────

// ENTER OPENS A SERVICE IN PLACE, and opening a second closes the first.
func TestTheConnectionsTabOpensOneServiceAtATime(t *testing.T) {
	second := connect.Status{
		Service:   connect.Service{ID: "slack", Name: "Slack"},
		Connected: true, Account: "jane@work",
	}
	a, conns := capsApp(t, []connect.Status{twoAccounts[0], second})
	conns.caps["slack"] = []connect.Capability{{ID: "post", Phrase: "post as you", Acts: true}}
	a.sheet.conn.expanded = ""
	a.sheet.build()

	if capRowAt(a, "google", "mail-read") >= 0 {
		t.Fatal("the tab opened with a service already expanded")
	}
	a.sheet.cursor = serviceRowAt(a, "google")
	drive(t, a, key("enter"))
	if capRowAt(a, "google", "mail-read") < 0 {
		t.Fatalf("enter did not open the account:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	if !sheetHas(a, "read your mail") || !sheetHas(a, "put things on your calendar") {
		t.Fatalf("the open account does not say what it may do:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}

	a.sheet.cursor = serviceRowAt(a, "slack")
	drive(t, a, key("enter"))
	if capRowAt(a, "slack", "post") < 0 {
		t.Fatal("enter did not open the second account")
	}
	if capRowAt(a, "google", "mail-read") >= 0 {
		t.Fatalf("two accounts are open at once:\n%s", strings.Join(sheetLabels(a), "\n"))
	}

	// And enter on the open one closes it: the same key, both ways.
	drive(t, a, key("enter"))
	if capRowAt(a, "slack", "post") >= 0 {
		t.Fatal("enter on the open account did not close it")
	}
}

// A LONE CONNECTED ACCOUNT IS ALREADY OPEN. There is nothing else the one-open
// rule could be protecting, and a tab that showed one line and hid the only
// thing it exists to say would be a tab nobody would open twice.
func TestALoneAccountOpensWithTheTab(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	if capRowAt(a, "google", "mail-read") < 0 {
		t.Fatalf("the only connected account did not open with the tab:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	// And a person who closes it has closed it — a rebuild does not re-open it.
	a.sheet.cursor = serviceRowAt(a, "google")
	drive(t, a, key("enter"))
	a.sheet.build()
	if capRowAt(a, "google", "mail-read") >= 0 {
		t.Fatal("the tab re-opened an account somebody closed")
	}
}

// ── 3. the word is the control ──────────────────────────────────────────────

// THE STATE IS ONE QUIET WORD, right-aligned where every other row of this sheet
// puts its value — and there is no widget around it.
func TestACapabilityRowIsAPhraseAndAWord(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	at := capRowAt(a, "google", "mail-send")
	if at < 0 {
		t.Fatal("the open account drew no capability rows")
	}
	line := plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	if !strings.Contains(line, "send mail as you") || !strings.Contains(line, capAskWord) {
		t.Fatalf("the row is not a phrase and an answer: %q", line)
	}
	// The defaults the contract states: looking is yes, acting asks first.
	read := capRowAt(a, "google", "mail-read")
	readLine := plain(strings.Join(a.sheet.rowLines(a.sheet.items[read], false, false, a.width, a.pal), "\n"))
	if !strings.Contains(readLine, capYesWord) {
		t.Fatalf("reading did not default to %q: %q", capYesWord, readLine)
	}
	// NO CHECKBOX, NO BRACKET, NO TOGGLE — the word is the control.
	for _, banned := range []string{"[", "]", "( )", "(x)", "✓ yes", "☐", "☑"} {
		if strings.Contains(line+readLine, banned) {
			t.Fatalf("a capability row drew a widget %q: %q", banned, line+readLine)
		}
	}
}

// ENTER WALKS yes → ask first → off → yes AND WRITES AT ONCE, with the exact
// arguments the engine takes. There is no save step, so there is nothing to
// forget to press.
func TestCyclingACapabilityWritesItImmediately(t *testing.T) {
	a, conns := capsApp(t, twoAccounts)
	at := capRowAt(a, "google", "mail-read")
	a.sheet.cursor = at

	drive(t, a, key("enter"))
	drive(t, a, key("enter"))
	drive(t, a, key("enter"))

	want := []capChange{
		{service: "google", capability: "mail-read", state: connect.StateAsk},
		{service: "google", capability: "mail-read", state: connect.StateOff},
		{service: "google", capability: "mail-read", state: connect.StateYes},
	}
	if len(conns.set) != len(want) {
		t.Fatalf("three presses wrote %d answers: %+v", len(conns.set), conns.set)
	}
	for i, one := range want {
		if conns.set[i] != one {
			t.Fatalf("press %d wrote %+v, want %+v", i+1, conns.set[i], one)
		}
	}
	// The cursor did not move off the row it was answering.
	if a.sheet.cursor != capRowAt(a, "google", "mail-read") {
		t.Fatal("the cursor walked away from the row it was cycling")
	}
	if !sheetHas(a, capYesWord) {
		t.Fatalf("the row does not read as the answer it holds:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
}

// A REFUSAL IS SHOWN AND THE WORD GOES BACK. Nothing pretends to have been
// written.
func TestARefusedCapabilityKeepsTheWordItHad(t *testing.T) {
	a, conns := capsApp(t, twoAccounts)
	conns.setErr = errConnect("Google is not answering right now")
	at := capRowAt(a, "google", "mail-read")
	a.sheet.cursor = at

	drive(t, a, key("enter"))
	if a.sheet.msg != "Google is not answering right now" {
		t.Fatalf("the refusal was swallowed: %q", a.sheet.msg)
	}
	row := a.sheet.items[capRowAt(a, "google", "mail-read")].conn
	if row.state != connect.StateYes {
		t.Fatalf("the row moved to %q on a write that failed", row.state)
	}
	if !strings.Contains(plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n")), capYesWord) {
		t.Fatal("the shown word was not restored")
	}
}

// THE THREE WORDS ARE THREE WEIGHTS and no colour beyond the sheet's own ramp:
// a person scanning the column can tell them apart without reading them.
func TestTheThreeAnswersAreThreeWeights(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	yes := capStateInk(pal, capYesWord, false)
	ask := capStateInk(pal, capAskWord, false)
	off := capStateInk(pal, capOffWord, false)
	if yes == ask || ask == off || yes == off {
		t.Fatalf("two answers are painted the same: %q %q %q", yes, ask, off)
	}
	// And on the selected row nothing is left dim, because dim on the band is
	// grey on grey.
	if capStateInk(pal, capOffWord, true) == off {
		t.Fatal("the selected row left its answer at the dim tier")
	}
}

// ── 4. the two things that leave the process ────────────────────────────────

// AN UNCONNECTED SERVICE CONNECTS FROM HERE — the same sign-in /connect starts,
// started from the row a person is already looking at, with the sheet still up.
func TestAnUnconnectedRowStartsTheSignInInPlace(t *testing.T) {
	a, conns := capsApp(t, twoAccounts)
	a.sheet.cursor = serviceRowAt(a, "slack")
	drive(t, a, key("enter"))

	if len(conns.began) != 1 || conns.began[0] != "slack" {
		t.Fatalf("the tab began %v, want one slack sign-in", conns.began)
	}
	if !a.sheet.open || settingTabs[a.sheet.tab] != tabConnections {
		t.Fatal("the sheet walked away from the account it was connecting")
	}

	// And what comes back settles ON THE ROW: the tick, the address, and what
	// the account may now do.
	conns.rows[1].Connected, conns.rows[1].Account = true, "jane@work"
	conns.caps["slack"] = []connect.Capability{{ID: "post", Phrase: "post as you", Acts: true}}
	// The engine's own answer, arriving on the surface's message loop — the
	// same one /connect's sign-in settles on (connectpanel.go).
	a.sheet.conn.pending = "slack"
	drive(t, a, connectResultMsg{
		service: "slack", name: "Slack",
		status: connect.Status{
			Service:   connect.Service{ID: "slack", Name: "Slack"},
			Connected: true, Account: "jane@work",
		},
	})

	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Slack") || !strings.Contains(screen, "jane@work") {
		t.Fatalf("the row did not gain its tick and its address:\n%s", screen)
	}
	if capRowAt(a, "slack", "post") < 0 {
		t.Fatalf("the account it just connected did not open:\n%s", screen)
	}
}

// WHILE THE TRIP IS OUT THE ROW SAYS SO, in the sentence the transcript's own
// block says.
func TestAWaitingRowSaysWhereTheSignInIs(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	a.sheet.conn.pending = "slack"
	a.sheet.build()
	at := serviceRowAt(a, "slack")
	line := plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	if !strings.Contains(line, browserWord) {
		t.Fatalf("the waiting row says %q", line)
	}
	// A sign-in that never reached a browser says so once, quietly, and the row
	// goes back to a dot.
	a.connTabStopped("slack", "this machine has no way to open a browser")
	if a.sheet.msg != "this machine has no way to open a browser" {
		t.Fatalf("the stopped sign-in said %q", a.sheet.msg)
	}
	if a.sheet.conn.pending != "" {
		t.Fatal("the row is still waiting for a trip that never left")
	}
}

// DISCONNECTING IS THE LAST ROW OF AN OPEN ACCOUNT AND TAKES TWO PRESSES.
func TestDisconnectingFromTheTabAsksFirst(t *testing.T) {
	a, conns := capsApp(t, twoAccounts)
	at := kindRowAt(a, connDisconnect, "google")
	if at < 0 {
		t.Fatalf("an open account has no disconnect row:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	a.sheet.cursor = at

	drive(t, a, key("enter"))
	if len(conns.dropped) != 0 {
		t.Fatalf("one press disconnected %v", conns.dropped)
	}
	if !sheetHas(a, disconnectArmedWord) {
		t.Fatalf("the row disconnected silently:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	// esc un-asks the question rather than closing the sheet.
	drive(t, a, key("esc"))
	if !a.sheet.open || sheetHas(a, disconnectArmedWord) {
		t.Fatal("esc did not drop the question standing on the row")
	}

	drive(t, a, key("enter"), key("enter"))
	if len(conns.dropped) != 1 || conns.dropped[0] != "google" {
		t.Fatalf("the second press disconnected %v", conns.dropped)
	}
	screen := strings.Join(sheetLabels(a), "\n")
	if strings.Contains(screen, "jane@example.com") || strings.Contains(screen, "read your mail") {
		t.Fatalf("the tab still shows the account it dropped:\n%s", screen)
	}
	if !strings.Contains(screen, glyphIdle+" Google") {
		t.Fatalf("the dropped account is not back to a dim dot:\n%s", screen)
	}
}

// ── 5. the keyboard and the pointer ─────────────────────────────────────────

// ESC BACKS OUT ONE LAYER AT A TIME: the search, then the question on a row,
// then the open account, then the sheet.
func TestEscOnTheConnectionsTabBacksOutInOrder(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)

	// The filter first: it is the sheet's own rung, and it wins over everything
	// this tab has open.
	drive(t, a, key("s"), key("l"))
	if !a.sheet.searching() {
		t.Fatal("typing did not reach the filter box")
	}
	drive(t, a, key("esc"))
	if !a.sheet.open || a.sheet.searching() {
		t.Fatal("esc did not drop the filter first")
	}
	if settingTabs[a.sheet.tab] != tabConnections {
		t.Fatal("filtering walked off the accounts tab")
	}

	a.sheet.cursor = kindRowAt(a, connDisconnect, "google")
	drive(t, a, key("enter")) // arm
	drive(t, a, key("esc"))
	if !a.sheet.open || a.sheet.conn.armed {
		t.Fatal("esc did not un-ask the disconnect question first")
	}
	drive(t, a, key("esc"))
	if !a.sheet.open {
		t.Fatal("esc closed the sheet over an open account")
	}
	if capRowAt(a, "google", "mail-read") >= 0 {
		t.Fatal("esc did not collapse the open account")
	}
	drive(t, a, key("esc"))
	if a.sheet.open {
		t.Fatal("esc did not close the sheet once there was nothing open on it")
	}
}

// THE POINTER HAS THE SAME REACH AS THE KEYBOARD: a click selects, a second
// click on the same row acts — an account opens, an answer cycles.
func TestTheConnectionsTabTakesTheMouse(t *testing.T) {
	a, conns := capsApp(t, twoAccounts)
	a.sheet.conn.expanded = ""
	a.sheet.build()

	press := func(item int) {
		t.Helper()
		_, hits, _, _ := a.sheetFrame(a.width, a.height)
		for y, hit := range hits {
			if hit.kind == sheetHitRow && hit.index == item {
				drive(t, a, clickAt(4, y))
				return
			}
		}
		t.Fatalf("item %d was not drawn", item)
	}

	// The cursor starts on the first row, so the pointer is aimed somewhere
	// else first: what is under test is that a press SELECTS before it acts.
	a.sheet.cursor = serviceRowAt(a, "slack")
	at := serviceRowAt(a, "google")
	press(at) // selects
	if a.sheet.cursor != at {
		t.Fatalf("the click selected item %d, want %d", a.sheet.cursor, at)
	}
	if capRowAt(a, "google", "mail-read") >= 0 {
		t.Fatal("one press opened an account the pointer was only passing over")
	}
	press(at) // acts
	if capRowAt(a, "google", "mail-read") < 0 {
		t.Fatal("a second click did not open the account")
	}

	answer := capRowAt(a, "google", "mail-read")
	press(answer)
	press(answer)
	if len(conns.set) != 1 || conns.set[0].state != connect.StateAsk {
		t.Fatalf("the pointer wrote %+v", conns.set)
	}
}

// THE HOVER REACHES THESE ROWS TOO, which is what makes them look pressable
// before they are pressed (hover.go).
func TestTheConnectionsTabAnswersTheHover(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	_, hits, _, _ := a.sheetFrame(a.width, a.height)
	want := capRowAt(a, "google", "mail-send")
	for y, hit := range hits {
		if hit.kind == sheetHitRow && hit.index == want {
			a.sheetHover(y)
			if a.hoveredSheetRow() != want {
				t.Fatalf("the pointer over a capability row hovered %d", a.hoveredSheetRow())
			}
			return
		}
	}
	t.Fatal("the capability row was not drawn")
}

// THE FOOT LINE ANSWERS THE ONE THING THE ROWS CANNOT: where a change goes, and
// what enter would do on a row nobody has connected yet.
func TestTheConnectionsTabSaysWhereItSaves(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	a.sheet.cursor = capRowAt(a, "google", "mail-read")
	if !strings.Contains(a.sheet.footNote(), "saved") {
		t.Fatalf("the foot line never says the answer is kept: %q", a.sheet.footNote())
	}
	a.sheet.cursor = serviceRowAt(a, "slack")
	if !strings.Contains(a.sheet.footNote(), "signs you in") {
		t.Fatalf("an unconnected row does not say what enter does: %q", a.sheet.footNote())
	}
	if !strings.Contains(a.sheet.keysLine(), "esc") {
		t.Fatalf("the key legend lost its door: %q", a.sheet.keysLine())
	}
}

// ── 6. the catalog: categories, and the filter that is the navigation ───────

// catalog is a build that knows about more services than a person can scan:
// two held accounts, and a shelf of others under five category words.
var catalog = []connect.Status{
	{
		Service:   connect.Service{ID: "google", Name: "Google", Category: "productivity"},
		Connected: true, Account: "jane@example.com",
	},
	{
		Service:   connect.Service{ID: "slack", Name: "Slack", Category: "productivity"},
		Connected: true, Account: "jane@work",
	},
	{Service: connect.Service{ID: "stripe", Name: "Stripe", Category: "billing"}},
	{Service: connect.Service{ID: "chargebee", Name: "Chargebee", Category: "billing"}},
	{Service: connect.Service{ID: "recurly", Name: "Recurly", Category: "billing"}},
	{Service: connect.Service{ID: "salesforce", Name: "Salesforce", Category: "crm"}},
	{Service: connect.Service{ID: "hubspot", Name: "HubSpot", Category: "crm"}},
	{Service: connect.Service{ID: "zendesk", Name: "Zendesk", Category: "support"}},
	{Service: connect.Service{ID: "intercom", Name: "Intercom", Category: "support"}},
	{Service: connect.Service{ID: "github", Name: "GitHub", Category: "developer"}},
	{Service: connect.Service{ID: "linear", Name: "Linear", Category: "developer"}},
	{Service: connect.Service{ID: "zoom", Name: "Zoom", Category: "calls & meetings"}},
	{Service: connect.Service{ID: "oddity", Name: "Oddity"}},
}

// headings is every label the tab drew, in order.
func headings(a *app) []string {
	out := []string{}
	for _, item := range a.sheet.items {
		if item.heading() {
			out = append(out, item.head)
		}
	}
	return out
}

// serviceOrder is every service row the tab drew, in order.
func serviceOrder(a *app) []string {
	out := []string{}
	for _, item := range a.sheet.items {
		if item.conn != nil && item.conn.kind == connService {
			out = append(out, item.conn.service)
		}
	}
	return out
}

// WHAT YOU HAVE IS FLAT AND FIRST; WHAT YOU COULD HAVE IS BY CATEGORY. The
// headings are labels and not rows: the cursor steps over them, and a click on
// one does nothing.
func TestTheCatalogIsHeldAccountsFlatThenCategories(t *testing.T) {
	a, _ := capsApp(t, catalog)
	a.sheet.conn.expanded = ""
	a.sheet.build()

	order := serviceOrder(a)
	if len(order) < 2 || order[0] != "google" || order[1] != "slack" {
		t.Fatalf("the held accounts are not the first two rows: %v", order)
	}
	// No "productivity" heading: both of its services are connected, and a
	// category whose every member is held is a heading over nothing.
	want := []string{"billing", "calls & meetings", "crm", "developer", "support", otherWord}
	got := headings(a)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the categories read %v, want %v", got, want)
	}
	// The two accounts a person holds carry NO heading over them, and the
	// service that declared no category lands under "other" rather than under a
	// blank one.
	for _, head := range got {
		if strings.TrimSpace(head) == "" {
			t.Fatal("the tab drew an empty heading")
		}
	}

	// A heading is not a place the cursor can be, so ↓ from the last held
	// account lands on a service and never on a word.
	a.sheet.cursor = serviceRowAt(a, "slack")
	a.sheet.move(1)
	if item := a.sheet.items[a.sheet.cursor]; item.heading() {
		t.Fatal("the cursor landed on a category label")
	}
	// And with no filter on, a heading is a word and nothing else: no counts.
	for _, head := range got {
		if strings.ContainsAny(head, "0123456789") {
			t.Fatalf("an unfiltered heading is carrying a count: %q", head)
		}
	}
}

// A CATALOG THAT SAYS NOTHING ABOUT CATEGORIES IS THE FLAT LIST IT ALWAYS WAS.
// The grouping keys off the field being filled, so the order this branch and
// the one that fills it land in cannot break anything.
func TestACatalogWithoutCategoriesStaysFlat(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	if got := headings(a); len(got) != 0 {
		t.Fatalf("a catalog with no categories grew headings: %v", got)
	}
}

// THE FILTER MATCHES THE CATEGORY AS WELL AS THE NAME, which is the whole point
// of it: "billing" reaches three services that do not contain the word.
func TestTheCatalogFilterMatchesCategoryAndName(t *testing.T) {
	a, _ := capsApp(t, catalog)
	for _, r := range "billing" {
		drive(t, a, key(string(r)))
	}
	order := serviceOrder(a)
	want := map[string]bool{"stripe": true, "chargebee": true, "recurly": true}
	if len(order) != len(want) {
		t.Fatalf("the filter left %v, want the three billing services", order)
	}
	for _, id := range order {
		if !want[id] {
			t.Fatalf("the filter kept %q, which is not filed under billing", id)
		}
	}
	// While a filter is on, a heading may carry how many it left.
	head := headings(a)
	if len(head) != 1 || !strings.HasPrefix(head[0], "billing") || !strings.Contains(head[0], "3") {
		t.Fatalf("the filtered heading reads %v", head)
	}

	// A NAME HIT OUTRANKS A CATEGORY HIT. "s" reaches Slack, Salesforce and
	// Stripe by name and Zendesk by nothing; the name hits lead.
	drive(t, a, key("ctrl+u"))
	for _, r := range "stri" {
		drive(t, a, key(string(r)))
	}
	order = serviceOrder(a)
	if len(order) == 0 || order[0] != "stripe" {
		t.Fatalf("typing the name did not put it first: %v", order)
	}

	// And a filter that reaches nothing says so where the rows were.
	drive(t, a, key("ctrl+u"))
	for _, r := range "zzz" {
		drive(t, a, key(string(r)))
	}
	if !sheetHas(a, "nothing matches") {
		t.Fatalf("a filter that matched nothing said nothing:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	// esc gives the whole catalog back.
	drive(t, a, key("esc"))
	if len(serviceOrder(a)) != len(catalog) {
		t.Fatalf("esc did not restore the catalog: %v", serviceOrder(a))
	}
}

// THE HELD ACCOUNTS STAY PINNED AT THE TOP OF A FILTERED LIST. What a person
// already has is not a search result.
func TestAFilteredCatalogKeepsTheHeldAccountsFirst(t *testing.T) {
	a, _ := capsApp(t, catalog)
	a.sheet.conn.expanded = ""
	a.sheet.build()
	for _, r := range "productivity" {
		drive(t, a, key(string(r)))
	}
	order := serviceOrder(a)
	if len(order) < 2 || order[0] != "google" || order[1] != "slack" {
		t.Fatalf("the held accounts were ranked among the rest: %v", order)
	}
}

// THE FILTER IS OFFERED WHERE IT IS NEEDED and is quiet where it is not: a
// legend that teaches a keyboard for six rows is a legend nobody reads.
func TestTheFilterIsOfferedAtCatalogScale(t *testing.T) {
	big, _ := capsApp(t, catalog)
	if !strings.Contains(big.sheet.keysLine(), "type to filter") {
		t.Fatalf("a catalog did not offer its filter: %q", big.sheet.keysLine())
	}
	small, _ := capsApp(t, twoAccounts)
	if strings.Contains(small.sheet.keysLine(), "type to filter") {
		t.Fatalf("a two-row list is teaching the keyboard: %q", small.sheet.keysLine())
	}
}

// THE GROUPING IS COMPUTED ONCE PER READ, not per frame and not per keystroke:
// the catalog is asked for when the tab opens and when an account changes, and
// a hundred repaints ask for nothing.
func TestTheCatalogIsReadOnceAndNotPerFrame(t *testing.T) {
	a, conns := capsApp(t, catalog)
	was := conns.reads
	for i := 0; i < 20; i++ {
		a.sheetFrame(a.width, a.height)
	}
	for _, r := range "billing" {
		drive(t, a, key(string(r)))
	}
	if conns.reads != was {
		t.Fatalf("drawing and filtering re-read the catalog %d times", conns.reads-was)
	}
	// An account that CHANGES is the one thing that does re-read it.
	a.sheet.conn.pending = "stripe"
	a.connTabSettled("stripe", "Stripe", true, "")
	if conns.reads == was {
		t.Fatal("a connected account did not refresh the catalog")
	}
}

// ── 7. the key, given on the tab ────────────────────────────────────────────
//
// The UX wave's half of this file: a service that wants a key is answered HERE,
// on the row, in the same box /connect opens (connectcaps.go, connect.go's
// [keyBoxLines]).

// keyCatalog is a build with one held account and a shelf of key services: one
// that says where its key lives, one that says nothing, one that wants a domain
// before the key, and one connected from a named variable.
var keyCatalog = []connect.Status{
	{
		Service:   connect.Service{ID: "google", Name: "Google", Category: "productivity"},
		Connected: true, Account: "jane@example.com",
	},
	{
		Service: connect.Service{
			ID: "stripe", Name: "Stripe", Category: "billing", Auth: connect.AuthKey,
			KeyHint: "https://dashboard.stripe.com/apikeys",
		},
		Connected: true, KeyEnv: "STRIPE_KEY",
	},
	{Service: connect.Service{
		ID: "chargebee", Name: "Chargebee", Category: "billing", Auth: connect.AuthKey,
		Blurb:   "Reach your Chargebee account, with a key you already hold.",
		KeyAsk:  "Give the domain and then the key, one space between them.",
		KeyHint: "https://apidocs.chargebee.com",
	}},
	{Service: connect.Service{
		ID: "recurly", Name: "Recurly", Category: "billing", Auth: connect.AuthKey,
		Blurb: "Reach your Recurly account, with a key you already hold.",
	}},
	{Service: connect.Service{
		ID: "notion", Name: "Notion", Category: "productivity",
		Blurb: "Notion's own tools, signed in in your browser.",
	}},
	{Service: connect.Service{ID: "linear", Name: "Linear", Category: "developer", Blurb: "your issues"}},
	{Service: connect.Service{ID: "sentry", Name: "Sentry", Category: "developer", Blurb: "your errors"}},
	{Service: connect.Service{ID: "apollo", Name: "Apollo", Category: "sales & outreach", Auth: connect.AuthKey, Blurb: "your prospects"}},
	{Service: connect.Service{ID: "hunter", Name: "Hunter", Category: "sales & outreach", Auth: connect.AuthKey, Blurb: "your addresses"}},
	{Service: connect.Service{ID: "dixa", Name: "Dixa", Category: "support", Auth: connect.AuthKey, Blurb: "your conversations"}},
	{Service: connect.Service{ID: "freshdesk", Name: "Freshdesk", Category: "support", Auth: connect.AuthKey, Blurb: "your tickets"}},
}

// keyApp is the tab over that catalog, with nothing expanded.
func keyApp(t *testing.T) (*app, *fakeConnections) {
	t.Helper()
	a, conns := capsApp(t, keyCatalog)
	a.sheet.conn.expanded = ""
	a.sheet.build()
	return a, conns
}

// onRow puts the cursor on one service's row.
func onRow(t *testing.T, a *app, service string) {
	t.Helper()
	at := serviceRowAt(a, service)
	if at < 0 {
		t.Fatalf("the tab drew no row for %s:\n%s", service, strings.Join(sheetLabels(a), "\n"))
	}
	a.sheet.cursor = at
}

// ENTER ON A KEY SERVICE OPENS THE BOX, HERE, and nothing leaves the process:
// the person has not answered anything yet.
func TestAKeyServiceOpensItsBoxOnTheTab(t *testing.T) {
	a, conns := keyApp(t)
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))

	if a.sheet.conn.entry == nil || a.sheet.conn.entry.id != "recurly" {
		t.Fatalf("enter on a key row opened %+v", a.sheet.conn.entry)
	}
	if len(conns.began) != 0 {
		t.Fatalf("a key service opened a browser: %v", conns.began)
	}
	if len(conns.keyed) != 0 {
		t.Fatalf("an unanswered box connected %+v", conns.keyed)
	}
	if !sheetHas(a, "paste your Recurly key") {
		t.Fatalf("the box does not say what to put in it:\n%s", strings.Join(sheetLabels(a), "\n"))
	}
	// The sheet stays up and stays on this page.
	if !a.sheet.open || settingTabs[a.sheet.tab] != tabConnections {
		t.Fatal("the box took the page away")
	}
	// And a browser service still opens a browser from the same key.
	drive(t, a, key("esc"))
	onRow(t, a, "notion")
	drive(t, a, key("enter"))
	if len(conns.began) != 1 || conns.began[0] != "notion" {
		t.Fatalf("a browser row began %v", conns.began)
	}
	if a.sheet.conn.entry != nil {
		t.Fatal("a browser row opened a key box")
	}
}

// THE KEY IS NEVER ON THE SCREEN, on this surface as on the other two: a bullet
// each and how many there are.
func TestTheTabsKeyBoxNeverDrawsTheKey(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey + "\n"})

	screen := strings.Join(sheetLabels(a), "\n")
	if strings.Contains(screen, "secret") || strings.Contains(screen, theKey) {
		t.Fatalf("the key is on the page:\n%s", screen)
	}
	if !strings.Contains(screen, "••") {
		t.Fatalf("the key was not masked at all:\n%s", screen)
	}
	// The newline the clipboard brought is dropped rather than kept: the count
	// is what was pasted, and a space inside a secret is a secret that does not
	// work.
	if !strings.Contains(screen, itoa(len(theKey))) {
		t.Fatalf("the count is not what was pasted:\n%s", screen)
	}
	// And the search box did not take a character of it.
	if a.sheet.query.String() != "" {
		t.Fatalf("the key reached the search box: %q", a.sheet.query.String())
	}
}

// ENTER HANDS THE KEY TO THE ENGINE, and what comes back settles ON THE ROW:
// the tick, and the account open on what it may do.
func TestSubmittingAKeyOnTheTabConnectsInPlace(t *testing.T) {
	a, conns := keyApp(t)
	conns.caps["recurly"] = []connect.Capability{
		{ID: "read", Phrase: "read what is in this account"},
		{ID: "act", Phrase: "act in this account in your name", Acts: true},
	}
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("enter"))

	if len(conns.keyed) != 1 || conns.keyed[0].id != "recurly" || conns.keyed[0].key != theKey {
		t.Fatalf("the tab handed over %+v", conns.keyed)
	}
	if a.sheet.conn.entry != nil {
		t.Fatal("the box survived the key it asked for")
	}
	if !a.sheet.open || settingTabs[a.sheet.tab] != tabConnections {
		t.Fatal("the sheet walked away from the account it was connecting")
	}
	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Recurly") {
		t.Fatalf("the row did not gain its tick:\n%s", screen)
	}
	if capRowAt(a, "recurly", "read") < 0 {
		t.Fatalf("the account it just connected did not open:\n%s", screen)
	}
}

// AN EMPTY BOX IS NOT AN ANSWER, and esc backs out of the box before it backs
// out of anything else — the tab's own rung of the sheet's esc ladder.
func TestTheTabsKeyBoxBacksOutWithoutConnecting(t *testing.T) {
	for _, out := range []string{"enter", "esc"} {
		a, conns := keyApp(t)
		onRow(t, a, "recurly")
		drive(t, a, key("enter"))
		if out == "esc" {
			drive(t, a, tea.PasteMsg{Content: theKey})
		}
		drive(t, a, key(out))

		if len(conns.keyed) != 0 {
			t.Fatalf("%s connected %+v", out, conns.keyed)
		}
		if a.sheet.conn.entry != nil {
			t.Fatalf("%s left the box open", out)
		}
		if !a.sheet.open {
			t.Fatalf("%s closed the whole sheet", out)
		}
		// The cursor is back on the row the box was opened from.
		if item, ok := a.sheet.current(); !ok || item.conn == nil || item.conn.service != "recurly" {
			t.Fatalf("%s left the cursor somewhere else", out)
		}
	}
}

// A KEY THE FAR END REFUSED SAYS SO IN THE FAR END'S OWN WORDS, on the foot
// line — which is the only place a person can read them while a fullscreen
// sheet is up.
func TestARefusedKeyOnTheTabSaysWhy(t *testing.T) {
	a, conns := keyApp(t)
	conns.keyErr = errConnect("Recurly did not accept that key: 401 Unauthorized")
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))
	drive(t, a, tea.PasteMsg{Content: theKey}, key("enter"))

	if !strings.Contains(a.sheet.msg, "did not accept that key") {
		t.Fatalf("the refusal was swallowed: %q", a.sheet.msg)
	}
	if a.sheet.conn.pending != "" {
		t.Fatal("the row is still waiting for an answer that landed")
	}
}

// WHILE THE KEY IS OUT THE ROW SAYS SO, and it does not claim a browser it
// never opened.
func TestAWaitingKeyRowDoesNotClaimABrowser(t *testing.T) {
	a, _ := keyApp(t)
	a.sheet.conn.pending, a.sheet.conn.pendingKey = "recurly", true
	a.sheet.build()
	at := serviceRowAt(a, "recurly")
	line := plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	if !strings.Contains(line, checkingWord) {
		t.Fatalf("the waiting key row says %q", line)
	}
	if strings.Contains(line, "browser") {
		t.Fatalf("a key row talked about a browser: %q", line)
	}
}

// ── 8. where to get the key ─────────────────────────────────────────────────

// THE ADDRESS IS DRAWN WHILE THE BOX IS OPEN AND AT NO OTHER TIME.
func TestTheKeyHintIsShownOnlyWhileTheBoxIsOpen(t *testing.T) {
	a, _ := keyApp(t)
	const host = "apidocs.chargebee.com"

	if sheetHas(a, host) {
		t.Fatalf("the catalog is advertising a settings page:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	onRow(t, a, "chargebee")
	if sheetHas(a, host) {
		t.Fatal("the cursor alone drew the address")
	}
	drive(t, a, key("enter"))
	if !sheetHas(a, host) {
		t.Fatalf("the open box does not say where the key is:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	drive(t, a, key("esc"))
	if sheetHas(a, host) {
		t.Fatal("the address outlived the box")
	}
}

// A SERVICE THAT SAYS NOTHING DRAWS NOTHING — the emptiness law, on a line that
// would otherwise be a link to a page nobody vouched for.
func TestABoxWithNoAddressDrawsNoLine(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))
	if sheetHas(a, "find it at") {
		t.Fatalf("a service with no address grew one:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	if !sheetHas(a, "paste your Recurly key") {
		t.Fatal("the box lost its own line with it")
	}
}

// AND THE INSTRUCTION STANDS OVER THE BOX for the services that need one, and
// over no other.
func TestTheBlankRuleIsSaidOverTheBox(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "chargebee")
	drive(t, a, key("enter"))
	if !sheetHas(a, "Give the domain and then the key") {
		t.Fatalf("the box does not say what it wants:\n%s",
			strings.Join(sheetLabels(a), "\n"))
	}
	drive(t, a, key("esc"))
	onRow(t, a, "recurly")
	drive(t, a, key("enter"))
	if sheetHas(a, "Give the domain") {
		t.Fatal("a service with no blank was asked for one")
	}
}

// THE FOOT LINE IS WHERE THE OTHER ANSWER IS TAUGHT: a variable's name is a key
// too, and nothing else on the screen could have said so.
func TestTheFootLineTeachesTheVariable(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "chargebee")
	drive(t, a, key("enter"))
	note := a.sheet.footNote()
	if !strings.Contains(note, "$CHARGEBEE_KEY") || !strings.Contains(note, "paste") {
		t.Fatalf("the foot line does not teach the two answers: %q", note)
	}
	if !strings.Contains(a.sheet.keysLine(), "enter connect") {
		t.Fatalf("the legend is still the list's: %q", a.sheet.keysLine())
	}
}

// ── 9. a key that lives in the environment ──────────────────────────────────

// A CONNECTION READ FROM A VARIABLE SAYS WHICH ONE, where an account would be —
// and a key pasted whole still says nothing at all.
func TestAConnectionFromAVariableNamesIt(t *testing.T) {
	a, _ := keyApp(t)
	at := serviceRowAt(a, "stripe")
	line := plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	if !strings.Contains(line, "from $STRIPE_KEY") {
		t.Fatalf("the row does not say where its key comes from: %q", line)
	}
	// THE EMPTINESS LAW: a key pasted whole has nothing to show, and a row of
	// bullets standing in for it would be this surface pretending to hold
	// something up.
	a.sheet.conn.catalog[1].KeyEnv = ""
	a.sheet.conn.groups = groupConnections(a.sheet.conn.catalog)
	a.sheet.build()
	at = serviceRowAt(a, "stripe")
	line = plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	if strings.Contains(line, "from $") || strings.Contains(line, "•") {
		t.Fatalf("a pasted key was drawn as something: %q", line)
	}
	if !strings.Contains(line, glyphConnected+" Stripe") {
		t.Fatalf("the row lost its tick: %q", line)
	}
}

// ── 10. what the page reads like at rest ────────────────────────────────────

// A CLOSED ACCOUNT SAYS WHAT IT MAY DO, in one line, so nothing has to be
// opened to be audited — and an OPEN one does not, because the rows under it
// are the same fact said longer.
func TestAClosedAccountSummarisesItsAnswers(t *testing.T) {
	a, _ := keyApp(t)
	at := serviceRowAt(a, "google")
	lines := plain(strings.Join(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal), "\n"))
	for _, want := range []string{capYesWord + ":", "read your mail", capAskWord + ":", "send mail as you"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("the closed account does not say %q:\n%s", want, lines)
		}
	}
	// The words are the tab's own three and nothing is rephrased.
	if strings.Contains(lines, "reads mail") {
		t.Fatalf("the summary invented a vocabulary:\n%s", lines)
	}

	a.sheet.cursor = at
	drive(t, a, key("enter"))
	open := plain(strings.Join(a.sheet.rowLines(a.sheet.items[serviceRowAt(a, "google")], false, false, a.width, a.pal), "\n"))
	if strings.Contains(open, capYesWord+":") {
		t.Fatalf("an open account is still summarising itself:\n%s", open)
	}
}

// AN ACCOUNT IS A BLOCK AND A BLOCK HAS AIR OVER IT — and the blank belongs to
// no row, so nothing can be pressed on it.
func TestEachHeldAccountGetsALineOfAir(t *testing.T) {
	a, _ := keyApp(t)
	lines, owner := a.sheet.listLines(a.width, a.pal, -1)
	held := 0
	for i, line := range lines {
		if !strings.Contains(plain(line), glyphConnected+" ") {
			continue
		}
		held++
		if i == 0 {
			continue
		}
		if strings.TrimSpace(plain(lines[i-1])) != "" {
			t.Fatalf("line %d is an account with no air over it: %q", i, plain(lines[i-1]))
		}
		if owner[i-1] != -1 {
			t.Fatalf("the blank over an account answers to row %d", owner[i-1])
		}
	}
	if held != 2 {
		t.Fatalf("the page drew %d held accounts, want two", held)
	}
}

// THE ANSWERS STAND IN ONE COLUMN: yes and ask first begin in the same place,
// which is what makes the column scannable without being read.
func TestTheAnswersBeginInOneColumn(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "google")
	drive(t, a, key("enter"))

	column := -1
	for _, id := range []string{"mail-read", "mail-send", "calendar-read", "calendar-write"} {
		at := capRowAt(a, "google", id)
		if at < 0 {
			t.Fatalf("the open account has no %s row", id)
		}
		line := plain(a.sheet.rowLines(a.sheet.items[at], false, false, a.width, a.pal)[0])
		word := capWord(a.sheet.items[at].conn.state)
		found := strings.Index(line, word)
		if found < 0 {
			t.Fatalf("%s does not carry its answer: %q", id, line)
		}
		if column < 0 {
			column = found
			continue
		}
		if found != column {
			t.Fatalf("%s begins its answer at %d, want %d — the column is ragged:\n%q",
				id, found, column, line)
		}
	}
}

// THE CATALOG IS QUIET AND THE CURSOR IS WHERE THE DETAIL IS: an available row
// is a name and one word saying what enter will ask for, and its sentence is
// drawn under the row a person has stopped on and under no other.
func TestTheCatalogDrawsItsBlurbUnderTheCursorOnly(t *testing.T) {
	a, _ := keyApp(t)
	onRow(t, a, "linear")

	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, "your issues") {
		t.Fatalf("the cursor's row does not explain itself:\n%s", screen)
	}
	if strings.Contains(screen, "your errors") || strings.Contains(screen, "your prospects") {
		t.Fatalf("the catalog is drawing everybody's sentence:\n%s", screen)
	}
	// What the other rows carry instead is the word saying what enter will ask
	// for — the /connect panel's own two tags.
	if !strings.Contains(screen, keyTag) || !strings.Contains(screen, signInTag) {
		t.Fatalf("the catalog rows do not say how they connect:\n%s", screen)
	}
	// A HELD ACCOUNT EXPLAINS NOTHING. It is connected, which is the whole of
	// what a person wanted to know.
	onRow(t, a, "google")
	if got := connAbout(a.sheet.items[a.sheet.cursor].conn); got != "" {
		t.Fatalf("a held account explained itself: %q", got)
	}
}

// AND A LIST SHORT ENOUGH TO READ KEEPS ITS SENTENCES ON THE ROWS, which is the
// /connect panel's own rule for the same catalog: the tag is what a person
// needs at two hundred rows and the sentence is what they need at six.
func TestAShortListKeepsItsBlurbsOnTheRow(t *testing.T) {
	a, _ := capsApp(t, twoAccounts)
	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, "your channels") {
		t.Fatalf("a short list hid its sentences:\n%s", screen)
	}
	if strings.Contains(screen, signInTag) {
		t.Fatalf("a list of two grew catalog tags:\n%s", screen)
	}
}
