package tui3

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// proposingAgent is an agent whose model pass answers what it is loaded with,
// and remembers what it was asked.
type proposingAgent struct {
	Agent
	mu    sync.Mutex
	res   session.TeamProposal
	err   error
	calls int
	in    session.TeamProposalInput
}

func (p *proposingAgent) ProposeTeams(_ context.Context, in session.TeamProposalInput) (session.TeamProposal, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.in = in
	return p.res, p.err
}

// orgKeys is the keys of props, row by row, for comparing.
func orgKeys(props []orgProp) [][]string {
	out := make([][]string, 0, len(props))
	for _, p := range props {
		out = append(out, p.keys)
	}
	return out
}

// THE FOLDER PASS IS EXACT: two or more conversations in one folder are a team
// named after it, unless one team already holds them all, or a team has the
// folder's name, when only the missing ones are suggested for it.
func TestOrganizeFolderPass(t *testing.T) {
	convs := []orgConv{
		{key: "a", title: "one", where: "/src/codeaf"},
		{key: "b", title: "two", where: "/src/codeaf/"},
		{key: "c", title: "three", where: "/work/codeaf"},
		{key: "d", title: "four", where: "/src/nvda"},
		{key: "e", title: "five", where: "/src/solo"},
		{key: "f", title: "six", where: ""},
		{key: "g", title: "seven", where: ""},
		{key: "h", title: "eight", where: "/src/nvda"},
		{key: "i", title: "nine", where: "/src/harbor"},
		{key: "j", title: "ten", where: "/src/harbor"},
		{key: "k", title: "eleven", where: "/work/codeaf"},
	}
	t.Run("new teams, two folders of one name are one", func(t *testing.T) {
		props := organizeFolders(convs, nil)
		var names []string
		for _, p := range props {
			names = append(names, p.name)
			if p.team != "" || !p.folder {
				t.Fatalf("a folder row that is not a new folder team: %+v", p)
			}
		}
		if !reflect.DeepEqual(names, []string{"codeaf", "nvda", "harbor"}) {
			t.Fatalf("teams %v", names)
		}
		if !reflect.DeepEqual(orgKeys(props), [][]string{{"a", "b", "c", "k"}, {"d", "h"}, {"i", "j"}}) {
			t.Fatalf("members %v", orgKeys(props))
		}
	})
	t.Run("a team holding them all, and a team of the folder's name", func(t *testing.T) {
		teams := []team{
			{ID: "t-ops", Name: "ops", Members: []teamMember{{Key: "d"}, {Key: "h"}, {Key: "a"}}},
			{ID: "t-harbor", Name: "Harbor", Members: []teamMember{{Key: "i"}}},
		}
		props := organizeFolders(convs, teams)
		if len(props) != 2 {
			t.Fatalf("props %+v", props)
		}
		if props[0].team != "" || props[0].name != "codeaf" {
			t.Fatalf("first row %+v", props[0])
		}
		if props[1].team != "t-harbor" || !reflect.DeepEqual(props[1].keys, []string{"j"}) {
			t.Fatalf("the harbor row %+v", props[1])
		}
	})
}

// THE FOLDER PASS WINS. A model team with a folder team's name or exactly its
// members is dropped; a model addition to a team the folder pass adds to
// brings only what that row lacks; new teams come first.
func TestOrganizeMergeFolderWins(t *testing.T) {
	folder := []orgProp{
		{team: "t1", name: "harbor", keys: []string{"f"}, folder: true},
		{name: "codeaf", keys: []string{"a", "b"}, folder: true},
	}
	model := []orgProp{
		{name: "codeaf", keys: []string{"c", "d"}, reason: "same name"},
		{name: "other", keys: []string{"b", "a"}, reason: "same members"},
		{name: "nvda", keys: []string{"c", "d"}, reason: "one company"},
		{team: "t1", name: "harbor", keys: []string{"e", "f"}},
		{team: "t2", name: "orbit", keys: []string{"g"}},
	}
	got := organizeMerge(folder, model)
	var names []string
	for _, p := range got {
		names = append(names, p.name)
	}
	if !reflect.DeepEqual(names, []string{"codeaf", "nvda", "harbor", "orbit"}) {
		t.Fatalf("rows %v", names)
	}
	if !reflect.DeepEqual(orgKeys(got), [][]string{{"a", "b"}, {"c", "d"}, {"f", "e"}, {"g"}}) {
		t.Fatalf("members %v", orgKeys(got))
	}
	if !got[0].folder || got[1].reason != "one company" {
		t.Fatalf("the rows lost where they came from: %+v", got[:2])
	}
}

