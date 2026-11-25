package session

// NESTED TEAMS, AS TESTS: a manager starts a sub-team whose manager makes
// itself one on its brief and reports up; orders go one level down and a
// handle below is pointed at its manager; a conflict goes to the lowest
// common manager, is ruled there, and the ruling reaches every party; the
// global manager runs the top-level managers and only them; and a sub-team
// with no manager of its own answers to the one above. Every fixture is a real
// teams.json and real Traffic, through internal/teams.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamTree is a profile and a set of named transcripts, each in a session folder
// of its own, for trees the flat fixture cannot draw.
type teamTree struct {
	fixture teamFixture
	paths   map[string]string
}

func newTeamTree(t *testing.T, names ...string) teamTree {
	t.Helper()
	root := t.TempDir()
	n := teamTree{fixture: teamFixture{profile: filepath.Join(root, "profile")}, paths: map[string]string{}}
	for _, name := range names {
		dir := filepath.Join(root, "sessions", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, placeTranscript)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		n.paths[name] = path
	}
	return n
}

// member is the teams record for the transcript called name, with handle.
func (n teamTree) member(t *testing.T, name, handle string) teams.Member {
	t.Helper()
	path := n.paths[name]
	return teams.Member{Key: convKeyOf(t, path), File: path, Word: name + " work", Handle: handle}
}

func (n teamTree) key(t *testing.T, name string) string { return convKeyOf(t, n.paths[name]) }

func (n teamTree) agent(t *testing.T, name string) *Agent {
	t.Helper()
	return teamAgent(t, n.fixture, n.paths[name], nil, nil)
}

// team adds a team with a manager (by transcript name, "" for none) and
// members, under parent ("" for the top), with the auto-wake off so every
// delivery here is read at the boundaries the test runs.
func (n teamTree) team(t *testing.T, name, parent, manager string, members ...teams.Member) string {
	t.Helper()
	id := teams.NewID()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(teams.Update(n.fixture.profile, func(file *teams.File) error {
		off := false
		team := teams.Team{ID: id, Name: name, Parent: parent}
		team.Settings.Wake = &off
		file.Teams = append(file.Teams, team)
		for _, m := range members {
			if err := file.AddMember(id, m); err != nil {
				return err
			}
		}
		if manager != "" {
			return file.SetManager(id, n.key(t, manager))
		}
		return nil
	}))
	return id
}

func (n teamTree) file(t *testing.T) *teams.File {
	t.Helper()
	f, err := teams.Load(n.fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// ── 1. a sub-team ───────────────────────────────────────────────────────────

// team_start OF KIND TEAM makes the child under the manager's team with its
// share of the pool, moves in the members named, writes the start the
// interface carries out, and the conversation it opens makes itself the
// child's manager on its brief and reports up.
func TestTeamStartOfKindTeamMakesASubTeamWhoseManagerReportsUp(t *testing.T) {
	n := newTeamTree(t, "boss", "web", "parser", "api")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"), n.member(t, "parser", "parser"))
	capUSD := 10.0
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		return f.SetSettings(harbor, func(s *teams.Settings) { s.CapUSDDay = &capUSD })
	}); err != nil {
		t.Fatal(err)
	}
	boss := n.agent(t, "boss")
	boss.teamBoundary()
	said, failed := callTool(t, boss.teamStartTool, `{"handle":"api","brief":"Build the signup API.\nDone is a green handler test.","kind":"team","name":"backend","members":["parser"]}`)
	if failed || !strings.Contains(said, `Made the team "backend" under "harbor"`) || !strings.Contains(said, "$5.00 a day") || !strings.Contains(said, "Moved in: @parser") {
		t.Fatalf("team_start of kind team said %q", said)
	}
	f := n.file(t)
	var backend teams.Team
	for _, team := range f.Teams {
		if team.Name == "backend" {
			backend = team
		}
	}
	if backend.Parent != harbor || !backend.Holds(n.key(t, "parser")) || backend.Settings.CapUSDDay == nil || *backend.Settings.CapUSDDay != 5 {
		t.Fatalf("the sub-team: %+v", backend)
	}
	if h, _ := f.Team(harbor); h.Holds(n.key(t, "parser")) {
		t.Fatal("the moved member is still in harbor")
	}
	log, _ := teams.ReadTraffic(n.fixture.profile, harbor, "", 0)
	start := last(log)
	if !strings.Contains(said, "("+teams.ThreadNumber(start.ID)+")") {
		t.Fatalf("start receipt lacks its root ID: %q", said)
	}
	if start.Kind != teams.KindStart || start.To != "api" || start.Team != backend.ID {
		t.Fatalf("harbor's start line: %+v", start)
	}
	here, _ := teams.ReadTraffic(n.fixture.profile, backend.ID, "", 0)
	if len(here) != 2 || here[0].Text != "@parser joined from harbor" || !strings.Contains(here[1].Text, "its manager @api starts on the brief") {
		t.Fatalf("backend's own lines: %+v", here)
	}

	// The interface carries out the start: the new conversation joins harbor
	// under its handle.
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		m := n.member(t, "api", "api")
		m.Started = true
		return f.AddMember(harbor, m)
	}); err != nil {
		t.Fatal(err)
	}
	api := n.agent(t, "api")
	news := api.teamBoundary()
	for _, want := range []string{`◆ you were started to manage the team "backend", under "harbor"`, "◆ brief from manager #2: Build the signup API."} {
		if !strings.Contains(news, want) {
			t.Errorf("the new manager's first delivery lacks %q:\n%s", want, news)
		}
	}
	f = n.file(t)
	backend, _ = f.Team(backend.ID)
	if backend.Manager != n.key(t, "api") {
		t.Fatalf("the new conversation did not become backend's manager: %+v", backend)
	}
	if home, _ := f.Home(n.key(t, "api")); home.Team != harbor {
		t.Fatalf("backend's manager reports to %+v, want harbor", home)
	}
	if home, _ := f.Home(n.key(t, "parser")); home.Team != backend.ID {
		t.Fatalf("the moved member reports to %+v, want backend", home)
	}
	api.mu.Lock()
	role := api.teamRoleText
	api.mu.Unlock()
	for _, want := range []string{`You are the manager of the team "backend".`, `Your team is a team under "harbor": you report to its manager, @boss.`, "@parser"} {
		if !strings.Contains(role, want) {
			t.Errorf("the sub-team manager's role lacks %q:\n%s", want, role)
		}
	}
	if !holds(api, teamStatusToolName) || !holds(api, teamPostToolName) || !holds(api, teamRaiseToolName) {
		t.Error("the sub-team manager lacks its verbs")
	}
	// The parent's manager is told what runs under it.
	boss.teamBoundary()
	boss.mu.Lock()
	role = boss.teamRoleText
	boss.mu.Unlock()
	if !strings.Contains(role, `Teams under yours: "backend" (run by @api). Direct their managers, never their members.`) {
		t.Errorf("harbor's manager is not told what runs under it:\n%s", role)
	}
}

