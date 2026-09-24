package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// TEAM TRAFFIC WAKES, AS TESTS. Each drives a real agent on a real teams file
// and Traffic log, with the watch's clocks shortened: what is asserted is what
// starts a turn, how often, and what the Traffic says about it.

// fastTeamWake shortens the watch's two clocks for one test. The watch reads
// them when it starts, so they are set before any agent is made.
func fastTeamWake(t *testing.T) {
	t.Helper()
	every, settle := teamWatchEvery, teamWakeSettle
	teamWatchEvery, teamWakeSettle = 20*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { teamWatchEvery, teamWakeSettle = every, settle })
}

// quietFor lets the watch run for a while so a test can assert nothing more
// happened.
func quietFor() { time.Sleep(15 * teamWatchEvery) }

// trafficEvents is every event in the fixture's Traffic whose text holds want.
func trafficEvents(t *testing.T, fixture teamFixture, want string) []teams.Entry {
	t.Helper()
	all, err := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []teams.Entry
	for _, entry := range all {
		if entry.Kind == teams.KindEvent && strings.Contains(entry.Text, want) {
			out = append(out, entry)
		}
	}
	return out
}

// waitEvent waits until an event holding want is in the Traffic.
func waitEvent(t *testing.T, fixture teamFixture, want string) teams.Entry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := trafficEvents(t, fixture, want); len(got) > 0 {
			return got[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no event saying %q reached the traffic", want)
	return teams.Entry{}
}

// A DIRECTIVE WAKES AN IDLE MEMBER ONCE, as the manager's line and never the
// person's, and the rail is told.
func TestTeamWakeADirectiveWakesAnIdleMemberOnce(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(2)
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	waitRequests(t, completer, 1)
	first := userTextIn(completer.request(0))
	if !strings.Contains(first, "◆ directive from manager: Fix the header.") {
		t.Fatalf("the woken turn did not carry the directive:\n%s", first)
	}
	if !strings.Contains(first, "the person did not speak") {
		t.Errorf("the woken turn does not say who started it:\n%s", first)
	}
	waitIdle(t, web)
	quietFor()
	if got := completer.requests(); got != 1 {
		t.Fatalf("one directive started %d requests, want 1", got)
	}
	for _, entry := range web.Transcript() {
		if entry.Role == "user" {
			t.Fatalf("the woken turn carries words as the person's: %+v", entry)
		}
	}
	woke := waitEvent(t, fixture, "woke @web")
	if woke.From != teams.FromManager || woke.State != teams.StateRunning {
		t.Errorf("the wake's line is %+v, want from the manager with the member running", woke)
	}
}

// A NOTE WAKES NOBODY; it is read at the member's next turn.
func TestTeamWakeANoteDoesNotWakeAMember(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "The API moved to v2."})
	quietFor()
	if got := completer.requests(); got != 0 {
		t.Fatalf("a note started %d requests, want none", got)
	}
	submitAndWait(t, web, "carry on")
	if said := userTextIn(completer.request(0)); !strings.Contains(said, "The API moved to v2.") {
		t.Fatalf("the note was not read at the next turn:\n%s", said)
	}
}

// A BUSY MEMBER IS NOT STARTED A SECOND TIME. The directive waits for the turn
// in flight, and once that ends it is handed over exactly once.
func TestTeamWakeABusyMemberIsNotStartedTwice(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return textResponse("done"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("fixed"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("again"), nil },
	}}
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	events, err := web.Submit(context.Background(), "work on the footer")
	if err != nil {
		t.Fatal(err)
	}
	waitRequests(t, completer, 1)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	quietFor()
	if got := completer.requests(); got != 1 {
		t.Fatalf("a busy member was asked %d times while its turn ran, want 1", got)
	}
	if got := trafficEvents(t, fixture, "woke @web"); len(got) != 0 {
		t.Fatalf("a busy member was woken: %+v", got)
	}
	close(release)
	collect(t, events)
	waitRequests(t, completer, 2)
	waitIdle(t, web)
	quietFor()
	if got := completer.requests(); got != 2 {
		t.Fatalf("the directive was handed over in %d requests after the turn, want 1", got-1)
	}
	if said := userTextIn(completer.request(1)); strings.Count(said, "Fix the header.") != 1 {
		t.Fatalf("the directive was not handed over once:\n%s", said)
	}
}

