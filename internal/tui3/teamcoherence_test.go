package tui3

import (
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
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
