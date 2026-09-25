package tui3

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TEST HELPERS FOR THE TEAMS FILE ─────────────────────────────────────────
//
// The interface reaches the store only through its seam (teamseam.go). These
// read and write the file directly, as fixtures and as the witness a test asks
// what reached the disk.

// teamsPath is where the sets live in profileDir.
func teamsPath(profileDir string) string { return teamstore.Path(profileDir) }

// loadTeams reads the sets from profileDir, coloured around reserved.
func loadTeams(profileDir string, reserved []float64) ([]team, error) {
	f, err := teamstore.LoadHued(profileDir, reserved)
	if err != nil {
		return nil, err
	}
	return f.Teams, nil
}

// saveTeams writes the sets to profileDir as the whole file, as a fixture.
func saveTeams(profileDir string, s []team) error { return teamstore.Save(profileDir, s) }

// teamsFlush writes the edits this window has queued, and folds the write in,
// as the loop does after the message that made them.
func teamsFlush(t *testing.T, a *app) {
	t.Helper()
	spend(t, a, a.teamsWrite())
}

// farTeams is a seam onto a profile of the test's own that stands for the
// engine's, the way the --host door's seam stands for the far machine. held
// false makes its Load answer "nothing held yet", as a connection does before
// its first answer. reads counts the reads made off the loop.
type farTeams struct {
	dir   string
	watch teamstore.Watch
	mu    sync.Mutex
	held  bool
	reads int
}

func (f *farTeams) seam() TeamsSeam {
	local := localTeams(f.dir, &f.watch)
	return TeamsSeam{
		Load: func(reserved []float64) ([]teamstore.Team, string, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.held {
				return nil, "", false
			}
			return local.Load(reserved)
		},
		ReadSince: func(since string, reserved []float64) ([]teamstore.Team, string, bool, error) {
			f.mu.Lock()
			f.reads++
			f.mu.Unlock()
			return local.ReadSince(since, reserved)
		},
		Update:  local.Update,
		Traffic: local.Traffic,
	}
}

