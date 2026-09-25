package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// titledAgent is a fake conversation that has a title, as a resumed one does.
type titledAgent struct {
	*fakeAgent
	title string
}

func (f titledAgent) Title() string { return f.title }

// teamAwayApp is trafficApp's team, harbor, with one member more that this window
// does not have open: a conversation the team kept from another day.
func teamAwayApp(t *testing.T) (a *app, harbor, awayKey string) {
	t.Helper()
	a, harbor, _, _ = trafficApp(t)
	file := "/tmp/lab/quantum-gravity.jsonl"
	awayKey = a.convKey(file)
	m := teamMember{Key: awayKey, File: file, Where: "/tmp/lab", Word: "quantum gravity research"}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.AddMember(harbor, m) }); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	a.touch()
	return a, harbor, awayKey
}

// THE STRIP AND THE WALL AGREE ABOUT A TEAM. Both are what is open in this
// window, narrowed to it: a member not open here is not a tab and not a tile,
// the Teams row counts it apart, and the title offers it once, `1 more in
// harbor · Open them`. The owner's screen read `test 1` over three tabs.
func TestTheStripAndTheWallAgreeAboutATeam(t *testing.T) {
	a, _, awayKey := teamAwayApp(t)
	if row := plain(a.tabsRow(a.width)); strings.Contains(row, "quantum") {
		t.Fatalf("a member not open here has a tab: %q", row)
	}
	for _, hit := range a.chatTabHits {
		if hit.tab.key == awayKey {
			t.Fatalf("a member not open here is a strip target: %+v", hit)
		}
	}
	spend(t, a, a.openWall())
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	open := len(a.wallShown(a.now()))
	for _, want := range []string{
		"open in this window · in harbor",
		"1 more in harbor · Open them",
		"harbor " + itoa(open),
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the wall lacks %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "quantum") {
		t.Fatalf("a member not open here is a tile:\n%s", frame)
	}
}

// OPEN THEM RESUMES BEHIND AND MOVES NOTHING. The member becomes a tab and a
// tile, the conversation in front and the box stay as they were, the focus
// stays on the tile it was on, and the button is gone because every member is
// open now (the emptiness law).
func TestOpenThemResumesTheRestBehindAndMovesNothing(t *testing.T) {
	a, _, awayKey := teamAwayApp(t)
	opened := 0
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened++
		return Conversation{Agent: titledAgent{&fakeAgent{model: "m"}, "quantum gravity research"}, SessionFile: transcript, Workspace: workspace}, nil
	}
	a.input.insert("half a thought")
	front := a.frontTabKey()
	spend(t, a, a.openWall())
	_ = a.wallFrame(a.width, a.height)
	focused := a.wallFocusedKey(a.wallShown(a.now()))
	spend(t, a, wallKeyPress(a, "r"))
	if opened != 1 {
		t.Fatalf("Open them opened %d conversations", opened)
	}
	if a.frontTabKey() != front || string(a.input.value) != "half a thought" {
		t.Fatalf("Open them moved the front: %q (was %q), box %q", a.frontTabKey(), front, string(a.input.value))
	}
	if a.behind[awayKey] == nil {
		t.Fatal("the member is not held behind")
	}
	if got := a.wallFocusedKey(a.wallShown(a.now())); got != focused {
		t.Fatalf("the focus moved from %q to %q", focused, got)
	}
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if strings.Contains(frame, wallResumeWord) {
		t.Fatalf("every member is open, yet the title still offers more:\n%s", frame)
	}
	if !strings.Contains(frame, "quantum gravity") {
		t.Fatalf("the resumed member is not a tile:\n%s", frame)
	}
	a.touch()
	if row := plain(a.tabsRow(a.width)); !strings.Contains(row, "quantum") {
		t.Fatalf("the resumed member has no tab: %q", row)
	}
}

// THE GRAMMAR. A handle of a member of a team this conversation is in is a
// link; an `@word` that is no one's handle, an address and this conversation's
// own handle are plain text; a team's name is a link where it is written as a
// team and plain where it is an ordinary word.
func TestTheTeamLinkGrammar(t *testing.T) {
	test := team{ID: "t1", Name: "test", Members: []teamMember{
		{Key: "k-sec", Handle: "security", Word: "santosh dev2 branch code complexity & security review"},
		{Key: "k-mil", Handle: "milestones", Word: "CodeAF repo issue tags & milestones"},
		{Key: "k-me", Handle: "boss"},
	}}
	pal := newPalette(tokens.ANSI256, false)
	for _, tc := range []struct {
		name, text string
		want       []string
	}{
		{"handles", "Ask @security, then @milestones.", []string{"@security", "@milestones"}},
		{"unknown handle", "Ask @nobody about it.", nil},
		{"own handle", "I am @boss here.", nil},
		{"address", "Write to santosh@security.dev today.", nil},
		{"team word before", "The team test has two members.", []string{"test"}},
		{"team word after", "Everything in the test team is done.", []string{"test"}},
		{"quoted", `Team traffic in "test" for you.`, []string{"test"}},
		{"ordinary word", "Run the test again.", nil},
		{"both", "@security is in team test.", []string{"@security", "test"}},
	} {
		out, links := linkifyTeams(tc.text, pal, []team{test}, []team{test}, "k-me", true, -1)
		var got []string
		for _, l := range links {
			got = append(got, ansi.Strip(ansi.Cut(out, l.span.from, l.span.to)))
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: %q linked %q, want %q", tc.name, tc.text, got, tc.want)
		}
		if ansi.Strip(out) != tc.text {
			t.Errorf("%s: the words changed: %q", tc.name, ansi.Strip(out))
		}
	}
}