func TestSubTeamManagerNamingItsOwnTeamGetsTheRule(t *testing.T) {
	n := newTeamTree(t, "boss", "api")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"))
	boss := n.agent(t, "boss")
	boss.teamBoundary()
	if said, failed := callTool(t, boss.teamStartTool, `{"handle":"api","brief":"Run the API.","kind":"team","name":"backend"}`); failed {
		t.Fatalf("team_start failed: %q", said)
	}
	f := n.file(t)
	var backend teams.Team
	for _, team := range f.Teams {
		if team.Name == "backend" {
			backend = team
			break
		}
	}
	if backend.ID == "" {
		t.Fatal("the child team was not made")
	}
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		m := n.member(t, "api", "api")
		m.Started = true
		if err := f.AddMember(harbor, m); err != nil {
			return err
		}
		return f.SetManager(backend.ID, m.Key)
	}); err != nil {
		t.Fatal(err)
	}
	api := n.agent(t, "api")
	api.teamBoundary()
	said, failed := callTool(t, api.teamPostTool, `{"team":"backend","to":"manager","text":"status"}`)
	if !failed || said != `You manage "backend"; team_post is for the team above you, "harbor".` {
		t.Fatalf("the own-team refusal was %q (failed=%v)", said, failed)
	}
}

// PAST THE DEPTH LIMIT, a sub-team is refused with the reason, and nothing is
// made.
func TestASubTeamPastTheDepthLimitIsRefusedWithTheReason(t *testing.T) {
	n := newTeamTree(t, "boss", "web")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	one := 1
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		return f.SetSettings(harbor, func(s *teams.Settings) { s.DepthLimit = &one })
	}); err != nil {
		t.Fatal(err)
	}
	boss := n.agent(t, "boss")
	boss.teamBoundary()
	said, failed := callTool(t, boss.teamStartTool, `{"handle":"api","brief":"b","kind":"team","name":"backend"}`)
	if !failed || !strings.Contains(said, `A team under "harbor" would be level 2, past its depth limit of 1 (its own setting)`) {
		t.Fatalf("a start past the limit said %q (failed %v)", said, failed)
	}
	if len(n.file(t).Teams) != 1 {
		t.Fatal("a refused start made a team")
	}
	if log, _ := teams.ReadTraffic(n.fixture.profile, harbor, "", 0); len(log) != 0 {
		t.Fatalf("a refused start wrote traffic: %+v", log)
	}
}

// ── 3. one level ────────────────────────────────────────────────────────────

