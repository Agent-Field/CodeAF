package teams

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func digestTeam() Team {
	return Team{ID: "t1", Name: "harbor", Manager: "m", Members: []Member{
		{Key: "m", Word: "Harbor manager", Handle: "lead"},
		{Key: "a", Word: "Fix the login bug", Handle: "fix-login"},
		{Key: "b", Word: "Write the docs", Handle: "docs"},
		{Key: "c"},
	}}
}

func trafficFixture(n int) []Entry {
	var out []Entry
	at := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		out = append(out, Entry{ID: fmt.Sprintf("%012d", i), At: at.Add(time.Duration(i) * time.Minute),
			Kind: KindNote, From: "fix-login", To: ToManager, Text: fmt.Sprintf("line %d", i)})
	}
	return out
}

func TestDigestContent(t *testing.T) {
	states := map[string]MemberState{
		"a": {State: StateAsking, SinceActive: 3 * time.Minute, Question: "Which\ndatabase?",
			Files: []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"}},
		"b": {State: StateFinished, SinceActive: 26 * time.Hour},
	}
	recent := trafficFixture(3)
	recent[2].Files = []string{"auth/login.go"}
	got := Digest(digestTeam(), states, recent, 0)
	for _, want := range []string{
		`Team "harbor": 4 members, manager @lead.`,
		`- @lead (manager) "Harbor manager": unknown`,
		`- @fix-login "Fix the login bug": asking, active within the hour. Asking: "Which database?". Files: a.go, b.go, c.go, d.go, e.go +2 more`,
		`- @docs "Write the docs": finished, active 26h ago`,
		`- [c]: unknown`,
		"Recent traffic:\n- 09:01 note fix-login -> manager: line 1\n",
		`- 09:03 note fix-login -> manager: line 3 [auth/login.go]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the digest lacks %q:\n%s", want, got)
		}
	}
	// The same inputs give the same text.
	if again := Digest(digestTeam(), states, recent, 0); again != got {
		t.Fatal("the digest is not deterministic")
	}
}

// THE BUDGET CUTS TRAFFIC FROM THE OLDEST END, and never the members, until
// the members alone do not fit.
func TestDigestBudget(t *testing.T) {
	team := digestTeam()
	full := Digest(team, nil, trafficFixture(40), 0)
	if strings.Contains(full, "manager: line 20\n") || !strings.Contains(full, "manager: line 21\n") || !strings.Contains(full, "manager: line 40\n") {
		t.Fatalf("more than the last twenty lines were shown:\n%s", full)
	}
	members := Digest(team, nil, nil, 0)
	budget := utf8.RuneCountInString(members) + 200
	cut := Digest(team, nil, trafficFixture(40), budget)
	if n := utf8.RuneCountInString(cut); n > budget {
		t.Fatalf("%d characters over a budget of %d", n, budget)
	}
	if !strings.HasPrefix(cut, members) || !strings.Contains(cut, "line 40\n") || strings.Contains(cut, "line 21\n") {
		t.Fatalf("the cut kept the wrong lines:\n%s", cut)
	}
	// Too small for the members: cut at the budget, marked.
	tiny := Digest(team, nil, trafficFixture(3), 30)
	if utf8.RuneCountInString(tiny) != 30 || !strings.HasSuffix(tiny, "…") {
		t.Fatalf("a tiny budget gave %q", tiny)
	}
	// Exactly the members: no traffic heading hanging on its own.
	if exact := Digest(team, nil, trafficFixture(3), utf8.RuneCountInString(members)); exact != members {
		t.Fatalf("a members-only budget gave:\n%s", exact)
	}
}

// THE DIGEST SAYS WHY HANDED-OUT WORK IS NOT MOVING: a member the person paused,
// a directive nobody started on, a turn the person stopped, and a member that
// has not been given work yet. A manager told only "idle" hands the same work
// to the same member again.
func TestDigestSaysPausedStandbyAndUnstartedWork(t *testing.T) {
	team := Team{Name: "harbor", Manager: "m", Members: []Member{
		{Key: "m", Handle: "boss", Word: "Harbor manager"},
		{Key: "a", Handle: "dev", Word: "Scaffold the app", Role: "macOS developer", Paused: true},
		{Key: "b", Handle: "review", Role: "Reviewer", Standby: true},
		{Key: "c", Handle: "design", Word: "Design the tiles"},
		{Key: "d", Handle: "docs", Word: "Write the guide"},
		{Key: "e", Handle: "busy", Word: "Port the parser"},
		{Key: "f", Handle: "shared", Word: "Shared helper"},
	}}
	at := time.Date(2026, 11, 23, 22, 2, 0, 0, time.UTC)
	recent := []Entry{
		{ID: "000000000021", At: at, Kind: KindDirective, From: FromManager, To: "docs", Text: "Write it."},
		{ID: "000000000022", At: at, Kind: KindDirective, From: FromManager, To: "dev", Text: "Build it."},
		{ID: "000000000023", At: at, Kind: KindEvent, From: FromSystem, To: "dev", Member: "a", State: StateIdle, Text: "held @dev: the person paused it", Answers: "000000000022"},
		{ID: "000000000024", At: at, Kind: KindEvent, From: FromManager, To: "docs", Member: "d", State: StateRunning, Text: "woke @docs", Answers: "000000000021"},
		{ID: "000000000025", At: at, Kind: KindEvent, From: "design", To: ToManager, Member: "c", State: StateIdle, Text: StoppedByPerson},
		// Sent to a member in a turn: it reads it at its next step, with no wake line.
		{ID: "000000000026", At: at, Kind: KindDirective, From: FromManager, To: "busy", Text: "Then the lexer."},
	}
	states := map[string]MemberState{
		"m": {State: StateRunning},
		"a": {State: StateIdle, SinceActive: time.Hour},
		// The journal of a member that never ran still has an age: its file's.
		"b": {State: StateIdle, SinceActive: time.Minute},
		"c": {State: StateIdle, SinceActive: time.Minute},
		"d": {State: StateRunning, SinceActive: time.Minute},
		"e": {State: StateRunning, SinceActive: time.Minute},
		"f": {State: StateIdle, SinceActive: time.Minute, Paused: true},
	}
	got := Digest(team, states, recent, 0)
	for _, want := range []string{
		`- @boss (manager) "Harbor manager": running`,
		`- @dev, macOS developer "Scaffold the app": ` + DigestPaused + `, has not started on #22, active 1h ago`,
		`- @review, Reviewer: ` + DigestStandby + `, active within the hour`,
		`- @design "Design the tiles": idle, its last turn was ` + StoppedByPerson + `, active within the hour`,
		`- @docs "Write the guide": running, active within the hour`,
		`- @busy "Port the parser": running, active within the hour`,
		// Paused by the person in another of its teams.
		`- @shared "Shared helper": ` + DigestPaused + `, active within the hour`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("the digest lacks the line\n%s\nin\n%s", want, got)
		}
	}
	// Standby is the member's record, cleared when its role line is handed to it.
	team.Members[2].Standby = false
	if got := Digest(team, states, nil, 0); strings.Contains(got, DigestStandby) {
		t.Errorf("a member whose standby is over still reads as on standby:\n%s", got)
	}
}