// A MANAGER'S WAKE COALESCES: a burst of replies is one turn carrying them all.
func TestTeamWakeTheManagersWakeCoalescesABurst(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(2)
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "web", To: teams.ToManager, State: teams.StateFinished, Text: "finished"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "parser", To: teams.ToManager, Text: "The grammar is done; see grammar.md."})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "parser", To: teams.ToManager, State: teams.StateFailed, Text: "failed: boom"})
	waitRequests(t, completer, 1)
	waitIdle(t, manager)
	time.Sleep(2 * teamWakeSettle)
	if got := completer.requests(); got != 1 {
		t.Fatalf("a burst of three replies started %d turns, want 1", got)
	}
	said := userTextIn(completer.request(0))
	for _, want := range []string{"@web finished its turn", "from @parser: The grammar is done", "@parser failed: boom", "the person did not speak"} {
		if !strings.Contains(said, want) {
			t.Errorf("the manager's woken turn lacks %q:\n%s", want, said)
		}
	}
	woke := waitEvent(t, fixture, "woke ◆")
	if woke.From != "web" || !strings.Contains(woke.Text, "(with @parser)") {
		t.Errorf("the manager's wake line is %+v, want from @web with @parser", woke)
	}
}

// A MEMBER'S EVENTS THAT ASK NOTHING WAKE NOBODY: a stop, a question coming
// down, and the wake lines themselves.
func TestTeamWakeTheManagerSleepsThroughLinesThatAskNothing(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(1)
	teamAgent(t, fixture, fixture.manager, completer, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "web", To: teams.ToManager, State: teams.StateIdle, Text: "stopped"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "web", To: teams.ToManager, State: teams.StateRunning, Text: "no longer waiting"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToRoom, Text: "lunch"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: teams.FromManager, To: "web", State: teams.StateRunning, Text: "woke @web"})
	time.Sleep(teamWakeSettle + 15*teamWatchEvery)
	if got := completer.requests(); got != 0 {
		t.Fatalf("lines that ask nothing of the manager started %d turns", got)
	}
}

// THE WAKE LIMIT HOLDS, and the Traffic says so once.
func TestTeamWakeAConversationIsWokenAtMostTwentyTimesAnHour(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(1)
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	now := time.Now()
	web.team.mu.Lock()
	for i := 0; i < teamWakesPerHour; i++ {
		web.team.watch.woken = append(web.team.watch.woken, now.Add(-time.Duration(i)*time.Minute))
	}
	web.team.mu.Unlock()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "And the footer."})
	said := waitEvent(t, fixture, "could not wake @web")
	quietFor()
	if got := completer.requests(); got != 0 {
		t.Fatalf("a conversation past its limit was woken %d times", got)
	}
	if !strings.Contains(said.Text, "20 times in the last hour") {
		t.Errorf("the limit's line does not say the limit: %q", said.Text)
	}
	if got := trafficEvents(t, fixture, "could not wake @web"); len(got) != 1 {
		t.Errorf("the limit was said %d times, want once", len(got))
	}
}

// THE LOOP BREAKER ASKS THE PERSON rather than wake the manager again, and the
// person speaking resets it.
func TestTeamWakeTheLoopBreakerRaisesItToThePerson(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(3)
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	manager.team.mu.Lock()
	manager.team.watch.rounds = teamLoopRounds
	manager.team.watch.heard = manager.team.person.n.Load()
	manager.team.mu.Unlock()
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "web", To: teams.ToManager, State: teams.StateFinished, Text: "finished"})
	asked := waitEvent(t, fixture, "woken me")
	if asked.From != teams.FromManager || asked.State != teams.StateAsking {
		t.Errorf("the breaker's line is %+v, want an asking event from the manager", asked)
	}
	quietFor()
	if got := completer.requests(); got != 0 {
		t.Fatalf("the manager was woken %d times past the loop bound", got)
	}
	submitAndWait(t, manager, "keep going")
	if said := userTextIn(completer.request(0)); !strings.Contains(said, "stopped waking you") {
		t.Errorf("the manager was not told why nothing woke it:\n%s", said)
	}
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindEvent, From: "parser", To: teams.ToManager, State: teams.StateFinished, Text: "finished"})
	waitRequests(t, completer, 2)
}

// WITH THE TEAM'S AUTO-WAKE OFF, NOTHING WAKES, and the manager is told so.
func TestTeamWakeAWakeOffTeamWakesNobody(t *testing.T) {
	fastTeamWake(t)
	fixture := newTeamFixture(t, true)
	webAnswers, managerAnswers := oneAnswer(1), oneAnswer(1)
	teamAgent(t, fixture, fixture.web, webAnswers, nil)
	manager := teamAgent(t, fixture, fixture.manager, managerAnswers, nil)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "done"})
	time.Sleep(teamWakeSettle + 15*teamWatchEvery)
	if webAnswers.requests() != 0 || managerAnswers.requests() != 0 {
		t.Fatalf("a team with wake off woke a conversation: web %d, manager %d", webAnswers.requests(), managerAnswers.requests())
	}
	submitAndWait(t, manager, "status?")
	if role := strings.Join(roleNotes(managerAnswers.request(0)), "\n"); !strings.Contains(role, "auto-wake is off") {
		t.Errorf("the manager of a team with wake off was told otherwise:\n%s", role)
	}
}

