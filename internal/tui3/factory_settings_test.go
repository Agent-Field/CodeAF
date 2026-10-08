package tui3

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// factorySettingsFake is [factoryFake] with the floor's own settings doors: a
// GitHub account that may or may not be connected, the repositories it can
// see, a kept rail the floor reads back, and a recipe file that may or may
// not have a known checkout.
type factorySettingsFake struct {
	*factoryFake
	mu        sync.Mutex
	link      factory.GitHubLink
	ghLogin   string
	watched   []string
	available []factory.RepoInfo
	rail      float64
	recipe    factory.Recipe
	problems  []factory.Problem
	dir       string
	saved     *factory.Recipe
}

func newSettingsFake() *factorySettingsFake {
	return &factorySettingsFake{
		factoryFake: &factoryFake{},
		link:        factory.GitHubLink{Login: "santoshkumarradha", Via: factory.ViaGH},
		watched:     []string{"agentfield/codeaf"},
		available: []factory.RepoInfo{
			{Full: "agentfield/codeaf", Owner: "agentfield", Name: "codeaf", Private: true, Pushed: factoryTestNow.Add(-50 * time.Hour), Open: 12, Dir: "/work/codeaf"},
			{Full: "agentfield/agentfield", Owner: "agentfield", Name: "agentfield", Pushed: factoryTestNow.Add(-5 * time.Hour), Open: 3},
			{Full: "santoshkumarradha/notes", Owner: "santoshkumarradha", Name: "notes", Pushed: factoryTestNow.Add(-21 * 24 * time.Hour)},
		},
		rail:   0,
		recipe: factory.DefaultRecipe(),
		dir:    "/work/codeaf",
	}
}

func (f *factorySettingsFake) seam() factory.Seam {
	s := f.factoryFake.seam()
	load := s.Load
	s.Load = func() (factory.Snapshot, error) {
		snap, err := load()
		f.mu.Lock()
		snap.Rail = f.rail
		f.mu.Unlock()
		return snap, err
	}
	s.Repos = func(ctx context.Context) ([]string, []factory.RepoInfo, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]string(nil), f.watched...), f.available, nil
	}
	s.SetRepos = func(repos []string) error {
		f.mu.Lock()
		f.watched = append([]string(nil), repos...)
		f.mu.Unlock()
		return f.rec("SetRepos", strings.Join(repos, " "))
	}
	s.GitHub = func(ctx context.Context) (factory.GitHubLink, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.link, nil
	}
	s.GHLogin = func(ctx context.Context) (string, error) { return f.ghLogin, nil }
	s.ConnectGitHub = func(ctx context.Context, token string) error {
		f.mu.Lock()
		via := factory.ViaGH
		if token != "" {
			via = factory.ViaToken
		}
		f.link = factory.GitHubLink{Login: "santoshkumarradha", Via: via}
		f.mu.Unlock()
		return f.rec("ConnectGitHub", token)
	}
	s.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		return f.recipe, f.problems, f.dir, nil
	}
	s.SaveRecipe = func(repo string, r factory.Recipe) error {
		f.mu.Lock()
		f.saved = &r
		f.mu.Unlock()
		return f.rec("SaveRecipe", repo)
	}
	s.SetRail = func(usd float64) error {
		f.mu.Lock()
		f.rail = usd
		f.mu.Unlock()
		return f.rec("SetRail", usd)
	}
	return s
}

// factorySettingsLab is the factory place over the settings fake at width.
func factorySettingsLab(t *testing.T, f *factorySettingsFake, width int) *app {
	t.Helper()
	a := placeApp(t)
	a.factory = f.seam()
	a.width, a.height = width, 40
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the settings fake")
	}
	f.said()
	return a
}

// factoryHint is the place's hint line as drawn.
func factoryHint(a *app) string { return placeFactory{}.hint(a) }