// linkChat is trafficApp's manager in front, with one more member this window
// does not have open, @gravity, and a reply from the manager naming members.
func linkChat(t *testing.T, said string) (a *app, gravityKey string) {
	t.Helper()
	a, harbor, _, _ := trafficApp(t)
	file := "/tmp/lab/quantum-gravity.jsonl"
	gravityKey = a.convKey(file)
	m := teamMember{Key: gravityKey, File: file, Where: "/tmp/lab", Word: "quantum gravity research", Handle: "gravity"}
	if err := a.teamEdit(func(f *teamstore.File) error { return f.AddMember(harbor, m) }); err != nil {
		t.Fatal(err)
	}
	teamsFlush(t, a)
	a.width, a.height = 160, 30
	a.entries = append(a.entries, entry{kind: entryAssistant, text: said, settled: true})
	a.touch()
	return a, gravityKey
}

// teamLinkedRow is the first frame row carrying a team link, and its screen y.
func teamLinkedRow(a *app) (row, int, bool) {
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	for i, r := range body {
		for _, l := range r.links {
			if l.team != "" {
				return r, a.bodyTop() + i, true
			}
		}
	}
	return row{}, 0, false
}

// A REPLY'S HANDLES ARE DOORS WITH A GROUND AND A HINT, AND A PRESS RESUMES. The
// manager writes about @gravity, which this window does not have open, and
// @nobody, which is no one: the one is a link that lights on a ground under the
// pointer, says `Resume @gravity · quantum gravity research · click` on the
// hint line, and opens the conversation when pressed; the other stays text.
func TestAHandleInAReplyIsADoorThatResumesItsMember(t *testing.T) {
	a, gravityKey := linkChat(t, "Ask @gravity for the numbers, and @nobody else.")
	r, y, ok := teamLinkedRow(a)
	if !ok {
		t.Fatalf("the reply grew no team link:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if len(r.links) != 1 || r.links[0].member != gravityKey {
		t.Fatalf("links %+v, want @gravity alone", r.links)
	}
	if got := plainCells(ansi.Strip(r.text), r.links[0].span.from, r.links[0].span.to); got != "@gravity" {
		t.Fatalf("the link covers %q", got)
	}
	drive(t, a, motionTo(r.links[0].span.from+1, y))
	if got := a.hoveringLink(r.entry); got != r.links[0].ord || got < teamLinkOrd {
		t.Fatalf("the hover recorded %d, want %d", got, r.links[0].ord)
	}
	if strings.Contains(r.text, "\x1b[48") {
		t.Fatalf("a team link at rest wears a ground:\n%q", r.text)
	}
	again, _, _ := teamLinkedRow(a)
	if !strings.Contains(again.text, "\x1b[48") {
		t.Fatalf("a hovered team link took no ground:\n%q", again.text)
	}
	if hint := a.dockHoverWords(); hint != "Resume @gravity · quantum gravity research · click" {
		t.Fatalf("the hint line says %q", hint)
	}
	opened := ""
	a.open = func(workspace, transcript string) (Conversation, error) {
		opened = transcript
		return Conversation{Agent: titledAgent{&fakeAgent{model: "m"}, "quantum gravity research"}, SessionFile: transcript, Workspace: workspace}, nil
	}
	x := again.links[0].span.from + 1
	cmd := a.press(x, y)
	spend(t, a, cmd)
	if opened != "/tmp/lab/quantum-gravity.jsonl" || a.frontTabKey() != gravityKey {
		t.Fatalf("the press did not resume @gravity: opened %q, front %q", opened, a.frontTabKey())
	}
}

// A TOOL CALL AND A TEAM'S CARD CARRY THE SAME DOORS.
func TestTeamToolRowsAndCardsLinkTheirHandles(t *testing.T) {
	a, gravityKey := linkChat(t, "")
	a.entries = append(a.entries,
		entry{kind: entryTool, tool: "team_send", text: "team_send @gravity: send the numbers", settled: true},
		entry{kind: entryTeam, text: "Team traffic in \"harbor\" for you (@gravity).\n◆ from manager to @gravity: send the numbers\n(rule)", settled: true},
	)
	a.touch()
	rows := a.visible(a.bodyWidth())
	found := map[entryKind]bool{}
	for _, r := range rows {
		if r.entry < 0 {
			continue
		}
		for _, l := range r.links {
			if l.member == gravityKey {
				found[a.entries[r.entry].kind] = true
			}
		}
	}
	if !found[entryTeam] {
		t.Errorf("the team card's @gravity is not a link:\n%s", strings.Join(plainRows(a), "\n"))
	}
	if !found[entryTool] {
		t.Errorf("the team_send row's @gravity is not a link:\n%s", strings.Join(plainRows(a), "\n"))
	}
}
