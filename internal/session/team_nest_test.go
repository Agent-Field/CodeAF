package session

// NESTED TEAMS, AS TESTS: a manager starts a sub-team whose manager makes
// itself one on its brief and reports up; orders go one level down and a
// handle below is pointed at its manager; a conflict goes to the lowest
// common manager, is ruled there, and the ruling reaches every party; the
// global manager runs the top-level managers and only them; and a sub-team
// with no manager of its own answers to the one above. Every fixture is a real
// teams.json and real Traffic, through internal/teams.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