// factoryExactBody fails when any body row is not exactly width cells or, at
// the plain floor, carries SGR; it answers the body, plain, for the log.
func factoryExactBody(t *testing.T, a *app, width, room int) []string {
	t.Helper()
	keep := a.pal
	a.pal = newPalette(tokens.NoColor, false)
	defer func() { a.pal = keep }()
	var out []string
	for i, r := range a.factoryBody(width, room) {
		if got := ansi.StringWidth(r.text); got != width {
			t.Fatalf("at %d: body row %d is %d cells: %q", width, i, got, ansi.Strip(r.text))
		}
		if strings.Contains(r.text, "\x1b[") {
			t.Fatalf("at %d: body row %d carries SGR at the plain floor: %q", width, i, r.text)
		}
		out = append(out, strings.TrimRight(r.text, " "))
	}
	return out
}

// `R` LISTS WHAT GITHUB SEES in sections, the watched ones first; typing
// filters with no `/` first, space ticks, enter saves through SetRepos and says
// how many on the note line; esc saves nothing.
func TestFactoryPickerRowsToggleSaveAndCancel(t *testing.T) {
	f := newSettingsFake()
	a := factorySettingsLab(t, f, 150)
	drive(t, a, key("R"))
	if a.fp.pick == nil {
		t.Fatal("R opened no picker")
	}
	body := strings.Join(factoryExactBody(t, a, 150, 16), "\n")
	for _, want := range []string{
		"watch · repositories github sees as santoshkumarradha",
		"1 watching · 3 listed",
		"type to filter",
		"WATCHING · 1",
		"[x] agentfield/codeaf",
		"12 open",
		"private",
		"here",
		"pushed 2d",
		"AGENTFIELD · 1",
		"[ ] agentfield/agentfield",
		"pushed 5h",
		"SANTOSHKUMARRADHA · 1",
		"[ ] santoshkumarradha/notes",
		"pushed 3w",
		"checked out at /work/codeaf",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the picker is missing %q:\n%s", want, body)
		}
	}
	if hint := factoryHint(a); hint != "space watch · enter save · ctrl+o by open · esc close · type to filter" {
		t.Fatalf("picker hint = %q", hint)
	}

	// Typing filters at once, an `o` inside the words included; esc clears
	// the filter before it closes anything.
	factoryType(t, a, "notes")
	if vis := a.fp.pick.visible(); len(vis) != 1 || a.fp.pick.rows[vis[0]].full != "santoshkumarradha/notes" {
		t.Fatalf("the filter %q kept %v", a.fp.pick.query, vis)
	}
	if hint := factoryHint(a); hint != "space watch · enter save · esc clear" {
		t.Fatalf("filtered hint = %q", hint)
	}
	lines := factoryExactBody(t, a, 150, 16)
	if !strings.HasSuffix(strings.TrimSpace(lines[1]), " notes") || strings.Contains(lines[1], "type to filter") {
		t.Fatalf("the filter line does not say the words: %q", lines[1])
	}
	body = strings.Join(lines, "\n")
	if !strings.Contains(body, "codeaf does not know where santoshkumarradha/notes is checked out") {
		t.Fatalf("a repository with no checkout does not say what that means:\n%s", body)
	}
	drive(t, a, key(" "))
	drive(t, a, key("esc"))
	if a.fp.pick == nil || a.fp.pick.query != "" {
		t.Fatal("esc on a filtered picker did not clear the filter first")
	}
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SetRepos(agentfield/codeaf santoshkumarradha/notes)" {
		t.Fatalf("enter asked %q", got)
	}
	if a.fp.pick != nil {
		t.Fatal("the picker stayed after saving")
	}
	// The receipt says reading exactly while the head does: on this floor,
	// whose items show the read at once, it is the count alone.
	note := a.pageMsg
	if note == "" {
		note = a.factoryReadNote()
	}
	if !strings.HasPrefix(note, "watching 2 repositories") || strings.Contains(note, factoryReadingNowWords) != a.factoryFirstReading() {
		t.Fatalf("note line = %q while reading is %v", note, a.factoryFirstReading())
	}

	// And esc on an unfiltered picker closes it with nothing saved.
	drive(t, a, key("R"))
	drive(t, a, key("down"), key(" "), key("esc"))
	if a.fp.pick != nil {
		t.Fatal("esc did not close the picker")
	}
	if got := f.said(); len(got) != 0 {
		t.Fatalf("cancel asked %v", got)
	}
}