// ORDERS GO ONE LEVEL DOWN: the sub-team's manager is the parent manager's to
// direct, and its members are not, and the refusal names whom to send to.
func TestOrdersGoOneLevelDownAndPointToTheSubTeamsManager(t *testing.T) {
	n := newTeamTree(t, "boss", "api", "parser", "loner")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "api", "api"))
	n.team(t, "backend", harbor, "api", n.member(t, "api", "lead"), n.member(t, "parser", "parser"))
	n.team(t, "dock", harbor, "", n.member(t, "loner", "loner"))
	boss, api, parser := n.agent(t, "boss"), n.agent(t, "api"), n.agent(t, "parser")
	for _, a := range []*Agent{boss, api, parser} {
		a.teamBoundary()
	}

	said, failed := callTool(t, boss.teamSendTool, `{"to":"@parser","text":"use JSON","kind":"directive"}`)
	if !failed || !strings.Contains(said, "orders go one level down") || !strings.Contains(said, "Send a message to @api") {
		t.Fatalf("a directive to a grandchild said %q", said)
	}
	said, failed = callTool(t, boss.teamStopTool, `{"handle":"parser"}`)
	if !failed || !strings.Contains(said, "Send a stop to @api") {
		t.Fatalf("a stop of a grandchild said %q", said)
	}
	said, failed = callTool(t, boss.teamSendTool, `{"to":"loner","text":"x","kind":"directive"}`)
	if !failed || !strings.Contains(said, "no manager of its own") {
		t.Fatalf("a directive into an unmanaged sub-team said %q", said)
	}
	// The sub-team's manager itself is a member, and takes the directive.
	if said, failed = callTool(t, boss.teamSendTool, `{"to":"api","text":"ship it","kind":"directive"}`); failed {
		t.Fatalf("a directive to the sub-team's manager was refused: %q", said)
	}
	if news := api.teamBoundary(); !strings.Contains(news, "◆ directive from manager #1: ship it") {
		t.Fatalf("the sub-team's manager did not get its manager's directive:\n%s", news)
	}
	// Everyone is the manager's own members only.
	callTool(t, boss.teamSendTool, `{"to":"everyone","text":"standup","kind":"note"}`)
	if news := parser.teamBoundary(); strings.Contains(news, "standup") {
		t.Fatalf("a grandchild was handed its grandparent's note to everyone:\n%s", news)
	}
	// A read reaches down the tree.
	if said, failed = callTool(t, boss.teamReadTool, `{"handle":"backend/@parser"}`); failed {
		t.Fatalf("a read of a member below was refused: %q", said)
	}
}

// ── 2. conflicts ────────────────────────────────────────────────────────────

// siblingSubTeams is harbor (boss) over front (lead; web) and back (chief;
// api), each sub-team's manager a member of harbor.
func siblingSubTeams(t *testing.T) (teamTree, string, string, string) {
	t.Helper()
	n := newTeamTree(t, "boss", "lead", "chief", "web", "api")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "lead", "lead"), n.member(t, "chief", "chief"))
	front := n.team(t, "front", harbor, "lead", n.member(t, "lead", "lead"), n.member(t, "web", "web"))
	back := n.team(t, "back", harbor, "chief", n.member(t, "chief", "chief"), n.member(t, "api", "api"))
	return n, harbor, front, back
}