// THE LOCAL SEAM IS THE STORE IN THE PROFILE. A load of no file is known and
// empty; an update answers what it wrote and a stamp; a read at that stamp is
// "same" and carries nothing; a quiet log after its cursor is nothing.
func TestTheLocalTeamsSeamIsTheStoreInTheProfile(t *testing.T) {
	dir := t.TempDir()
	var watch teamstore.Watch
	seam := localTeams(dir, &watch)
	teams, stamp, known := seam.Load(nil)
	if !known || len(teams) != 0 || stamp != teamstore.MissingStamp {
		t.Fatalf("a load of no file: %v, %q, %v", teams, stamp, known)
	}
	wrote, stamp, err := seam.Update(func(f *teamstore.File) error {
		f.Teams = append(f.Teams, team{ID: "0a0a0a0a0a0a", Name: "harbor"})
		return nil
	})
	if err != nil || len(wrote) != 1 || stamp == teamstore.MissingStamp {
		t.Fatalf("the update: %v, %q, %v", wrote, stamp, err)
	}
	if got, again, same, err := seam.ReadSince(stamp, nil); err != nil || !same || got != nil || again != stamp {
		t.Fatalf("a read at the written stamp: %v, %q, %v, %v", got, again, same, err)
	}
	if got, _, same, _ := seam.ReadSince("", nil); same || len(got) != 1 {
		t.Fatalf("a first read: %v, same %v", got, same)
	}
	if err := teamstore.AppendTraffic(dir, "0a0a0a0a0a0a", teamstore.Entry{Kind: teamstore.KindNote, From: "a", To: "b", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	tail, err := seam.Traffic("0a0a0a0a0a0a", "", 10)
	if err != nil || len(tail) != 1 {
		t.Fatalf("the tail: %v, %v", tail, err)
	}
	if more, _ := seam.Traffic("0a0a0a0a0a0a", tail[0].ID, 10); len(more) != 0 {
		t.Fatalf("a quiet log answered %v", more)
	}
}

// AN EDIT IS DRAWN AT ONCE AND WRITTEN OFF THE LOOP. teamMake changes what the
// window holds and touches no disk; the command the loop hands back after it
// writes the file, and what it wrote is what the window holds afterwards.
func TestATeamEditIsWrittenOffTheLoop(t *testing.T) {
	a, _, _ := tabApp(t)
	a.profileDir = t.TempDir()
	id, err := a.teamMake("harbor", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.teamByID(id); !ok {
		t.Fatal("the edit is not in what the window holds")
	}
	if _, err := os.Stat(teamsPath(a.profileDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the edit wrote the disk on the loop: %v", err)
	}
	if a.traffic.wrote == a.traffic.edits {
		t.Fatal("an unwritten edit reads as written")
	}
	teamsFlush(t, a)
	on, err := loadTeams(a.profileDir, nil)
	if err != nil || len(on) != 1 || on[0].ID != id {
		t.Fatalf("the write: %+v, %v", on, err)
	}
	if a.traffic.wrote != a.traffic.edits || a.traffic.stamp != teamstore.Stamp(a.profileDir) {
		t.Fatalf("the fold did not take the write (wrote %d of %d, stamp %q)", a.traffic.wrote, a.traffic.edits, a.traffic.stamp)
	}
	if a.teamsWrite() != nil {
		t.Fatal("a window with nothing queued asked for a write")
	}
}

// OVER --host WITH A SEAM, NOTHING IS REFUSED AND THE LAPTOP'S FILE IS LEFT
// ALONE. The window's own profile stands for the laptop and the seam's for the
// engine. A team, its manager and a member's handle are written to the
// engine's file; the laptop's teams.json is never created; nothing says
// managers are not available; the manager's Traffic, written in the engine's
// profile, reaches the rail.
func TestTeamsOverAHostedSeamRefuseNothingAndLeaveTheLaptopFileAlone(t *testing.T) {
	a, _, _ := tabApp(t)
	laptop := t.TempDir()
	a.profileDir, a.host = laptop, "devbox"
	far := &farTeams{dir: t.TempDir(), held: true}
	a.teamsDisk.door = far.seam()
	if a.teamsOff() {
		t.Fatal("a hosted window with a seam has its teams off")
	}
	id, err := a.teamMake("harbor", a.tabList())
	if err != nil {
		t.Fatal(err)
	}
	a.teamActivate(id)
	front := a.frontTabKey()
	for _, tab := range a.tabList() {
		if tab.key == front {
			if err := a.teamMakeManager(id, tab); err != nil {
				t.Fatal(err)
			}
		}
	}
	teamsFlush(t, a)
	for _, e := range a.entries {
		if e.kind == entryNote && strings.Contains(e.text, teamHostedWord) {
			t.Fatalf("a hosted window with a seam said %q", e.text)
		}
	}
	on, err := loadTeams(far.dir, nil)
	if err != nil || len(on) != 1 || on[0].Manager != front {
		t.Fatalf("the engine's file: %+v, %v", on, err)
	}
	if _, err := os.Stat(teamsPath(laptop)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the laptop's teams file was touched: %v", err)
	}
	if !a.trafficWanted() || a.sideKind() != sideKindManager {
		t.Fatal("the manager in front over --host has no clock or no Traffic")
	}
	handle := ""
	for _, m := range on[0].Members {
		if m.Key != front && m.Handle != "" {
			handle = m.Handle
			break
		}
	}
	if err := teamstore.AppendTraffic(far.dir, id, teamstore.Entry{Kind: teamstore.KindDirective,
		From: teamstore.FromManager, To: handle, Text: "take the lexer"}); err != nil {
		t.Fatal(err)
	}
	trafficReadNow(t, a)
	rows := a.traffic.rows[id]
	if len(rows) != 1 || rows[0].Text != "take the lexer" {
		t.Fatalf("the cache over --host holds %+v", rows)
	}
	// AND THE SIDE COLUMN DRAWS IT FROM THAT CACHE: the manager's Traffic in
	// front, the directive a row of work, and the frames that draw it write
	// nothing on the laptop.
	a.width, a.height = 160, 40
	a.welcome.open = false
	if col := strings.Join(railLines(t, a), "\n"); !strings.Contains(col, "take the lexer") || a.sideView() != sideTraffic {
		t.Fatalf("the column over --host does not draw the engine's Traffic:\n%s", col)
	}
	if entries, _ := os.ReadDir(laptop); len(entries) != 0 {
		t.Fatalf("the laptop's profile holds %v", entries)
	}
}

// AN ENGINE WITHOUT THE TEAMS DOORS TURNS TEAMS OFF, IT DOES NOT FALL BACK TO
// THE LAPTOP. With --host and no seam an edit is refused, the manager's door
// says the honest line, the clock does not run, and the laptop's profile is
// never written.
func TestAnOlderEngineTurnsTeamsOffOverHost(t *testing.T) {
	a, _, _ := tabApp(t)
	laptop := t.TempDir()
	a.profileDir, a.host = laptop, "devbox"
	if !a.teamsOff() {
		t.Fatal("a hosted window with no seam keeps teams")
	}
	if _, err := a.teamMake("harbor", a.tabList()); !errors.Is(err, errTeamsHosted) {
		t.Fatalf("an edit over an older engine: %v", err)
	}
	var front chatTab
	for _, tab := range a.tabList() {
		if tab.key == a.frontTabKey() {
			front = tab
		}
	}
	a.wall.teams = []team{{ID: "0a0a0a0a0a0a", Name: "harbor", Members: []teamMember{{Key: front.key}}, Manager: front.key}}
	if err := a.teamMakeManager("0a0a0a0a0a0a", front); err != nil {
		t.Fatal(err)
	}
	if got := lastNote(t, a); got != teamHostedWord {
		t.Fatalf("the manager's door said %q", got)
	}
	if a.trafficWanted() || a.teamsWrite() != nil {
		t.Fatal("an older engine left the clock or a write running")
	}
	if entries, _ := os.ReadDir(laptop); len(entries) != 0 {
		t.Fatalf("the laptop's profile holds %v", entries)
	}
}

// A SEAM THAT HOLDS NOTHING YET IS READ OFF THE LOOP. The opening finds nothing
// held, holds no teams and asks; the answer is folded in and the teams are
// there, and the ask was made by the command, never by the opening.
func TestAHostedSeamThatHoldsNothingYetIsReadOffTheLoop(t *testing.T) {
	a, _, _ := tabApp(t)
	a.profileDir, a.host = t.TempDir(), "devbox"
	far := &farTeams{dir: t.TempDir()}
	if err := saveTeams(far.dir, []team{{ID: "0a0a0a0a0a0a", Name: "harbor"}}); err != nil {
		t.Fatal(err)
	}
	a.teamsDisk.door = far.seam()
	a.wall.loaded = false
	a.teamsEnsure()
	if a.wall.loaded || len(a.wall.teams) != 0 || far.reads != 0 {
		t.Fatalf("the opening read on the loop (loaded %v, %d teams, %d reads)", a.wall.loaded, len(a.wall.teams), far.reads)
	}
	teamsFlush(t, a)
	if !a.wall.loaded || len(a.wall.teams) != 1 || a.wall.teams[0].Name != "harbor" || far.reads != 1 {
		t.Fatalf("the read off the loop: loaded %v, %+v, %d reads", a.wall.loaded, a.wall.teams, far.reads)
	}
}

// THE LOCAL SEAM CARRIES THE DELEGATION DOORS ONTO THIS PROFILE, and a seam
// handed without them says so. A packet raised through the local seam is read
// back through it, a second read at the stamp is same, and the spend of a team
// nobody has spent in is zero with a stamp that answers same.
func TestTheLocalTeamsSeamCarriesTheDelegationDoors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", t.TempDir())
	var watch teamstore.Watch
	seam := localTeams(dir, &watch)
	if !seam.delegation() {
		t.Fatal("the local seam has no delegation doors")
	}
	if (TeamsSeam{Load: seam.Load, Update: seam.Update}).delegation() {
		t.Fatal("a seam without the doors says it has them")
	}
	if err := teamstore.Save(dir, []teamstore.Team{{ID: "0a0a0a0a0a0a", Name: "harbor", Manager: "hm",
		Members: []teamstore.Member{{Key: "hm", Handle: "boss"}}}}); err != nil {
		t.Fatal(err)
	}
	if d, err := seam.Defaults(); err != nil || d.DepthLimit != 3 {
		t.Fatalf("defaults %+v %v", d, err)
	}
	p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: "0a0a0a0a0a0a",
		Kind: teamstore.PacketQuestion, RaisedBy: "boss", Question: "Friday or Monday?"})
	if err != nil {
		t.Fatal(err)
	}
	mine, stamp, same, err := seam.Packets(teamstore.Person, "")
	if err != nil || same || len(mine) != 1 || mine[0].ID != p.ID {
		t.Fatalf("the person's packets %+v %v %v", mine, same, err)
	}
	if _, _, same, _ := seam.Packets(teamstore.Person, stamp); !same {
		t.Fatal("a quiet read was not same")
	}
	spend, at, same, err := seam.Spend("0a0a0a0a0a0a", "", "")
	if err != nil || same || spend.USD != 0 {
		t.Fatalf("spend %+v %v %v", spend, same, err)
	}
	if _, _, same, _ := seam.Spend("0a0a0a0a0a0a", "", at); !same {
		t.Fatal("a quiet spend was not same")
	}
}