// newPickerFake is the settings fake with an account in several orgs: three
// owners, one a near-namesake of another, open counts, one checkout, and a
// watched repository GitHub no longer lists.
func newPickerFake() *factorySettingsFake {
	f := newSettingsFake()
	h := func(n int) time.Time { return factoryTestNow.Add(-time.Duration(n) * time.Hour) }
	f.watched = []string{"agent-field/codeaf", "santoshkumarradha/dotfiles", "gone/old"}
	f.available = []factory.RepoInfo{
		{Full: "agent-field/codeaf", Owner: "agent-field", Name: "codeaf", Private: true, Pushed: h(1), Open: 12, Dir: "/work/codeaf"},
		{Full: "santoshkumarradha/notes", Owner: "santoshkumarradha", Name: "notes", Pushed: h(2), Open: 2},
		{Full: "agent-field/agentfield", Owner: "agent-field", Name: "agentfield", Pushed: h(3), Open: 40},
		{Full: "xagent-field/mirror", Owner: "xagent-field", Name: "mirror", Pushed: h(5 * 24), Open: 1},
		{Full: "santoshkumarradha/dotfiles", Owner: "santoshkumarradha", Name: "dotfiles", Pushed: h(10 * 24)},
		{Full: "agent-field/docs", Owner: "agent-field", Name: "docs", Pushed: h(30 * 24), Open: 50},
	}
	return f
}

// factoryPickOrder is the picker's rows in the order the cursor walks them.
func factoryPickOrder(a *app) string {
	var out []string
	for _, i := range a.fp.pick.visible() {
		out = append(out, a.fp.pick.rows[i].full)
	}
	return strings.Join(out, " ")
}

// factoryPickHeadings is the section headings the body draws, top first.
func factoryPickHeadings(t *testing.T, a *app, width, room int) string {
	t.Helper()
	var out []string
	for _, l := range factoryExactBody(t, a, width, room) {
		if i := strings.Index(l, " ─"); i >= 0 {
			out = append(out, strings.TrimSpace(l[:i]))
		}
	}
	return strings.Join(out, " | ")
}

// THE SECTIONS AND THEIR ORDER: WATCHING first, then one block per owner,
// owners by their newest push, repositories by their last push; `ctrl+o` orders
// every block by how many are open, keeps the cursor on its repository, and
// the hint says the order `ctrl+o` would change to.
func TestFactoryPickerSectionsAndOrder(t *testing.T) {
	a := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, a, key("R"))
	if got := factoryPickHeadings(t, a, 150, 30); got != "WATCHING · 3 | SANTOSHKUMARRADHA · 1 | AGENT-FIELD · 2 | XAGENT-FIELD · 1" {
		t.Fatalf("headings = %q", got)
	}
	want := "agent-field/codeaf santoshkumarradha/dotfiles gone/old santoshkumarradha/notes agent-field/agentfield agent-field/docs xagent-field/mirror"
	if got := factoryPickOrder(a); got != want {
		t.Fatalf("order by push =\n%s\nwant\n%s", got, want)
	}
	drive(t, a, key("down"), key("down"), key("down"), key("down"), key("down"))
	if a.fp.pick.rows[a.fp.pick.visible()[a.fp.pick.cursor]].full != "agent-field/docs" {
		t.Fatal("the cursor is not on agent-field/docs")
	}
	drive(t, a, key("ctrl+o"))
	if a.fp.pick.query != "" || !a.fp.pick.byOpen {
		t.Fatalf("ctrl+o typed %q instead of ordering", a.fp.pick.query)
	}
	want = "agent-field/codeaf santoshkumarradha/dotfiles gone/old santoshkumarradha/notes agent-field/docs agent-field/agentfield xagent-field/mirror"
	if got := factoryPickOrder(a); got != want {
		t.Fatalf("order by open =\n%s\nwant\n%s", got, want)
	}
	if a.fp.pick.rows[a.fp.pick.visible()[a.fp.pick.cursor]].full != "agent-field/docs" {
		t.Fatal("o moved the cursor off its repository")
	}
	if hint := factoryHint(a); !strings.Contains(hint, "ctrl+o by pushed") {
		t.Fatalf("the hint does not say o goes back to the push: %q", hint)
	}
}