// organizeApp is the strip's three conversations on the wall, two of them in
// /tmp/lab, with agent as the one that answers.
func organizeApp(t *testing.T, agent func(Agent) Agent) *app {
	t.Helper()
	a, _, _ := tabApp(t)
	if agent != nil {
		a.agent = agent(a.agent)
	}
	_ = a.openWall()
	_ = a.wallFrame(a.width, a.height)
	return a
}

// orgFrame is the wall as text.
func orgFrame(a *app) string {
	return wallPlainFrame(a.wallFrame(a.width, a.height))
}

// orgHitAt is where the last frame drew the first target of kind and arg.
func orgHitAt(t *testing.T, a *app, kind wallHitKind, arg int) wallHit {
	t.Helper()
	for _, h := range a.wall.hits {
		if h.kind == kind && h.arg == arg {
			return h
		}
	}
	t.Fatalf("no target %d/%d on the frame:\n%s", kind, arg, orgFrame(a))
	return wallHit{}
}

// WITH NO MODEL TO ASK, THE FOLDER PASS IS SHOWN ALONE AND SAYS SO, and every
// row is toggled by space and by a press.
func TestOrganizeCardTogglesAndSaysFoldersOnly(t *testing.T) {
	a := organizeApp(t, nil)
	a.wallKey(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if !a.wall.org.on || a.wall.org.thinking {
		t.Fatalf("the card is not up with its rows: %+v", a.wall.org)
	}
	frame := orgFrame(a)
	for _, want := range []string{"─ Organize ", "New teams", "lab", "from the folder", "suggestions from folders only", "Cancel esc", "Apply ↵"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the card lacks %q:\n%s", want, frame)
		}
	}
	if len(a.wall.org.props) != 1 || !a.wall.org.props[0].take {
		t.Fatalf("props %+v", a.wall.org.props)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if a.wall.org.props[0].take {
		t.Fatal("space did not untick the row")
	}
	if frame := orgFrame(a); !strings.Contains(frame, "☐ ") {
		t.Fatalf("the row does not show it is unticked:\n%s", frame)
	}
	row := orgHitAt(t, a, wallHitOrgRow, 0)
	_, _ = a.wallPress(row.x0+1, row.y0)
	if !a.wall.org.props[0].take {
		t.Fatal("a press did not tick the row")
	}
	// A press off the card puts it away with no change.
	_, _ = a.wallPress(0, a.height-4)
	if a.wall.org.on || len(a.wall.teams) != 0 {
		t.Fatalf("a press off the card: on %v, teams %d", a.wall.org.on, len(a.wall.teams))
	}
}

// APPLY MAKES THE TICKED TEAMS IN ONE SAVE, AND UNDO PUTS BACK THE EXACT LIST
// THAT WAS THERE, on disk as in memory.
func TestOrganizeApplyThenUndoRestoresTheExactList(t *testing.T) {
	a := organizeApp(t, nil)
	tiles := a.wallShown(a.now())
	if _, err := a.teamMake("orbit", []chatTab{tiles[1].tab}); err != nil {
		t.Fatal(err)
	}
	// The list Undo must give back is the one as written: the save is queued,
	// and the member's handle and the stored time come back with it.
	teamsFlush(t, a)
	prior := teamsClone(a.wall.teams)
	before, err := os.ReadFile(teamsPath(a.profileDir))
	if err != nil {
		t.Fatal(err)
	}
	a.wallKey(tea.KeyPressMsg{Code: 'o', Text: "o"})
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.wall.org.on || len(a.wall.teams) != 2 || a.wall.teams[1].Name != "lab" || len(a.wall.teams[1].Members) != 2 {
		t.Fatalf("after Apply: %+v", a.wall.teams)
	}
	if hue := a.wall.teams[1].HueSpec(); hue == a.wall.teams[0].HueSpec() {
		t.Fatal("the new team took an existing team's colour")
	}
	// `Organized` is said once the store took the Apply (teamwritesaid.go).
	teamsFlush(t, a)
	frame := orgFrame(a)
	if !strings.Contains(frame, "Organized · 1 new team  ") || !strings.Contains(frame, " Undo ") {
		t.Fatalf("the Teams row does not offer Undo:\n%s", frame)
	}
	undo := orgHitAt(t, a, wallHitAction, int(wallActOrgUndo))
	if got := ansi.Strip(ansi.Cut(a.wallFrame(a.width, a.height)[undo.y0], undo.x0, undo.x1)); got != " Undo " {
		t.Fatalf("the Undo target lies on %q", got)
	}
	_, _ = a.wallPress(undo.x0+1, undo.y0)
	if !reflect.DeepEqual(a.wall.teams, prior) {
		t.Fatalf("Undo left %+v\nwant %+v", a.wall.teams, prior)
	}
	teamsFlush(t, a)
	after, err := os.ReadFile(teamsPath(a.profileDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the file after Undo:\n%s\nbefore:\n%s", after, before)
	}
	if frame := orgFrame(a); strings.Contains(frame, "Undo") {
		t.Fatalf("Undo is still offered:\n%s", frame)
	}
}

// THE BUTTON IS ON ALL ONLY, and o does nothing while a team is shown.
func TestOrganizeOnlyOnAll(t *testing.T) {
	a := organizeApp(t, nil)
	if frame := orgFrame(a); !strings.Contains(frame, "✦ Organize") {
		t.Fatalf("All has no Organize:\n%s", frame)
	}
	hit := orgHitAt(t, a, wallHitAction, int(wallActOrganize))
	if got := ansi.Strip(ansi.Cut(a.wallFrame(a.width, a.height)[hit.y0], hit.x0, hit.x1)); got != " ✦ Organize " {
		t.Fatalf("the button's target lies on %q", got)
	}
	tiles := a.wallShown(a.now())
	id, err := a.teamMake("orbit", []chatTab{tiles[0].tab})
	if err != nil {
		t.Fatal(err)
	}
	a.wallSetTeam(id)
	if frame := orgFrame(a); strings.Contains(frame, "Organize") {
		t.Fatalf("a team's view has Organize:\n%s", frame)
	}
	a.wallKey(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if a.wall.org.on {
		t.Fatal("o opened the card on a team's view")
	}
}

// THE BUTTON COUNTS FIVE OR MORE CONVERSATIONS IN NO TEAM, and after a run
// that found nothing it says Organized until something changes.
func TestOrganizeCountAndOrganizedStates(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		v := wallUnmarked(wallFixture(6))
		v.team = ""
		v.org.loose = 7
		rows, _ := renderWall(pal, v, 120, 40)
		want := "✦ Organize 7"
		if pal.ascii {
			want = "* Organize 7"
		}
		if got := ansi.Strip(rows[1]); !strings.Contains(got, want) {
			t.Fatalf("%s: the Teams row %q lacks %q", pname, got, want)
		}
		v.org.loose = 4
		rows, _ = renderWall(pal, v, 120, 40)
		if got := ansi.Strip(rows[1]); strings.Contains(got, "Organize 4") || !strings.Contains(got, "Organize ") {
			t.Fatalf("%s: under five the row is %q", pname, got)
		}
		v.org.clean = true
		rows, _ = renderWall(pal, v, 120, 40)
		want = "Organized ✓"
		if pal.ascii {
			want = "Organized"
		}
		if got := ansi.Strip(rows[1]); !strings.Contains(got, want) {
			t.Fatalf("%s: the clean row %q lacks %q", pname, got, want)
		}
	}

	// On the app: a run that finds nothing says so beside a Close, and the
	// button says Organized until the teams change.
	p := &proposingAgent{}
	a := organizeApp(t, func(inner Agent) Agent { p.Agent = inner; return p })
	tiles := a.wallShown(a.now())
	var lab []chatTab
	for _, tile := range tiles {
		if tile.tab.where == "/tmp/lab" {
			lab = append(lab, tile.tab)
		}
	}
	if _, err := a.teamMake("lab", lab); err != nil {
		t.Fatal(err)
	}
	cmd := a.wallOrganizeOpen()
	if !a.wall.org.thinking || !strings.Contains(orgFrame(a), "thinking…") {
		t.Fatalf("the card does not say it is thinking:\n%s", orgFrame(a))
	}
	namerDoors(t, a, cmd)
	if frame := orgFrame(a); !strings.Contains(frame, "Everything is organized") || !strings.Contains(frame, "Close esc") {
		t.Fatalf("an empty run:\n%s", frame)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if frame := orgFrame(a); !strings.Contains(frame, "Organized ✓") {
		t.Fatalf("the button does not say Organized:\n%s", frame)
	}
	if _, err := a.teamMake("orbit", []chatTab{tiles[0].tab}); err != nil {
		t.Fatal(err)
	}
	if frame := orgFrame(a); strings.Contains(frame, "Organized ✓") || !strings.Contains(frame, "✦ Organize") {
		t.Fatalf("a change left the button saying Organized:\n%s", frame)
	}
}

// A FAILED MODEL PASS SHOWS THE FOLDER PASS ALONE; a good one is merged in
// and priced.
func TestOrganizeModelFailureFallsBackToFolders(t *testing.T) {
	p := &proposingAgent{err: errors.New("no provider answered")}
	a := organizeApp(t, func(inner Agent) Agent { p.Agent = inner; return p })
	cmd := a.wallOrganizeOpen()
	namerDoors(t, a, cmd)
	if !a.wall.org.folderOnly || len(a.wall.org.props) != 1 || a.wall.org.props[0].name != "lab" {
		t.Fatalf("after a failure: %+v", a.wall.org)
	}
	if frame := orgFrame(a); !strings.Contains(frame, "suggestions from folders only") {
		t.Fatalf("the card does not say it is folders only:\n%s", frame)
	}
	if len(p.in.Conversations) != 3 || p.in.Conversations[0].Folder == "" {
		t.Fatalf("the model was asked about %+v", p.in.Conversations)
	}
	a.wallKey(tea.KeyPressMsg{Code: tea.KeyEscape})

	var lab, leadgen string
	for _, tile := range a.wallShown(a.now()) {
		switch tile.tab.where {
		case "/tmp/lab":
			lab = tile.tab.key
		case "/tmp/leadgen":
			leadgen = tile.tab.key
		}
	}
	p.err = nil
	p.res = session.TeamProposal{
		New:         []session.ProposedTeam{{Name: "scrapers", Members: []string{lab, leadgen}, Reason: "both scrape"}},
		Model:       "some/unpriced-model",
		PromptChars: 4000,
	}
	cmd = a.wallOrganizeOpen()
	namerDoors(t, a, cmd)
	o := a.wall.org
	if o.folderOnly || len(o.props) != 2 || o.props[0].name != "lab" || o.props[1].name != "scrapers" {
		t.Fatalf("after an answer: %+v", o.props)
	}
	if o.props[0].hue == o.props[1].hue {
		t.Fatal("two new teams were offered one colour")
	}
	frame := orgFrame(a)
	if !strings.Contains(frame, "about 1.3k tokens") || strings.Contains(frame, "folders only") {
		t.Fatalf("the priced card:\n%s", frame)
	}
	if p.calls != 2 {
		t.Fatalf("%d asks, want one per press", p.calls)
	}
}

// orgFixture is a view with the card up over six tiles: two new teams, one
// with a long name and long titles, and an addition.
func orgFixture(pal palette) wallView {
	v := wallUnmarked(wallFixture(6))
	v.team = ""
	v.org.on = true
	v.org.loose = 6
	v.org.cost = "about $0.0020"
	reserved := teamReservedFrom(darkRamp)
	hues := wallViewHues(v)
	h1 := nextTeamHue(hues, reserved)
	h2 := nextTeamHue(append(hues, h1), reserved)
	v.org.props = []orgProp{
		{name: "codeaf", hue: h1, keys: []string{"k0", "k1", "k2", "k3", "k4"}, names: []string{"a", "b", "c", "d", "e"}, folder: true, take: true},
		{name: "nvda research", hue: h2, keys: []string{"k1", "k2", "k5"}, names: []string{"cpu profiling", "nvda deep dive into the tree", "10-K"}, reason: "one company", take: true},
		{team: wallTestIDs[0], name: "port", hue: v.teams[0].hue, keys: []string{"k3", "k4"}, names: []string{"relay audit", "footprint table"}, take: false},
	}
	return v
}

// EVERY ROW FITS AND EVERY TARGET LIES ON ITS LABEL, at 80, 120 and 180
// columns, in both tiers, with the card up and with the button and the minimap
// sharing the Teams row.
func TestOrganizeRowWidths(t *testing.T) {
	for pname, pal := range wallTestPalettes() {
		for _, sz := range [][2]int{{80, 24}, {120, 40}, {180, 50}, {80, 12}} {
			w, h := sz[0], sz[1]
			for _, n := range []int{6, 13} {
				v := orgFixture(pal)
				if n != 6 {
					big := wallUnmarked(wallFixture(n))
					big.team, big.org = "", v.org
					v = big
				}
				for _, cardOn := range []bool{true, false} {
					v.org.on = cardOn
					name := fmt.Sprintf("%s/%dx%d/n%d/card%v", pname, w, h, n, cardOn)
					rows, hits := renderWall(pal, v, w, h)
					wallCheckRows(t, name, rows, w, h)
					wallCheckHits(t, name, hits, w, h)
					for _, hit := range hits {
						label := ansi.Strip(ansi.Cut(rows[hit.y0], hit.x0, hit.x1))
						switch {
						case hit.kind == wallHitAction && hit.arg == int(wallActOrganize):
							if !strings.Contains(label, "Organize") {
								t.Fatalf("%s: the button's target lies on %q", name, label)
							}
						case hit.kind == wallHitAction && hit.arg == int(wallActOrgApply):
							if strings.TrimSpace(label) != "Apply "+wallKeysFor(pal.ascii).enter {
								t.Fatalf("%s: Apply's target lies on %q", name, label)
							}
						case hit.kind == wallHitOrgRow:
							if !strings.Contains(label, v.org.props[hit.arg].name[:4]) {
								t.Fatalf("%s: row %d's target lies on %q", name, hit.arg, label)
							}
						}
					}
				}
			}
		}
	}
	// The mock's rows, at 120.
	pal := wallTestPalettes()["unicode"]
	rows, _ := renderWall(pal, orgFixture(pal), 120, 40)
	frame := wallPlainFrame(rows)
	for _, want := range []string{
		"☑ ● codeaf          5  from the folder",
		"☑ ● nvda research   3  cpu profiling, nvda deep dive…, 10-K",
		"☐ ● port          + 2  relay audit, footprint table",
		"about $0.0020",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the card lacks %q:\n%s", want, frame)
		}
	}
}

// THE FRAMES, printed for a person to look at: the All header with the
// button, the card with suggestions, and the Undo on the Teams row.
func TestOrganizePrintsFrames(t *testing.T) {
	pal := wallTestPalettes()["unicode"]
	v := wallUnmarked(wallFixture(6))
	v.team = ""
	v.org.loose = 6
	v.hover = wallHitRef{kind: wallHitAction, arg: int(wallActOrganize)}
	rows, _ := renderWall(pal, v, 120, 40)
	t.Logf("120x40, All with the Organize button hovered:\n%s", wallPlainFrame(rows))

	rows, _ = renderWall(pal, orgFixture(pal), 120, 40)
	t.Logf("120x40, the Organize card:\n%s", wallPlainFrame(rows))

	v.hover = wallHitRef{}
	v.org.doneAt, v.org.made, v.org.added = wallTestNow.Add(-2e9), 2, 2
	rows, _ = renderWall(pal, v, 120, 40)
	t.Logf("120x40, after Apply, Undo offered:\n%s", wallPlainFrame(rows))
}