// THE MANAGER'S ROLE SAYS WHAT WAKES, on a team where it does.
func TestTeamWakeTheManagersRoleSaysDirectivesWakeAndRepliesComeBack(t *testing.T) {
	fixture := newWakingTeamFixture(t)
	completer := oneAnswer(1)
	manager := teamAgent(t, fixture, fixture.manager, completer, nil)
	submitAndWait(t, manager, "what's happening?")
	role := strings.Join(roleNotes(completer.request(0)), "\n")
	for _, want := range []string{"starts an idle member's turn", "a note waits", "come back to you and start your turn"} {
		if !strings.Contains(role, want) {
			t.Errorf("the manager's role lacks %q:\n%s", want, role)
		}
	}
}

// A MEMBER OPENED FOR A DIRECTIVE IS WOKEN BY IT. The engine opens a member
// nobody holds because a directive was just sent to it, and a member that
// never read this team before starts at that directive, not after it.
func TestTeamWakeAMemberOpenedForADirectiveIsWokenByIt(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "old news"})
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."})
	time.Sleep(5 * time.Millisecond)
	completer := oneAnswer(1)
	teamAgent(t, fixture, fixture.web, completer, nil)
	waitRequests(t, completer, 1)
	said := userTextIn(completer.request(0))
	if !strings.Contains(said, "Fix the header.") {
		t.Fatalf("the member opened for a directive was not handed it:\n%s", said)
	}
	if strings.Contains(said, "old news") {
		t.Errorf("the member was handed traffic from before the directive:\n%s", said)
	}
}

// recordedResume is a stand-in for the engine's door, recording what it was
// asked to open.
type recordedResume struct {
	mu     sync.Mutex
	opened []string
	fail   error
}

func (r *recordedResume) open(file, workspace string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = append(r.opened, file+"|"+workspace)
	return r.fail
}

func (r *recordedResume) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.opened...)
}

func withTeamResume(t *testing.T, open func(file, workspace string) error) {
	t.Helper()
	SetTeamResume(open)
	t.Cleanup(func() { SetTeamResume(nil) })
}

func sendDirective(t *testing.T, manager *Agent, to string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"to": to, "text": "Fix the header.", "kind": "directive"})
	said, failed, err := manager.teamSendTool(context.Background(), args)
	if err != nil || failed {
		t.Fatalf("team_send refused: %s %v", said, err)
	}
	return said
}

// A DIRECTIVE TO A MEMBER NOBODY HOLDS OPENS IT through the engine's door, and
// one somebody holds is left to wake itself.
func TestTeamWakeADirectiveOpensAMemberNobodyHolds(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	recorder := &recordedResume{}
	withTeamResume(t, recorder.open)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)
	parser := oneAnswer(1)
	teamAgent(t, fixture, fixture.parser, parser, nil)
	said := sendDirective(t, manager, "web")
	if !strings.Contains(said, "starts a turn on it now") || !strings.Contains(said, "wake you") {
		t.Errorf("the send does not say what happens next: %s", said)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(recorder.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	opened := recorder.seen()
	if len(opened) != 1 || !strings.HasPrefix(opened[0], fixture.web+"|") {
		t.Fatalf("opened %v, want the web member's transcript once", opened)
	}
	sendDirective(t, manager, "parser")
	waitRequests(t, parser, 1)
	if got := recorder.seen(); len(got) != 1 {
		t.Fatalf("a member that is open was opened again: %v", got)
	}
}

// AND WHERE IT CANNOT BE OPENED, THE TRAFFIC SAYS SO rather than the manager
// being told a wake that never happens.
func TestTeamWakeAMemberThatCannotBeOpenedIsSaidInTheTraffic(t *testing.T) {
	fastTeamWake(t)
	fixture := newWakingTeamFixture(t)
	manager := teamAgent(t, fixture, fixture.manager, oneAnswer(1), nil)

	SetTeamResume(nil)
	sendDirective(t, manager, "web")
	said := waitEvent(t, fixture, "could not wake @web")
	if !strings.Contains(said.Text, "cannot open a conversation headless") {
		t.Errorf("the missing road is not named: %q", said.Text)
	}

	recorder := &recordedResume{fail: errors.New("the host would not start")}
	withTeamResume(t, recorder.open)
	sendDirective(t, manager, "web")
	deadline := time.Now().Add(5 * time.Second)
	for len(trafficEvents(t, fixture, "the host would not start")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := trafficEvents(t, fixture, "the host would not start"); len(got) != 1 {
		t.Fatalf("a failed open was said %d times, want once", len(got))
	}
}