// THE WATCHED SECTION HOLDS EVERY TICKED REPOSITORY, a watched one GitHub no
// longer lists included, and no owner's block repeats one. A tick made now
// does not move its row, so the cursor never jumps.
func TestFactoryPickerWatchingHoldsEveryTicked(t *testing.T) {
	a := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, a, key("R"))
	p := a.fp.pick
	secs := p.sections()
	if len(secs) == 0 || secs[0].heading != "watching" {
		t.Fatalf("the first section is not WATCHING: %+v", secs)
	}
	in := map[string]bool{}
	for _, i := range secs[0].rows {
		in[strings.ToLower(p.rows[i].full)] = true
	}
	for _, w := range []string{"agent-field/codeaf", "santoshkumarradha/dotfiles", "gone/old"} {
		if !in[w] {
			t.Fatalf("WATCHING is missing %s", w)
		}
	}
	for _, s := range secs[1:] {
		for _, i := range s.rows {
			if p.on[strings.ToLower(p.rows[i].full)] {
				t.Fatalf("%s is ticked outside WATCHING, in %s", p.rows[i].full, s.heading)
			}
		}
	}
	drive(t, a, key("down"), key("down"), key("down"), key(" "))
	if got := p.rows[p.visible()[p.cursor]].full; got != "santoshkumarradha/notes" || !p.on["santoshkumarradha/notes"] {
		t.Fatalf("the tick moved the cursor to %s", got)
	}
	if body := strings.Join(factoryExactBody(t, a, 150, 30), "\n"); !strings.Contains(body, "4 watching") {
		t.Fatalf("the heading does not count the new tick:\n%s", body)
	}
}

// TYPING NARROWS: plain words anywhere in owner/name, case aside; `owner/`
// narrows to that owner exactly, not to a name that merely contains it; words
// after the slash narrow inside the owner.
func TestFactoryPickerTypingNarrows(t *testing.T) {
	a := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, a, key("R"))
	for _, c := range []struct{ typed, want string }{
		{"NOTES", "santoshkumarradha/notes"},
		{"field", "agent-field/codeaf agent-field/agentfield agent-field/docs xagent-field/mirror"},
		{"agent-field/", "agent-field/codeaf agent-field/agentfield agent-field/docs"},
		{"agent-field/do", "agent-field/docs"},
		{"zzz", ""},
	} {
		drive(t, a, key("ctrl+u"))
		factoryType(t, a, c.typed)
		if got := factoryPickOrder(a); got != c.want {
			t.Fatalf("typing %q kept %q, want %q", c.typed, got, c.want)
		}
	}
	if body := strings.Join(factoryExactBody(t, a, 150, 12), "\n"); !strings.Contains(body, "no repository matches zzz") {
		t.Fatalf("an empty filter result says nothing:\n%s", body)
	}
}

