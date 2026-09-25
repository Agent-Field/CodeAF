package session

// DELEGATION, AS TESTS: questions go up as packets and answers come back
// down, links read and fyi but do not direct, a cap holds new work and asks
// the person once, and a wrap-up ends in a closing report that closes the
// team only when the person accepts it. Every fixture is a real teams.json,
// a real Traffic log and real packet files, through internal/teams.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// clarifying is an `ask` a member writes when it needs an answer to go on.
const clarifying = `{"head":"JSON or form data?","kind":"clarification","reason":"the handler and the form disagree","stakes":"reversible",` +
	`"options":[{"key":"1","label":"JSON","consequence":"the handler changes"},{"key":"2","label":"form data","consequence":"the form changes"}],` +
	`"pick":{"key":"1","reason":"the other endpoints take JSON"}}`

func callTool(t *testing.T, run func(context.Context, json.RawMessage) (string, bool, error), args string) (string, bool) {
	t.Helper()
	text, failed, err := run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("the tool returned an error: %v", err)
	}
	return text, failed
}

func onlyPacket(t *testing.T, profile, scope string) teams.Packet {
	t.Helper()
	waiting, _, err := teams.OpenPackets(profile, scope)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("packets waiting on %q: %+v %v", scope, waiting, err)
	}
	return waiting[0]
}

// A MEMBER'S CLARIFYING QUESTION GOES TO ITS MANAGER AS A PACKET, the manager
// is handed it whole and answers it, and the member is handed the answer
// marked as answered. Nothing is put in front of the person.
func TestTeamQuestionGoesUpAsAPacketAndTheAnswerComesBack(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	web.teamBoundary()

	said, failed := callTool(t, web.executeAsk, clarifying)
	if failed || !strings.Contains(said, "went to your manager (@boss") || !strings.Contains(said, "not to the person") {
		t.Fatalf("the member was told %q", said)
	}
	p := onlyPacket(t, fixture.profile, fixture.teamID)
	if p.Kind != teams.PacketQuestion || p.RaisedBy != "web" || p.Question != "JSON or form data?" || len(p.Options) != 2 ||
		p.Recommendation == nil || p.Recommendation.Option != "1" || len(p.Parties) != 1 || p.Parties[0].Context != "the handler and the form disagree" {
		t.Fatalf("the packet is not self-contained: %+v", p)
	}
	if mine, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(mine) != 0 {
		t.Fatal("a member's question reached the person")
	}

	news := manager.teamBoundary()
	for _, want := range []string{"◆ question " + p.ID + " from @web, waiting on you: JSON or form data?", "[1] JSON: the handler changes", "recommended: 1", "team_decide"} {
		if !strings.Contains(news, want) {
			t.Errorf("the manager's delivery lacks %q:\n%s", want, news)
		}
	}
	answer, failed := callTool(t, manager.teamDecideTool, `{"packet":"`+p.ID+`","answer":"json","reason":"matches the rest"}`)
	if failed || !strings.Contains(answer, "Decided "+p.ID) {
		t.Fatalf("team_decide said %q", answer)
	}
	got := web.teamBoundary()
	if !strings.Contains(got, "◆ answered: JSON (by ◆ @boss, because matches the rest). Your question was: JSON or form data?") {
		t.Fatalf("the member was not handed the answer:\n%s", got)
	}
	log, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0)
	if last := log[len(log)-1]; last.Text != "answered @web: JSON" {
		t.Fatalf("the rail's line: %+v", last)
	}
	// The member's answer and the manager's packet both wake, on a team that
	// wakes.
	role := teamRole{id: fixture.teamID, handle: "web", managed: true, key: convKeyOf(t, fixture.web)}
	if !teamPacketWakes(fixture.profile, role, last(log)) {
		t.Error("the answer does not wake the member it answers")
	}
}

func last(log []teams.Entry) teams.Entry { return log[len(log)-1] }

// PERMISSION PROMPTS AND QUESTIONS WITH questions_up OFF ARE THE PERSON'S.
func TestTeamQuestionsUpOffAndPermissionsStayThePersons(t *testing.T) {
	if askGoesUp(AskPermission, false) || askGoesUp(AskPermission, true) || askGoesUp(AskLanding, true) {
		t.Fatal("a permission or a landing goes up")
	}
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	if _, up := web.askUp(Question{Ask: AskPermission, Head: "run bash?"}); up {
		t.Fatal("a permission kind went up")
	}
	off := false
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetSettings(fixture.teamID, func(s *teams.Settings) { s.QuestionsUp = &off })
	}); err != nil {
		t.Fatal(err)
	}
	if _, up := web.askUp(Question{Ask: AskClarification, Head: "tabs?"}); up {
		t.Fatal("a question went up with questions_up off")
	}
}

