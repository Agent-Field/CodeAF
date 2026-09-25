package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── HOW THE TEAMS PAGE READS (teamspage.go's [app.teamsRead]) ───────────────

// A TEAM CHOSEN WHILE A READ IS OUT IS READ, NOT DROPPED. The beat's read left
// with harbor selected; orbit is chosen before it answers, and orbit's spend is
// on the page when everything has landed, with no second beat. The choice's
// read used to be dropped because one was out, and a team chosen in that window
// showed no spend until the next beat, or ever on a page whose clock was still.
func TestTeamsAChoiceWhileAReadIsOutIsReadNotDropped(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	if a.tp.sel != harbor {
		t.Fatalf("the lab opened on %q, want harbor", a.tp.sel)
	}
	if _, read := a.tp.spend[orbit]; read {
		t.Fatal("the lab already read orbit's spend")
	}
	beat := a.teamsRead(true)
	if beat == nil {
		t.Fatal("the beat's read was not made")
	}
	choice := a.teamsSelect(orbit)
	drive(t, a, runCmd(beat)...)
	drive(t, a, runCmd(choice)...)
	if _, read := a.tp.spend[orbit]; !read {
		t.Fatalf("orbit was chosen while a read was out and its spend was never read: %+v", a.tp.spend)
	}
	if a.tp.reading || a.tp.again {
		t.Fatalf("a read is still out or owed after everything landed (reading %v, again %v)", a.tp.reading, a.tp.again)
	}
}

// A READ ASKS ABOUT THE OPEN TEAMS' MEMBERS AND NO OTHER CONVERSATION. On this
// machine's disk the rows are read by name through the page's door, never by a
// walk of every session under the root; over a connection they are the
// members' rows out of the world the window holds, and the page keeps nothing
// else.
func TestTeamsReadAsksAboutTheMembersAlone(t *testing.T) {
	a, _, _ := teamsPlaceLabIDs(t)
	members := map[string]bool{}
	for _, tm := range a.wall.teams {
		if tm.Closed() {
			continue
		}
		for _, m := range tm.Members {
			members[filepath.Clean(m.File)] = true
		}
	}
	if len(members) == 0 {
		t.Fatal("the lab has no members")
	}
	stranger := filepath.Join(t.TempDir(), "-work-else", "aaaa000000000009", "transcript.jsonl")

	// This machine's disk.
	a.world = nil
	var calls int
	var asked []string
	a.teamsDisk.rows = func(files []string) map[string]session.SessionRow {
		calls++
		asked = append(asked, files...)
		out := map[string]session.SessionRow{}
		for _, f := range files {
			out[f] = session.SessionRow{Transcript: f}
		}
		return out
	}
	drive(t, a, runCmd(a.teamsRead(true))...)
	if calls != 1 {
		t.Fatalf("one read asked the rows door %d times", calls)
	}
	if len(asked) != len(members) {
		t.Fatalf("the read asked about %d conversations, want the %d members: %q", len(asked), len(members), asked)
	}
	for _, f := range asked {
		if !members[f] {
			t.Fatalf("the read asked about %q, which is no open team's member", f)
		}
	}
	if len(a.tp.world) != len(members) {
		t.Fatalf("the page holds %d rows, want the %d members'", len(a.tp.world), len(members))
	}

	// Over a connection: the world the window holds, cut to the members.
	var rows []session.SessionRow
	for f := range members {
		rows = append(rows, session.SessionRow{Transcript: f, Title: "member"})
	}
	rows = append(rows, session.SessionRow{Transcript: stranger, Title: "stranger"})
	a.world = func() (session.World, bool) {
		return session.World{Projects: []session.Project{{Sessions: rows}}}, true
	}
	calls = 0
	a.tp.world = nil
	drive(t, a, runCmd(a.teamsRead(true))...)
	if calls != 0 {
		t.Fatal("a window with a world door read this machine's disk")
	}
	if _, kept := a.tp.world[stranger]; kept || len(a.tp.world) != len(members) {
		t.Fatalf("the page kept %d rows over a connection, want the %d members' alone", len(a.tp.world), len(members))
	}
}

// THE FIRST FRAME IS DRAWN FROM MEMORY AND THE READ FILLS IT IN PLACE. The
// page's rail and the selected team's header are on the frame the opening
// draws, before any read has answered, with no word or mark saying it is
// loading; and the read landing moves none of those rows.
func TestTeamsFirstFrameIsDrawnBeforeAnyReadAndTheReadMovesNothing(t *testing.T) {
	a, harbor, orbit := menuApp(t)
	a.profileDir = t.TempDir()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetParent(orbit, harbor) }); err != nil {
		t.Fatal(err)
	}
	flushTeams(t, a)
	a.width, a.height = 120, 24
	cmd := a.showPage(pageTeams)
	before := strings.Split(teamsFrameText(a), "\n")
	text := strings.Join(before, "\n")
	for _, want := range []string{"All teams", "harbor", "orbit", "Settings", "Close"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the first frame, drawn before any read, lacks %q:\n%s", want, text)
		}
	}
	for _, loading := range []string{"loading", "reading", "⠋", "⠙", "⠹"} {
		if strings.Contains(text, loading) {
			t.Fatalf("the first frame says %q while the read is out:\n%s", loading, text)
		}
	}
	drive(t, a, runCmd(cmd)...)
	after := strings.Split(teamsFrameText(a), "\n")
	if len(after) != len(before) {
		t.Fatalf("the read changed the frame's height from %d to %d", len(before), len(after))
	}
	for y := range before {
		if before[y] != after[y] {
			t.Fatalf("the read moved row %d:\nbefore %q\nafter  %q", y, before[y], after[y])
		}
	}
}