// A CONFLICT BETWEEN MEMBERS OF SIBLING SUB-TEAMS lands at their common
// parent's manager, which is handed it whole and rules; both parties receive
// the ruling as a directive, and it wakes them.
func TestAConflictBetweenSiblingSubTeamsIsRuledByTheirCommonManager(t *testing.T) {
	n, harbor, front, back := siblingSubTeams(t)
	web, api, boss, lead := n.agent(t, "web"), n.agent(t, "api"), n.agent(t, "boss"), n.agent(t, "lead")
	for _, a := range []*Agent{web, api, boss, lead} {
		a.teamBoundary()
	}
	if !holds(web, teamRaiseToolName) {
		t.Fatal("a member was not offered team_raise")
	}
	said, failed := callTool(t, web.teamRaiseTool, `{"question":"which shape does the signup form send?","parties":["back/@api"],"context":"the form posts JSON",`+
		`"options":[{"label":"JSON","consequence":"@api changes the handler"},{"label":"form data","consequence":"@web rewrites the submit"}],"recommend":"JSON","reason":"matches the rest"}`)
	if failed || !strings.Contains(said, `with @api (back) for the manager of "harbor" (@boss) to decide`) {
		t.Fatalf("team_raise said %q", said)
	}
	p := onlyPacket(t, n.fixture.profile, harbor)
	if p.Kind != teams.PacketConflict || p.Origin != front || len(p.Parties) != 2 || p.Parties[1].Team != back || p.Recommendation == nil {
		t.Fatalf("the packet: %+v", p)
	}
	if mine, _, _ := teams.OpenPackets(n.fixture.profile, teams.Person); len(mine) != 0 {
		t.Fatal("a conflict with a common manager reached the person")
	}
	// Not the sub-teams' managers: each is below the other party.
	if news := lead.teamBoundary(); strings.Contains(news, p.ID) {
		t.Fatalf("a party's own manager was handed the conflict:\n%s", news)
	}
	news := boss.teamBoundary()
	for _, want := range []string{"◆ conflict " + p.ID + " from @web, waiting on you", "parties: @web, @api", "@web says: the form posts JSON", "[1] JSON: @api changes the handler"} {
		if !strings.Contains(news, want) {
			t.Errorf("the deciding manager's delivery lacks %q:\n%s", want, news)
		}
	}
	if told := api.teamBoundary(); !strings.Contains(told, "◆ @web raised a conflict naming you ("+p.ID+")") {
		t.Errorf("the other party was not told:\n%s", told)
	}
	// A party's manager may not decide it: it does not wait on lead's team.
	if said, failed := callTool(t, lead.teamDecideTool, `{"packet":"`+p.ID+`","answer":"2"}`); !failed {
		t.Fatalf("a party's own manager decided it: %q", said)
	}
	if said, failed := callTool(t, boss.teamDecideTool, `{"packet":"`+p.ID+`","answer":"1","reason":"the other endpoints take JSON"}`); failed {
		t.Fatalf("the common manager could not decide: %q", said)
	}
	for name, a := range map[string]*Agent{"web": web, "api": api} {
		got := a.teamBoundary()
		for _, want := range []string{"◆ ruling on the conflict " + p.ID, `by ◆ @boss (manager of "harbor"): JSON: @api changes the handler`, "the other endpoints take JSON"} {
			if !strings.Contains(got, want) {
				t.Errorf("@%s's ruling lacks %q:\n%s", name, want, got)
			}
		}
		if strings.Contains(got, "◆ answered") {
			t.Errorf("@%s was handed the ruling twice:\n%s", name, got)
		}
	}
	// Each ruling is in its party's team log, and wakes that party.
	for team, handle := range map[string]string{front: "web", back: "api"} {
		log, _ := teams.ReadTraffic(n.fixture.profile, team, "", 0)
		ruling := last(log)
		role := teamRole{id: team, handle: handle, managed: true, key: n.key(t, handle)}
		if !teams.IsRuling(ruling) || !teamWakes(role, ruling) {
			t.Errorf("%s's ruling does not wake @%s: %+v", team, handle, ruling)
		}
	}
}

// NO COMMON MANAGER IS THE PERSON: two top-level teams with no root.
func TestAConflictWithNoCommonManagerGoesToThePerson(t *testing.T) {
	n := newTeamTree(t, "boss", "yard", "web", "api")
	n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	n.team(t, "dock", "", "yard", n.member(t, "yard", "yard"), n.member(t, "api", "api"))
	web := n.agent(t, "web")
	web.teamBoundary()
	said, failed := callTool(t, web.teamRaiseTool, `{"question":"who owns the schema?","parties":["@api"],"options":[{"label":"web","consequence":"web owns it"},{"label":"api","consequence":"api owns it"}]}`)
	if failed || !strings.Contains(said, "for the person to decide") {
		t.Fatalf("team_raise said %q", said)
	}
	onlyPacket(t, n.fixture.profile, teams.Person)
}

// ── 4. the global manager ───────────────────────────────────────────────────