// A MANAGER'S OWN QUESTION IS A PACKET TO THE PERSON when it has no manager
// above it, and the person's answer is handed to it marked as answered.
func TestTeamAManagersOwnQuestionGoesToThePersonsInbox(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	said, _ := callTool(t, manager.executeAsk, clarifying)
	if !strings.Contains(said, "the person's inbox") {
		t.Fatalf("the manager was told %q", said)
	}
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Origin != fixture.teamID || p.RaisedBy != teams.FromManager {
		t.Fatalf("the manager's packet: %+v", p)
	}
	if _, err := teams.Decide(fixture.profile, p.ID, teams.Person, "2", ""); err != nil {
		t.Fatal(err)
	}
	if news := manager.teamBoundary(); !strings.Contains(news, "◆ answered: form data (by the person)") {
		t.Fatalf("the manager was not handed the person's answer:\n%s", news)
	}
}

// linkedFixture is harbor (boss; web, parser) and dock (dockboss; web): web
// joined harbor first, so harbor is its home and dock's manager is a link.
func linkedFixture(t *testing.T) (teamFixture, string, string) {
	t.Helper()
	fixture := newTeamFixture(t, true)
	dir := filepath.Join(filepath.Dir(filepath.Dir(fixture.manager)), "dockboss")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	dockboss := filepath.Join(dir, placeTranscript)
	if err := os.WriteFile(dockboss, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dock := teams.NewID()
	err := teams.Update(fixture.profile, func(f *teams.File) error {
		f.Teams = append(f.Teams, teams.Team{ID: dock, Name: "dock"})
		for _, m := range []teams.Member{
			{Key: convKeyOf(t, dockboss), File: dockboss, Word: "dock manager", Handle: "dockboss"},
			{Key: convKeyOf(t, fixture.web), File: fixture.web, Word: "web frontend", Handle: "web"},
		} {
			if err := f.AddMember(dock, m); err != nil {
				return err
			}
		}
		return f.SetManager(dock, convKeyOf(t, dockboss))
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, dock, dockboss
}

// A LINK READS AND SENDS NOTES, AND IS REFUSED A DIRECTIVE OR A STOP in a
// sentence that says whose the member is; its status marks the member shared.
func TestTeamALinkMayNoteButNotDirectOrStop(t *testing.T) {
	fixture, dock, dockboss := linkedFixture(t)
	link := teamAgent(t, fixture, dockboss, nil, nil)
	link.teamBoundary()
	said, failed := callTool(t, link.teamSendTool, `{"to":"web","text":"do the header","kind":"directive"}`)
	if !failed || !strings.Contains(said, `@web reports to the manager of "harbor", not to you`) {
		t.Fatalf("a link's directive: %q", said)
	}
	said, failed = callTool(t, link.teamStopTool, `{"handle":"web"}`)
	if !failed || !strings.Contains(said, "not a stop") {
		t.Fatalf("a link's stop: %q", said)
	}
	said, failed = callTool(t, link.teamSendTool, `{"to":"everyone","text":"do the header","kind":"directive"}`)
	if !failed || !strings.Contains(said, "reports to another team's manager") {
		t.Fatalf("a link's directive to everyone: %q", said)
	}
	if said, failed = callTool(t, link.teamSendTool, `{"to":"web","text":"fyi the api moved"}`); failed {
		t.Fatalf("a link's note was refused: %q", said)
	}
	status, _ := callTool(t, link.teamStatusTool, `{}`)
	if !strings.Contains(status, "reports to harbor") {
		t.Fatalf("status does not mark the shared member:\n%s", status)
	}
	log, _ := teams.ReadTraffic(fixture.profile, dock, "", 0)
	for _, entry := range log {
		if entry.Kind == teams.KindDirective || entry.Kind == teams.KindStop {
			t.Fatalf("a refused verb wrote %+v", entry)
		}
	}
	// And the member reads a link's line as an fyi, and is not woken by one.
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	roles := web.teamRoles()
	var shared teamRole
	for _, role := range roles {
		if role.id == dock {
			shared = role
		}
	}
	if !shared.shared || shared.reportsTo != "harbor" {
		t.Fatalf("web's role in dock: %+v", shared)
	}
	directive := teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "do it"}
	if teamWakes(shared, directive) || !strings.Contains(teamLine(shared, directive), "fyi from the manager of \"dock\"") {
		t.Fatal("a link's directive reads or wakes as the home manager's")
	}
	// Its home manager still directs it.
	boss := teamAgent(t, fixture, fixture.manager, nil, nil)
	boss.teamBoundary()
	if said, failed := callTool(t, boss.teamSendTool, `{"to":"web","text":"do the header","kind":"directive"}`); failed {
		t.Fatalf("the home manager's directive was refused: %q", said)
	}
}

// capSpend stands in for the ledger: a fixed spend under a stamp the test
// moves, counting every real read.
type capSpend struct {
	usd   float64
	stamp string
	reads int
}

func stubCapSpend(t *testing.T, spend *capSpend) {
	t.Helper()
	oldOf, oldStamp, oldDay := teamSpendOf, teamSpendStamp, teamToday
	teamSpendOf = func(profile, team, day string) (teams.Spend, error) {
		spend.reads++
		return teams.Spend{Team: team, Day: day, USD: spend.usd}, nil
	}
	teamSpendStamp = func(profile, team, day string) string { return spend.stamp + "|" + team }
	teamToday = func() string { return "2026-09-24" }
	t.Cleanup(func() { teamSpendOf, teamSpendStamp, teamToday = oldOf, oldStamp, oldDay })
}

// A POOL AT ITS CAP HOLDS NEW WORK AND ASKS THE PERSON ONCE. The spend is read
// once per stamp, never per check; team_start is refused; a raise lifts the
// ceiling for the day; meeting the raised ceiling asks again, once; a stop
// holds; and a manager cannot decide a cap.
func TestTeamACapHoldsNewWorkAndAsksThePersonOnce(t *testing.T) {
	fixture := newTeamFixture(t, true)
	five := 5.0
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetSettings(fixture.teamID, func(s *teams.Settings) { s.CapUSDDay = &five })
	}); err != nil {
		t.Fatal(err)
	}
	spend := &capSpend{usd: 6, stamp: "s1"}
	stubCapSpend(t, spend)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	roles := manager.teamRoles()

	held := manager.teamCapHold(fixture.profile, roles)
	if !strings.Contains(held, "harbor reached its $5 cap today") {
		t.Fatalf("held for %q", held)
	}
	for i := 0; i < 5; i++ {
		manager.teamCapHold(fixture.profile, roles)
	}
	if spend.reads != 1 {
		t.Fatalf("the spend was read %d times under one stamp", spend.reads)
	}
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Kind != teams.PacketCap || p.Cap == nil || p.Cap.RaiseTo != 10 || p.Options[0].Label != "Raise to $10" ||
		p.Options[1].ID != teams.OptionStopToday || p.Recommendation == nil {
		t.Fatalf("the cap packet: %+v", p)
	}
	said, failed := callTool(t, manager.teamStartTool, `{"handle":"docs","brief":"write the README"}`)
	if !failed || !strings.Contains(said, "No new member starts") {
		t.Fatalf("team_start at the cap: %q", said)
	}
	if said, failed := callTool(t, manager.teamDecideTool, `{"packet":"`+p.ID+`","answer":"raise"}`); !failed || !strings.Contains(said, "waits on the person") {
		t.Fatalf("a manager decided a cap: %q", said)
	}
	if _, err := teams.Decide(fixture.profile, p.ID, teams.Person, teams.OptionRaiseCap, ""); err != nil {
		t.Fatal(err)
	}
	if held := manager.teamCapHold(fixture.profile, roles); held != "" {
		t.Fatalf("a raised cap still holds: %q", held)
	}
	spend.usd, spend.stamp = 11, "s2"
	if held := manager.teamCapHold(fixture.profile, roles); !strings.Contains(held, "$10 cap") {
		t.Fatalf("the raised ceiling did not hold: %q", held)
	}
	second := onlyPacket(t, fixture.profile, teams.Person)
	if second.ID == p.ID || second.Cap.CapUSD != 10 || second.Cap.RaiseTo != 20 {
		t.Fatalf("the second cap packet: %+v", second)
	}
	if _, err := teams.Decide(fixture.profile, second.ID, teams.Person, teams.OptionStopToday, ""); err != nil {
		t.Fatal(err)
	}
	if held := manager.teamCapHold(fixture.profile, roles); !strings.Contains(held, "chose to stop it for today") {
		t.Fatalf("a stop did not hold: %q", held)
	}
	if waiting, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(waiting) != 0 {
		t.Fatalf("a stop raised another packet: %+v", waiting)
	}
}