// KEYS BOUND BEFORE TYPING NEVER ENTER THE FILTER: the arrows, space,
// backspace, `ctrl+o` and a first `/`; once words are typed `/` is a letter
// like any other, and `o` always is.
func TestFactoryPickerBoundKeysNeverEnterTheFilter(t *testing.T) {
	a := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, a, key("R"))
	drive(t, a, key("down"), key("up"), key(" "), key("backspace"), key("ctrl+o"), key("/"), key("ctrl+o"))
	if q := a.fp.pick.query; q != "" {
		t.Fatalf("bound keys typed %q", q)
	}
	factoryType(t, a, "agent-field/co")
	if q := a.fp.pick.query; q != "agent-field/co" {
		t.Fatalf("typing after words kept %q", q)
	}
	// `o` is a letter even with open counts drawn, so a name that starts with
	// one is typed like any other.
	drive(t, a, key("esc"))
	drive(t, a, key("o"))
	if a.fp.pick.query != "o" {
		t.Fatalf("o typed %q", a.fp.pick.query)
	}
	// With no open count drawn, `ctrl+o` orders nothing and is not offered.
	b := factorySettingsLab(t, newSettingsFake(), 150)
	drive(t, b, key("R"))
	for i := range b.fp.pick.rows {
		b.fp.pick.rows[i].open = -1
	}
	drive(t, b, key("ctrl+o"))
	if b.fp.pick.query != "" || b.fp.pick.byOpen {
		t.Fatalf("ctrl+o with no open counts typed %q or ordered", b.fp.pick.query)
	}
	if hint := factoryHint(b); strings.Contains(hint, "o by") {
		t.Fatalf("the hint offers ctrl+o with nothing to order by: %q", hint)
	}
}

// THE FACTS THAT CHOOSE ARE DRAWN, and a column no row has anything in is
// not: no checkout anywhere draws no `here`, and no open count no `open`.
func TestFactoryPickerOpenAndHereColumns(t *testing.T) {
	a := factorySettingsLab(t, newPickerFake(), 150)
	drive(t, a, key("R"))
	body := strings.Join(factoryExactBody(t, a, 150, 30), "\n")
	for _, want := range []string{"12 open", "40 open", "here", "pushed 1h"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the picker is missing %q:\n%s", want, body)
		}
	}
	for i := range a.fp.pick.rows {
		a.fp.pick.rows[i].dir, a.fp.pick.rows[i].open = "", -1
	}
	body = strings.Join(factoryExactBody(t, a, 150, 30), "\n")
	for _, gone := range []string{" open", " here"} {
		if strings.Contains(body, gone) {
			t.Fatalf("an empty column %q is drawn:\n%s", gone, body)
		}
	}
}

// NO TOKEN IS THE CONNECT PROMPT. With gh logged in the floor offers it and
// `y` keeps it, then opens the picker; without gh it asks for a token, drawn
// masked, and the token goes to the door and nowhere on the screen.
func TestFactoryConnectPromptWhenNoToken(t *testing.T) {
	f := newSettingsFake()
	f.link = factory.GitHubLink{}
	f.ghLogin = "santoshkumarradha"
	a := factorySettingsLab(t, f, 150)
	drive(t, a, key("R"))
	if a.fp.pick != nil || a.fp.ghOffer != "santoshkumarradha" {
		t.Fatalf("no token opened pick=%v offer=%q", a.fp.pick != nil, a.fp.ghOffer)
	}
	// Beside the rows the pane breaks the question after the login; on a
	// floor with no peek it is one line.
	if text := factoryFrameText(a); !strings.Contains(text, "connect github: gh is logged in as santoshkumarradha,") || !strings.Contains(text, "use it? [y] · [n] a token instead") {
		t.Fatalf("the offer is not drawn:\n%s", text)
	}
	a.width = 100
	if text := factoryFrameText(a); !strings.Contains(text, "connect github: gh is logged in as santoshkumarradha, use it? [y]") {
		t.Fatalf("the offer is not one line at 100:\n%s", text)
	}
	a.width = 150
	if hint := factoryHint(a); hint != "y use gh · n a token instead · esc not now" {
		t.Fatalf("offer hint = %q", hint)
	}
	drive(t, a, key("y"))
	if got := strings.Join(f.said(), " "); got != "ConnectGitHub()" {
		t.Fatalf("y asked %q", got)
	}
	if a.fp.pick == nil || a.fp.pick.login != "santoshkumarradha" {
		t.Fatal("a kept connection did not open the picker")
	}

	f2 := newSettingsFake()
	f2.link = factory.GitHubLink{}
	b := factorySettingsLab(t, f2, 150)
	drive(t, b, key("R"))
	if b.fp.act.ask == nil || b.fp.act.ask.kind != factoryAskToken {
		t.Fatal("no gh and no token did not open the token row")
	}
	factoryType(t, b, "ghp_secret")
	text := factoryFrameText(b)
	if !strings.Contains(text, "github token ›") || strings.Contains(text, "ghp_secret") {
		t.Fatalf("the token row drew its words:\n%s", text)
	}
	drive(t, b, key("enter"))
	if got := strings.Join(f2.said(), " "); got != "ConnectGitHub(ghp_secret)" {
		t.Fatalf("enter asked %q", got)
	}
	if b.fp.pick == nil {
		t.Fatal("a kept token did not open the picker")
	}
}