// THE GLOBAL MANAGER runs the top-level managers and only them: its digest and
// status list them, never their members; a directive reaches a top-level
// manager as its manager's; a member below is pointed at its manager; a
// top-level manager's own question goes to it before the person; and it
// starts top-level teams.
func TestTheGlobalManagerRunsTheTopLevelManagersOnly(t *testing.T) {
	n := newTeamTree(t, "gm", "boss", "yard", "web", "api", "hq")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	n.team(t, "dock", "", "yard", n.member(t, "yard", "yard"), n.member(t, "api", "api"))
	root := ""
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		root = f.MakeRoot(f.Teams[0].Made)
		off := false
		if err := f.SetSettings(root, func(s *teams.Settings) { s.Wake = &off }); err != nil {
			return err
		}
		if err := f.AddMember(root, n.member(t, "gm", "all")); err != nil {
			return err
		}
		return f.SetManager(root, n.key(t, "gm"))
	}); err != nil {
		t.Fatal(err)
	}
	gm, boss := n.agent(t, "gm"), n.agent(t, "boss")
	gm.teamBoundary()
	boss.teamBoundary()
	gm.mu.Lock()
	role := gm.teamRoleText
	gm.mu.Unlock()
	for _, want := range []string{"You are the global manager", "@boss", "@yard"} {
		if !strings.Contains(role, want) {
			t.Errorf("the global manager's role lacks %q:\n%s", want, role)
		}
	}
	if strings.Contains(role, "@web") || strings.Contains(role, "@api") {
		t.Errorf("the global manager was handed the teams' members:\n%s", role)
	}
	status, _ := callTool(t, gm.teamStatusTool, `{}`)
	if !strings.Contains(status, "@boss") || strings.Contains(status, "@web") {
		t.Errorf("the global manager's status:\n%s", status)
	}
	if said, failed := callTool(t, gm.teamSendTool, `{"to":"web","text":"x","kind":"directive"}`); !failed || !strings.Contains(said, "Send a message to @boss") {
		t.Fatalf("a directive past the top-level managers said %q", said)
	}
	if said, failed := callTool(t, gm.teamSendTool, `{"to":"boss","text":"ship harbor first","kind":"directive"}`); failed {
		t.Fatalf("a directive to a top-level manager was refused: %q", said)
	}
	news := boss.teamBoundary()
	if !strings.Contains(news, "◆ directive from manager #1: ship harbor first") {
		t.Fatalf("the top-level manager did not get the global manager's directive:\n%s", news)
	}
	boss.mu.Lock()
	role = boss.teamRoleText
	boss.mu.Unlock()
	if !strings.Contains(role, `Your team is a top-level team, under the global manager of "All teams": you report to its manager, @all.`) {
		t.Errorf("the top-level manager is not told whom it reports to:\n%s", role)
	}
	// Its own question goes to the global manager, not the person.
	said, _ := callTool(t, boss.executeAsk, clarifying)
	if strings.Contains(said, "the person's inbox") {
		t.Fatalf("a top-level manager's question skipped the global manager: %q", said)
	}
	if p := onlyPacket(t, n.fixture.profile, root); p.RaisedBy == "" {
		t.Fatalf("the packet: %+v", p)
	}
	// It starts a top-level team.
	said, failed := callTool(t, gm.teamStartTool, `{"handle":"ops","brief":"Run the ops.","kind":"team","name":"ops"}`)
	if failed {
		t.Fatalf("the global manager could not start a top-level team: %q", said)
	}
	f := n.file(t)
	for _, team := range f.Teams {
		if team.Name == "ops" && (team.Parent != root || f.Depth(team.ID) != 1) {
			t.Fatalf("the new team is not top-level: %+v", team)
		}
	}
	_ = harbor
}

// ── 5. a sub-team with no manager ───────────────────────────────────────────

// A MEMBER OF A SUB-TEAM WITH NO MANAGER answers to the nearest manager above
// it: it has the member's verb, its post to the manager reaches that manager,
// and its question goes there as a packet from its own team.
func TestAnUnmanagedSubTeamsMemberAnswersToTheManagerAbove(t *testing.T) {
	n := newTeamTree(t, "boss", "web", "loner")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	dock := n.team(t, "dock", harbor, "", n.member(t, "loner", "loner"))
	loner, boss := n.agent(t, "loner"), n.agent(t, "boss")
	loner.teamBoundary()
	boss.teamBoundary()
	if !holds(loner, teamPostToolName) {
		t.Fatal("a member of an unmanaged sub-team has no team_post")
	}
	loner.mu.Lock()
	role := loner.teamRoleText
	loner.mu.Unlock()
	if !strings.Contains(role, `You are @loner in the team "dock", which has no manager of its own, so you answer to the manager of "harbor"`) {
		t.Errorf("its role:\n%s", role)
	}
	said, failed := callTool(t, loner.teamPostTool, `{"to":"manager","text":"the dock is done"}`)
	if failed || !strings.Contains(said, `Posted to the manager of "harbor"`) {
		t.Fatalf("team_post to the manager said %q", said)
	}
	log, _ := teams.ReadTraffic(n.fixture.profile, harbor, "", 0)
	if post := last(log); post.From != "loner" || post.To != teams.ToManager || !strings.Contains(post.Text, `(from "dock"`) {
		t.Fatalf("harbor's log: %+v", post)
	}
	// Its answer names the line it became in harbor's log and harbor by id;
	// the same words posted again are another line, read back as that one.
	read, ok := ReadTeamPost(said)
	if !ok || !read.Up || read.TeamID != harbor || read.Team != "harbor" || read.From != "dock" || read.Post != last(log).ID {
		t.Fatalf("the answer %q read back as %+v %v (log line %s)", said, read, ok, last(log).ID)
	}
	again, _ := callTool(t, loner.teamPostTool, `{"to":"manager","text":"the dock is done"}`)
	log, _ = teams.ReadTraffic(n.fixture.profile, harbor, "", 0)
	if second, ok := ReadTeamPost(again); !ok || second.Post == read.Post || second.Post != last(log).ID || second.TeamID != harbor {
		t.Fatalf("the same words again read back as %+v %v (log line %s, first %s)", second, ok, last(log).ID, read.Post)
	}
	if news := boss.teamBoundary(); !strings.Contains(news, "from @loner") || !strings.Contains(news, "the dock is done") {
		t.Fatalf("harbor's manager was not handed the post:\n%s", news)
	}
	said, _ = callTool(t, loner.executeAsk, clarifying)
	if !strings.Contains(said, "went to your manager (@boss") {
		t.Fatalf("its question said %q", said)
	}
	if p := onlyPacket(t, n.fixture.profile, harbor); p.Origin != dock || p.RaisedBy != "loner" {
		t.Fatalf("the question's packet: %+v", p)
	}
}