// NO CAP, NO READ: a team with no cap never reads the spend at all.
func TestTeamNoCapReadsNoSpend(t *testing.T) {
	fixture := newTeamFixture(t, true)
	spend := &capSpend{usd: 100, stamp: "s1"}
	stubCapSpend(t, spend)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	if held := web.teamCapHold(fixture.profile, web.teamRoles()); held != "" || spend.reads != 0 {
		t.Fatalf("no cap: held %q after %d reads", held, spend.reads)
	}
}

// WRAP UP FIRST ENDS IN A CLOSING REPORT, and the team closes when the person
// accepts it.
func TestTeamWrapUpEndsInAClosingReportThatClosesOnAccept(t *testing.T) {
	fixture := newTeamFixture(t, true)
	stubCapSpend(t, &capSpend{usd: 1.25, stamp: "s1"})
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	appendTraffic(t, fixture, teams.WrapUpRequest(""))
	news := manager.teamBoundary()
	if !strings.Contains(news, `Wrap up "harbor" now`) || !strings.Contains(news, "team_close_report") {
		t.Fatalf("the manager was not told to wrap up:\n%s", news)
	}
	manager.team.mu.Lock()
	going := len(manager.team.wraps)
	manager.team.mu.Unlock()
	if going != 1 {
		t.Fatal("the wrap-up's clock did not start")
	}
	f, _ := teams.Load(fixture.profile)
	if team, _ := f.Team(fixture.teamID); team.Wrap == nil || team.Wrap.Bound != wrapUpFor || team.Wrap.Started.IsZero() {
		t.Fatalf("the wrap-up was not written down: %+v", team.Wrap)
	}
	said, failed := callTool(t, manager.teamCloseReportTool, `{"done":"the form","left":"the tests","files":["web/form.go"]}`)
	if failed || !strings.Contains(said, "closing report went to the person") {
		t.Fatalf("team_close_report said %q", said)
	}
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Kind != teams.PacketClosing || p.Report == nil || p.Report.Done != "the form" || p.Report.SpendUSD != 1.25 || p.Report.Incomplete {
		t.Fatalf("the closing report: %+v", p)
	}
	if said, failed := callTool(t, manager.teamCloseReportTool, `{"done":"again"}`); !failed || !strings.Contains(said, "already waiting") {
		t.Fatalf("a second report: %q", said)
	}
	if _, err := teams.Decide(fixture.profile, p.ID, teams.Person, teams.OptionClose, ""); err != nil {
		t.Fatal(err)
	}
	if news := manager.teamBoundary(); !strings.Contains(news, "accepted the closing report") {
		t.Fatalf("the manager was not told:\n%s", news)
	}
	f, _ = teams.Load(fixture.profile)
	if team, _ := f.Team(fixture.teamID); !team.Closed() || team.Report != p.ID {
		t.Fatalf("the team did not close on its report: %+v", team)
	}
}