// `E` IS THE RECIPE PAGE for the repository the floor shows: tabs, the stage
// rail and pane, a line that did not load, the page's own edits and `b`
// saving through the door.
func TestFactoryRecipePageTabsProblemAndSave(t *testing.T) {
	f := newSettingsFake()
	f.problems = []factory.Problem{{Line: 7, Text: "3. review · until clen", Why: "until is one of done, clean, green, proven"}}
	a := factorySettingsLab(t, f, 150)

	// With every repository on the floor, E asks for one first.
	if len(a.fp.snap.Repos) < 2 {
		t.Fatal("the fixture has one repository")
	}
	drive(t, a, key("E"))
	if a.fp.recipe != nil || !strings.Contains(a.pageMsg, "pick a repository with [ ] first") {
		t.Fatalf("E on all repos: open=%v note=%q", a.fp.recipe != nil, a.pageMsg)
	}
	drive(t, a, key("]"), key("E"))
	if a.fp.recipe == nil {
		t.Fatal("E opened no recipe page")
	}
	repo := a.fp.recipe.repo
	body := factoryExactBody(t, a, 150, 14)
	joined := strings.Join(body, "\n")
	for _, want := range []string{"issue · pr · ci", "Factory › " + factoryRepoShort(repo) + " › recipe", "line 7: until is one of done, clean, green, proven", "plan", "write", "runs first"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the recipe page is missing %q:\n%s", want, joined)
		}
	}
	if hint := factoryHint(a); hint != "[ ] kind · ↑↓ stages · 1-9 stages · s stage · e effort · w in words · b save · esc floor" {
		t.Fatalf("recipe hint = %q", hint)
	}

	// The tabs switch; an edit on pr is pr's own; w reads knobs; b saves.
	drive(t, a, key("]"))
	if a.fp.recipe.kindNow() != factory.KindPR {
		t.Fatalf("] moved to %s", a.fp.recipe.kindNow())
	}
	drive(t, a, key("["), key("2"))
	if a.fp.recipe.stages()[1].On {
		t.Fatal("2 did not switch the second stage off")
	}
	drive(t, a, key("w"))
	factoryType(t, a, "until clean, max 3")
	drive(t, a, key("enter"))
	if st := a.fp.recipe.stages()[0]; st.Until != "clean" || st.Max != 3 {
		t.Fatalf("w left the stage %+v", st)
	}
	drive(t, a, key("w"))
	factoryType(t, a, "until clen")
	drive(t, a, key("enter"))
	if !strings.Contains(a.pageMsg, "until is one of") {
		t.Fatalf("a knob that is not a word said %q", a.pageMsg)
	}
	drive(t, a, key("b"))
	if got := strings.Join(f.said(), " "); got != "SaveRecipe("+repo+")" {
		t.Fatalf("b asked %q", got)
	}
	if f.saved == nil || f.saved.For(factory.KindIssue)[1].On || f.saved.For(factory.KindIssue)[0].Max != 3 {
		t.Fatalf("the saved recipe is %+v", f.saved)
	}
	if !strings.Contains(a.pageMsg, "saved to .codeaf/factory.md") {
		t.Fatalf("note after b = %q", a.pageMsg)
	}
	drive(t, a, key("esc"))
	if a.fp.recipe != nil {
		t.Fatal("esc left the recipe page standing")
	}
}