// THE GLOBAL MANAGER IS NOT WOKEN BY A TOP-LEVEL MANAGER'S TURN ENDING. The
// person talks to that manager directly and each answer ends a turn; what the
// global manager should act on is posted to it, and that still wakes it. A
// directive of the global manager's is answered the same way, and both sides
// are told so, because the ending that closes the loop in any other team says
// nothing here.
func TestTheGlobalManagerIsNotWokenByATopLevelManagersTurnEnding(t *testing.T) {
	fastTeamWake(t)
	n := newTeamTree(t, "gm", "boss", "web")
	n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	root := ""
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		root = f.MakeRoot(f.Teams[0].Made)
		if err := f.AddMember(root, n.member(t, "gm", "all")); err != nil {
			return err
		}
		return f.SetManager(root, n.key(t, "gm"))
	}); err != nil {
		t.Fatal(err)
	}
	gmCalls := oneAnswer(4)
	gm := teamAgent(t, n.fixture, n.paths["gm"], gmCalls, nil)
	bossCalls := oneAnswer(4)
	boss := teamAgent(t, n.fixture, n.paths["boss"], bossCalls, nil)

	submitAndWait(t, boss, "plan the release")
	if role := strings.Join(roleNotes(bossCalls.request(0)), "\n"); !strings.Contains(role, "Your turn ending tells it nothing: answer each of its directives, and the brief it started you on if it started you, with team_post to the manager.") {
		t.Fatalf("the top-level manager is not told to answer the global manager's directive with a post:\n%s", role)
	}
	boss.settleTeamEvents()
	// Past the settle a wake waits out, with room: a wake the ending had armed
	// would have fired by now.
	time.Sleep(teamWakeDelay() + 15*teamWatchInterval())
	all := teamFixture{profile: n.fixture.profile, teamID: root}
	if ended := trafficEvents(t, all, "finished"); len(ended) != 1 {
		t.Fatalf("the top-level manager's ending was logged %d times in the team that holds every team, want once", len(ended))
	}
	if got := gmCalls.requests(); got != 0 {
		t.Fatalf("the global manager was woken %d times by a top-level manager's turn ending", got)
	}
	if woke := trafficEvents(t, all, teamWokePrefix); len(woke) != 0 {
		t.Fatalf("the traffic says the global manager was woken by the ending: %+v", woke)
	}

	if said, failed := callTool(t, boss.teamPostTool, `{"to":"manager","text":"harbor shipped"}`); failed {
		t.Fatalf("the report to the global manager was refused: %q", said)
	}
	waitRequests(t, gmCalls, 1)
	asked := userTextIn(gmCalls.request(0))
	for _, want := range []string{"from @boss: harbor shipped", "A manager answers a directive of yours, and the brief you started it on, with team_post", "and their failing or asking, come back to you", "A member's turn finishing or being stopped does not start yours."} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the global manager's turn lacks %q:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, "their finishing") {
		t.Fatalf("the global manager is still told a member's finishing starts its turn:\n%s", asked)
	}
	if strings.Contains(asked, "finished its turn") || strings.Contains(asked, "What your members in") {
		t.Fatalf("the ending rode into the global manager's turn as something that woke it:\n%s", asked)
	}
	// Its own send's answer does not promise that an ending wakes it, and says
	// what answers the directive.
	if said := sendDirective(t, gm, "boss"); strings.Contains(said, "endings wake you") || !strings.Contains(said, "A top-level team's manager answers a directive with team_post: its turn finishing or being stopped does not wake you") {
		t.Fatalf("the global manager's send says endings wake it: %s", said)
	}
}