// A WRAP-UP PAST ITS BOUND IS REPORTED INCOMPLETE by codeaf.
func TestTeamAWrapUpPastItsBoundIsReportedIncomplete(t *testing.T) {
	old := wrapUpFor
	wrapUpFor = 10 * time.Millisecond
	t.Cleanup(func() { wrapUpFor = old })
	fixture := newTeamFixture(t, true)
	stubCapSpend(t, &capSpend{usd: 0.5, stamp: "s1"})
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	appendTraffic(t, fixture, teams.WrapUpRequest("Wrap up first"))
	manager.teamBoundary()
	manager.teamWrapUpDue(fixture.profile, time.Now())
	time.Sleep(20 * time.Millisecond)
	manager.teamWrapUpDue(fixture.profile, time.Now())
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Kind != teams.PacketClosing || p.Report == nil || !p.Report.Incomplete || p.Options[0].ID != teams.OptionCloseNow ||
		!strings.Contains(p.Question, "wrap-up incomplete") {
		t.Fatalf("the incomplete report: %+v", p)
	}
	manager.teamWrapUpDue(fixture.profile, time.Now())
	if waiting, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(waiting) != 1 {
		t.Fatal("the incomplete report was raised twice")
	}
}

// A RESTART KEEPS THE TIME THAT IS LEFT. The clock on the team is what the
// next process arms, and it does not start the bound again.
func TestTeamWrapUpResumesWithTheTimeLeft(t *testing.T) {
	fixture := newTeamFixture(t, true)
	stubCapSpend(t, &capSpend{usd: 0.5, stamp: "s1"})
	started := time.Now().Add(-time.Minute).UTC()
	bound := 15 * time.Minute
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetWrap(fixture.teamID, started, bound)
	}); err != nil {
		t.Fatal(err)
	}
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	if waiting, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(waiting) != 0 {
		t.Fatal("a wrap-up with time left raised a report on start")
	}
	manager.team.mu.Lock()
	w := manager.team.wraps[fixture.teamID]
	manager.team.mu.Unlock()
	if w == nil || !w.started.Equal(started) || w.bound != bound {
		t.Fatalf("the clock did not resume: %+v", w)
	}
	manager.teamWrapUpDue(fixture.profile, started.Add(bound))
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Kind != teams.PacketClosing || p.Report == nil || !p.Report.Incomplete {
		t.Fatalf("the resumed clock did not report at its bound: %+v", p)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	_ = teamAgent(t, fixture, fixture.manager, nil, nil)
	if waiting, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(waiting) != 1 {
		t.Fatal("a second start raised the report again")
	}
}