// A RECIPE WHOSE CHECKOUT IS UNKNOWN IS DRAWN AND NOT EDITED, and the note line
// says why.
func TestFactoryRecipePageWithNoDirSaysSo(t *testing.T) {
	f := newSettingsFake()
	f.dir = ""
	a := factorySettingsLab(t, f, 100)
	drive(t, a, key("]"), key("E"))
	if a.fp.recipe == nil {
		t.Fatal("E opened no recipe page")
	}
	repo := a.fp.recipe.repo
	note := strings.Join(placeFactory{}.note(a, 100), "")
	if !strings.Contains(ansi.Strip(note), "codeaf does not know where "+repo+" is checked out") {
		t.Fatalf("note = %q", ansi.Strip(note))
	}
	if hint := factoryHint(a); hint != "[ ] kind · ↑↓ stages · esc floor" {
		t.Fatalf("hint = %q", hint)
	}
	drive(t, a, key("2"), key("b"))
	if got := f.said(); len(got) != 0 || !a.fp.recipe.stages()[1].On {
		t.Fatalf("a page with no checkout changed: asked %v", got)
	}
	factoryExactBody(t, a, 100, 12)
}

// `$` SETS THE DAY RAIL and the handover's money clause reads it; NO RAIL IS
// NO CLAUSE.
func TestFactoryRailSetsTheClause(t *testing.T) {
	f := newSettingsFake()
	f.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].Stream != nil {
				s.Items[i].Stream.Spent = 0
			}
		}
	}
	a := factorySettingsLab(t, f, 150)
	if text := factoryFrameText(a); strings.Contains(text, "/ $") {
		t.Fatalf("a floor with no rail drew a rail clause:\n%s", text)
	}
	drive(t, a, key("$"))
	if a.fp.act.ask == nil || a.fp.act.ask.kind != factoryAskRail {
		t.Fatal("$ opened no rail row")
	}
	if text := factoryFrameText(a); !strings.Contains(text, "day rail ›") {
		t.Fatalf("the rail row is not drawn:\n%s", text)
	}
	factoryType(t, a, "$45")
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SetRail(45)" {
		t.Fatalf("enter asked %q", got)
	}
	if a.pageMsg != "the day rail is $45" {
		t.Fatalf("note = %q", a.pageMsg)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "/ $45") {
		t.Fatalf("the handover does not read the rail:\n%s", text)
	}
	// The row opens on the rail that is set.
	drive(t, a, key("$"))
	if a.fp.act.ask == nil || a.fp.act.ask.text != "$45" {
		t.Fatalf("the rail row opened on %+v", a.fp.act.ask)
	}
	drive(t, a, key("esc"))
}

// EVERY NIL DOOR DRAWS NO KEY, and pressing it does nothing.
func TestFactorySettingsNilDoorsDrawNoKey(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.width = 400
	hint := factoryHint(a)
	for _, gone := range []string{"R repos", "$ rail"} {
		if strings.Contains(hint, gone) {
			t.Fatalf("a seam without the door offers %q: %s", gone, hint)
		}
	}
	drive(t, a, key("R"), key("$"))
	if a.fp.pick != nil || a.fp.act.ask != nil {
		t.Fatal("a nil door opened something")
	}

	s := newSettingsFake()
	b := factorySettingsLab(t, s, 400)
	hint = factoryHint(b)
	for _, want := range []string{"R repos", "E recipe", "$ rail"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("the floor's hint is missing %q: %s", want, hint)
		}
	}
	// On a narrow floor they are the first to go.
	b.width = 100
	if hint := factoryHint(b); strings.Contains(hint, "$ rail") && !strings.Contains(hint, "A backlog") {
		t.Fatalf("the settings keys outlived the rail's own: %s", hint)
	}
}