// A GLOBAL MANAGER NOBODY HAS OPEN IS NOT OPENED BY AN ENDING IN ITS TEAM
// EITHER, whoever's it is: a top-level manager's, a member's that manages
// nothing, or a manager's that was replaced and kept its seat. It sees and
// directs only the managers of the top-level teams, so none of those endings
// is one it can act on. A failure still opens it. Only a top-level manager is
// told it sits there because it manages a team.
func TestAClosedGlobalManagerIsNotOpenedByATurnEndingInItsTeam(t *testing.T) {
	fastTeamWake(t)
	n := newTeamTree(t, "gm", "boss", "web", "guest")
	harbor := n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"), n.member(t, "web", "web"))
	root := ""
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		root = f.MakeRoot(f.Teams[0].Made)
		for _, m := range []teams.Member{n.member(t, "gm", "all"), n.member(t, "guest", "guest")} {
			if err := f.AddMember(root, m); err != nil {
				return err
			}
		}
		return f.SetManager(root, n.key(t, "gm"))
	}); err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	recorder := &recordedResume{}
	withTeamResume(t, recorder.open)
	opened := func() int {
		count := 0
		for _, seen := range recorder.seen() {
			if strings.HasPrefix(seen, n.paths["gm"]+"|") {
				count++
			}
		}
		return count
	}
	// due is the desktop app's question of a conversation it has not opened.
	due := func() bool {
		return TeamOpenPending(n.fixture.profile, n.paths["gm"], n.file(t), teams.DefaultsAt(n.fixture.profile), since)
	}
	all := teamFixture{profile: n.fixture.profile, teamID: root}
	// quiet runs one turn of agent's and says what, if anything, opened the
	// closed global manager on its ending.
	endings := 0
	quiet := func(who string, agent *Agent) {
		t.Helper()
		submitAndWait(t, agent, "tidy the notes")
		agent.settleTeamEvents()
		endings++
		if ended := trafficEvents(t, all, "finished"); len(ended) != endings {
			t.Fatalf("%s ended its turn and the team that holds every team has %d finished events, want %d", who, len(ended), endings)
		}
		time.Sleep(teamWakeDelay() + 15*teamWatchInterval())
		if got := opened(); got != 0 {
			t.Fatalf("a closed global manager was opened %d times by the turn ending of %s", got, who)
		}
		if due() {
			t.Fatalf("the turn ending of %s leaves the closed global manager due an opening", who)
		}
	}
	bossCalls := oneAnswer(2)
	bossCalls.steps = append(bossCalls.steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("insufficient_quota: out of budget")
	})
	boss := teamAgent(t, n.fixture, n.paths["boss"], bossCalls, nil)
	quiet("a top-level manager", boss)

	guestCalls := oneAnswer(1)
	guest := teamAgent(t, n.fixture, n.paths["guest"], guestCalls, nil)
	quiet("a member that manages nothing", guest)
	ordinary := func(who, role string) {
		t.Helper()
		if !strings.Contains(role, `in the team "All teams", which has a manager.`) {
			t.Fatalf("%s is not told it is a member of the team that holds every team:\n%s", who, role)
		}
		for _, not := range []string{"you manage a top-level team", "Your turn ending tells it nothing"} {
			if strings.Contains(role, not) {
				t.Fatalf("%s is told %q, which is said to a top-level team's manager only:\n%s", who, not, role)
			}
		}
	}
	ordinary("a member that manages nothing", strings.Join(roleNotes(guestCalls.request(0)), "\n"))

	// The team's manager is replaced. The old one keeps its seat in the team
	// that holds every team, where the global manager no longer sees it.
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		return f.SetManager(harbor, n.key(t, "web"))
	}); err != nil {
		t.Fatal(err)
	}
	f := n.file(t)
	if r, _ := f.Team(root); !r.Holds(n.key(t, "boss")) {
		t.Fatal("the replaced manager lost its seat in the team that holds every team")
	}
	if tops := f.TopManagers(); len(tops) != 1 || tops[0].Key != n.key(t, "web") {
		t.Fatalf("the top-level managers after the replacement: %+v", tops)
	}
	quiet("a replaced top-level manager", boss)
	former := roleNotes(bossCalls.request(1))
	if len(former) == 0 {
		t.Fatal("the replaced manager's next turn carries no new role")
	}
	ordinary("a replaced top-level manager", former[len(former)-1])

	// A failure there still opens it, a member's it does not direct included.
	submitAndWait(t, boss, "ship it")
	boss.settleTeamEvents()
	if failed := trafficEvents(t, all, "failed: "); len(failed) != 1 || failed[0].State != teams.StateFailed {
		t.Fatalf("the failed turn is not one failed event in the team that holds every team: %+v", failed)
	}
	deadline := time.Now().Add(5 * time.Second)
	for opened() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if opened() == 0 {
		t.Fatal("a turn failed in the team that holds every team and its closed manager was not opened")
	}
	if !due() {
		t.Fatal("a failed turn does not leave the closed global manager due an opening")
	}
}