func TestSetPausedAndSetRole(t *testing.T) {
	file := &File{Teams: []Team{{ID: "t", Name: "harbor", Manager: "m", Members: []Member{{Key: "m", Handle: "boss"}, {Key: "a", Handle: "dev"}}}}}
	if err := file.SetPaused("t", "a", true); err != nil || !file.Teams[0].Members[1].Paused {
		t.Fatalf("pause: %v, %+v", err, file.Teams[0].Members[1])
	}
	if err := file.SetPaused("t", "a", false); err != nil || file.Teams[0].Members[1].Paused {
		t.Fatalf("resume: %v, %+v", err, file.Teams[0].Members[1])
	}
	if err := file.SetPaused("t", "m", true); err == nil {
		t.Fatal("the manager was paused")
	}
	if err := file.SetPaused("t", "x", true); err == nil {
		t.Fatal("a stranger was paused")
	}
	if err := file.SetRole("t", "a", "  macOS\n developer "); err != nil || file.Teams[0].Members[1].Role != "macOS developer" {
		t.Fatalf("role: %v, %q", err, file.Teams[0].Members[1].Role)
	}
	raw, err := json.Marshal(Member{Key: "a", Paused: true, Role: "Reviewer", Standby: true})
	if err != nil {
		t.Fatal(err)
	}
	var back Member
	if err := json.Unmarshal(raw, &back); err != nil || !back.Paused || !back.Standby || back.Role != "Reviewer" || len(back.extra) != 0 {
		t.Fatalf("a member did not survive its own JSON: %+v, %v (%s)", back, err, raw)
	}
}