// THE FRAMES ARE EXACT at 150 and 100 columns for the picker and the recipe
// page, and carry no SGR at the plain floor. They are logged, for the eye.
func TestFactorySettingsFramesAreExact(t *testing.T) {
	for _, width := range []int{150, 100} {
		f := newSettingsFake()
		a := factorySettingsLab(t, f, width)
		drive(t, a, key("R"))
		t.Logf("picker at %d:\n%s", width, strings.Join(factoryExactBody(t, a, width, 8), "\n"))
		drive(t, a, key("esc"), key("]"), key("E"))
		t.Logf("recipe at %d:\n%s", width, strings.Join(factoryExactBody(t, a, width, 12), "\n"))
		drive(t, a, key("esc"), key("$"))
		lines := factoryFrameLines(a)
		for _, l := range lines {
			if strings.Contains(l, "day rail ›") {
				t.Logf("rail row at %d: %s", width, strings.TrimRight(l, " "))
			}
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Fatalf("at %d row %d is %d cells", width, i, w)
			}
		}
	}
}

// THE SETTINGS PLACE'S CONNECTIONS TAB HAS ONE `github` ROW when the seam can
// say how this machine reaches GitHub, read once off the loop; enter on it
// runs the floor's connect prompt. With no door there is no row.
func TestSettingsGitHubRowReadsTheLinkAndConnects(t *testing.T) {
	f := newSettingsFake()
	f.ghLogin = "santoshkumarradha"
	a := placeApp(t)
	a.factory = f.seam()
	a.width, a.height = 120, 40
	if cmd := a.showPage(pageSettings); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	for at, title := range settingTabs {
		if title == tabConnections {
			a.sheet.tab = at
		}
	}
	a.sheet.build()
	found := -1
	for i, it := range a.sheet.items {
		if it.conn != nil && it.conn.service == githubService {
			found = i
		}
	}
	if found < 0 {
		t.Fatal("no github row on the Connections tab")
	}
	row := a.sheet.items[found].conn
	if !row.connected || row.account != "santoshkumarradha · via gh" {
		t.Fatalf("the github row says %+v", row)
	}
	if text := placeFrameText(a); !strings.Contains(text, "santoshkumarradha · via gh") {
		t.Fatalf("the row is not drawn:\n%s", text)
	}
	if got := githubNote(&githubReading{link: factory.GitHubLink{Login: "x", Via: factory.ViaToken}}); got != "x · token" {
		t.Fatalf("a kept token reads %q", got)
	}
	if got := githubNote(&githubReading{}); got != "not connected" {
		t.Fatalf("no token reads %q", got)
	}
	a.sheet.cursor = found
	drive(t, a, runCmd(a.connAct(row))...)
	if !a.at(pageFactory) {
		t.Fatal("enter on the github row did not open the floor")
	}
	if a.fp.ghOffer != "santoshkumarradha" {
		t.Fatalf("the floor did not offer gh: %q", a.fp.ghOffer)
	}

	b := placeApp(t)
	if cmd := b.showPage(pageSettings); cmd != nil {
		drive(t, b, runCmd(cmd)...)
	}
	for at, title := range settingTabs {
		if title == tabConnections {
			b.sheet.tab = at
		}
	}
	b.sheet.build()
	for _, it := range b.sheet.items {
		if it.conn != nil && it.conn.service == githubService {
			t.Fatal("a seam with no github door drew the row")
		}
	}
}