// A TEAM THE GLOBAL MANAGER STARTS REPORTS ITS FIRST TURN WITH A POST. The new
// manager's turn on the brief ends like any other there and wakes nobody, so
// team_start's answer and both roles name the brief beside the directives.
func TestATeamTheGlobalManagerStartsAnswersItsBriefWithAPost(t *testing.T) {
	fastTeamWake(t)
	n := newTeamTree(t, "gm", "boss", "ops")
	n.team(t, "harbor", "", "boss", n.member(t, "boss", "boss"))
	root := ""
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		root = f.MakeRoot(f.Teams[0].Made)
		if err := f.AddMember(root, n.member(t, "gm", "all")); err != nil {
			return err
		}
		return f.SetManager(root, n.key(t, "gm"))
	}); err != nil {
		t.Fatal(err)
	}
	gmCalls := oneAnswer(2)
	gm := teamAgent(t, n.fixture, n.paths["gm"], gmCalls, nil)
	gm.teamBoundary()
	said, failed := callTool(t, gm.teamStartTool, `{"handle":"ops","brief":"Run the ops.","kind":"team","name":"ops"}`)
	if failed {
		t.Fatalf("the global manager could not start a top-level team: %q", said)
	}
	if want := `makes itself the manager of "ops", and reports to you with team_post: its turn finishing on your brief does not start yours, and team_status says where it stands.`; !strings.Contains(said, want) {
		t.Fatalf("team_start's answer for a top-level team lacks %q:\n%s", want, said)
	}
	gm.mu.Lock()
	role := gm.teamRoleText
	gm.mu.Unlock()
	if want := "A manager answers a directive of yours, and the brief you started it on, with team_post, since its turn finishing does not start yours"; !strings.Contains(role, want) {
		t.Fatalf("the global manager's role lacks %q:\n%s", want, role)
	}

	// The interface carries out the start, and the new manager runs its first
	// turn on the brief without posting.
	if err := teams.Update(n.fixture.profile, func(f *teams.File) error {
		m := n.member(t, "ops", "ops")
		m.Started = true
		return f.AddMember(root, m)
	}); err != nil {
		t.Fatal(err)
	}
	opsCalls := oneAnswer(1)
	ops := teamAgent(t, n.fixture, n.paths["ops"], opsCalls, nil)
	submitAndWait(t, ops, "go")
	first := opsCalls.request(0)
	if asked := userTextIn(first); !strings.Contains(asked, "◆ brief from manager") || !strings.Contains(asked, "Run the ops.") {
		t.Fatalf("the new manager's first turn lacks its brief:\n%s", asked)
	}
	if role := strings.Join(roleNotes(first), "\n"); !strings.Contains(role, "because you manage a top-level team") ||
		!strings.Contains(role, "answer each of its directives, and the brief it started you on if it started you, with team_post to the manager.") {
		t.Fatalf("the new manager is not told to answer its brief with a post:\n%s", role)
	}
	ops.settleTeamEvents()
	time.Sleep(teamWakeDelay() + 15*teamWatchInterval())
	all := teamFixture{profile: n.fixture.profile, teamID: root}
	if ended := trafficEvents(t, all, "finished"); len(ended) != 1 {
		t.Fatalf("the new manager's first turn was logged %d times in the team that holds every team, want once", len(ended))
	}
	if got := gmCalls.requests(); got != 0 {
		t.Fatalf("the global manager was woken %d times by the new manager's first turn ending", got)
	}

	if said, failed := callTool(t, ops.teamPostTool, `{"to":"manager","text":"ops is set up"}`); failed {
		t.Fatalf("the new manager's report was refused: %q", said)
	}
	waitRequests(t, gmCalls, 1)
	if asked := userTextIn(gmCalls.request(0)); !strings.Contains(asked, "from @ops: ops is set up") {
		t.Fatalf("the global manager's turn lacks the new manager's report:\n%s", asked)
	}
}

func TestTeamEventWakesTheGlobalManager(t *testing.T) {
	for _, c := range []struct {
		state, text string
		root, want  bool
	}{
		{teams.StateFinished, "finished", false, true},
		{teams.StateFinished, "finished", true, false},
		{teams.StateIdle, teams.StoppedByPerson, false, true},
		{teams.StateIdle, teams.StoppedByPerson, true, false},
		{teams.StateFailed, "failed: no key", true, true},
		{teams.StateAsking, "asks: which branch?", true, true},
		{teams.StateRunning, "no longer waiting", true, false},
	} {
		if got := teamEventWakesManagerOf(c.root, c.state, c.text); got != c.want {
			t.Errorf("in the team that holds every team %v, %s %q wakes the manager: %v, want %v", c.root, c.state, c.text, got, c.want)
		}
	}
}