// A manager woken by its member's stopped turn still reads that it was stopped:
// the wake line is written under the member's name, about the manager.
func TestDigestKeepsTheStoppedTurnAfterTheManagersWake(t *testing.T) {
	team := Team{Name: "harbor", Manager: "m", Members: []Member{{Key: "m", Handle: "boss"}, {Key: "a", Handle: "web"}, {Key: "b", Handle: "api"}}}
	at := time.Date(2026, 11, 23, 22, 2, 0, 0, time.UTC)
	recent := []Entry{
		{ID: "000000000001", At: at, Kind: KindDirective, From: FromManager, To: "web", Text: "Build it."},
		{ID: "000000000002", At: at, Kind: KindEvent, From: FromManager, To: "web", Member: "a", State: StateRunning, Text: "woke @web"},
		{ID: "000000000003", At: at, Kind: KindEvent, From: "web", To: ToManager, Member: "a", State: StateIdle, Text: StoppedByPerson},
		{ID: "000000000004", At: at, Kind: KindEvent, From: "web", To: ToManager, Member: "m", State: StateRunning, Text: "woke ◆"},
		{ID: "000000000005", At: at, Kind: KindDirective, From: FromManager, To: "api", Text: "Then this."},
	}
	states := map[string]MemberState{"a": {State: StateIdle}, "b": {State: StateFailed}}
	got := Digest(team, states, recent, 0)
	for _, want := range []string{
		"- @web: idle, its last turn was " + StoppedByPerson,
		// A member whose last turn failed is not in a turn either.
		"- @api: failed, has not started on #5",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("the digest lacks the line\n%s\nin\n%s", want, got)
		}
	}
}

// A conversation that manages a team anywhere is not paused, a resume lifts a
// pause in every team, and becoming a manager lifts it too.
func TestPauseAcrossTeams(t *testing.T) {
	file := &File{Teams: []Team{
		{ID: "top", Name: "harbor", Manager: "m", Members: []Member{{Key: "m", Handle: "boss"}, {Key: "lead", Handle: "lead"}, {Key: "a", Handle: "dev"}}},
		{ID: "sub", Name: "dock", Parent: "top", Manager: "lead", Members: []Member{{Key: "lead", Handle: "lead"}, {Key: "a", Handle: "dev"}}},
	}}
	if err := file.SetPaused("top", "lead", true); err == nil || !strings.Contains(err.Error(), "manages dock") {
		t.Fatalf("pausing a sub-team's manager in the team above answered %v", err)
	}
	if err := file.SetPaused("top", "a", true); err != nil {
		t.Fatal(err)
	}
	if name, paused := file.PausedAnywhere("a"); !paused || name != "harbor" {
		t.Fatalf("paused anywhere: %q %v", name, paused)
	}
	if err := file.SetPaused("sub", "a", false); err != nil {
		t.Fatal(err)
	}
	if _, paused := file.PausedAnywhere("a"); paused {
		t.Fatal("a resume from another team left the pause")
	}
	if err := file.SetPaused("top", "a", true); err != nil {
		t.Fatal(err)
	}
	file.Teams = append(file.Teams, Team{ID: "new", Name: "pier", Members: []Member{{Key: "a", Handle: "dev"}}})
	if err := file.SetManager("new", "a"); err != nil {
		t.Fatal(err)
	}
	if _, paused := file.PausedAnywhere("a"); paused {
		t.Fatal("a conversation made a manager is still paused")
	}
}

// A crew's manager seated in the root team, the one that holds every crew, is
// not paused there: it manages its crew, and the engine's wake rules take a
// manager to be never paused.
func TestPauseRefusesACrewManagerInTheRootTeam(t *testing.T) {
	file := &File{Teams: []Team{
		{ID: "all", Name: "All teams", Root: true, Manager: "g", Members: []Member{{Key: "g", Handle: "global"}, {Key: "lead", Handle: "lead"}}},
		{ID: "crew", Name: "dock", Parent: "all", Manager: "lead", Members: []Member{{Key: "lead", Handle: "lead"}, {Key: "a", Handle: "dev"}}},
	}}
	if err := file.SetPaused("all", "lead", true); err == nil || !strings.Contains(err.Error(), "manages dock") {
		t.Fatalf("pausing a crew's manager in the root team answered %v", err)
	}
	if _, paused := file.PausedAnywhere("lead"); paused {
		t.Fatal("a refused pause left the crew's manager paused")
	}
}