// A WRAP-UP ALREADY PAST ITS BOUND CLOSES ON THE START, and only once.
func TestTeamWrapUpPastItsBoundClosesOnceOnStart(t *testing.T) {
	fixture := newTeamFixture(t, true)
	stubCapSpend(t, &capSpend{usd: 0.5, stamp: "s1"})
	started := time.Now().Add(-20 * time.Minute).UTC()
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetWrap(fixture.teamID, started, 15*time.Minute)
	}); err != nil {
		t.Fatal(err)
	}
	first := teamAgent(t, fixture, fixture.manager, nil, nil)
	p := onlyPacket(t, fixture.profile, teams.Person)
	if p.Kind != teams.PacketClosing || p.Report == nil || !p.Report.Incomplete ||
		!strings.Contains(p.Question, "wrap-up incomplete") {
		t.Fatalf("a past wrap-up did not close on start: %+v", p)
	}
	f, _ := teams.Load(fixture.profile)
	if team, _ := f.Team(fixture.teamID); team.Wrap != nil {
		t.Fatalf("the finished clock was left on the team: %+v", team.Wrap)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	_ = teamAgent(t, fixture, fixture.manager, nil, nil)
	if waiting, _, _ := teams.OpenPackets(fixture.profile, teams.Person); len(waiting) != 1 {
		t.Fatal("a second start closed the team again")
	}
}

// THE PROFILE'S teams.wake IS THE DEFAULT A TEAM WITH NO OVERRIDE TAKES.
func TestTeamWakeFollowsTheProfileDefault(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	if roles := web.teamRoles(); len(roles) != 1 || !roles[0].wakes {
		t.Fatalf("a team with no override does not wake by default: %+v", roles)
	}
	if err := os.WriteFile(filepath.Join(fixture.profile, "config.json"), []byte(`{"teams.wake": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The config's stamp moves; the teams file's does not.
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(fixture.profile, "config.json"), later, later)
	if roles := web.teamRoles(); len(roles) != 1 || roles[0].wakes {
		t.Fatalf("teams.wake off did not reach the role: %+v", roles)
	}
}

// AT THE CAP A DIRECTIVE WAKES NOBODY: the member is held, the Traffic says
// so, and the person is asked once.
func TestTeamACapHoldsAWake(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	five := 5.0
	if err := teams.Update(fixture.profile, func(f *teams.File) error {
		return f.SetSettings(fixture.teamID, func(s *teams.Settings) { s.CapUSDDay = &five })
	}); err != nil {
		t.Fatal(err)
	}
	stubCapSpend(t, &capSpend{usd: 6, stamp: "s1"})
	webAnswers := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webAnswers, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	deadline := time.Now().Add(teamWakeSettle + 40*teamWatchEvery)
	held := false
	for time.Now().Before(deadline) && !held {
		log, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0)
		for _, entry := range log {
			held = held || strings.HasPrefix(entry.Text, "held @web: harbor reached its $5 cap today")
		}
		time.Sleep(teamWatchEvery)
	}
	if !held {
		t.Fatal("the Traffic never said the wake was held")
	}
	if webAnswers.requests() != 0 {
		t.Fatal("a member at the cap was woken")
	}
	onlyPacket(t, fixture.profile, teams.Person)
}
