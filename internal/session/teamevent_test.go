package session

// THE TEAM'S EVENTS AND THE BRIEF, AS TESTS: what a member says without being
// asked, how a status reads a member held on a prompt, what the person is asked
// when a manager starts a member, and how a started member is handed its brief.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// holdJournal is a process holding a member's transcript lock, the way a live
// session does, for as long as the test runs.
func holdJournal(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filelock.Unlock(file) })
}

// teamEvents is every event in the fixture's log, oldest first.
func teamEvents(t *testing.T, fixture teamFixture) []teams.Entry {
	t.Helper()
	all, err := teams.ReadTraffic(fixture.profile, fixture.teamID, teamLogStart, 0)
	if err != nil {
		t.Fatal(err)
	}
	var events []teams.Entry
	for _, entry := range all {
		if entry.Kind == teams.KindEvent {
			events = append(events, entry)
		}
	}
	return events
}

func TestAMemberTellsItsManagerHowEachTurnEnded(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()

	failed := &eventHub{}
	failed.send(Event{Kind: EventError, Err: errors.New("provider said no\nand more")})
	web.teamTurnEnded(context.Background(), failed)
	stopped, stop := context.WithCancel(context.Background())
	stop()
	web.teamTurnEnded(stopped, &eventHub{})
	web.teamTurnEnded(context.Background(), &eventHub{})
	web.settleTeamEvents()

	events := teamEvents(t, fixture)
	want := []struct{ state, text string }{
		{teams.StateFailed, "failed: provider said no"},
		{teams.StateIdle, "stopped"},
		{teams.StateFinished, "finished"},
	}
	if len(events) != len(want) {
		t.Fatalf("%d events written, want %d: %+v", len(events), len(want), events)
	}
	for index, event := range events {
		if event.From != "web" || event.To != teams.ToManager || event.Member != convKeyOf(t, fixture.web) ||
			event.State != want[index].state || event.Text != want[index].text {
			t.Errorf("event %d is %+v, want web -> manager, state %s, text %q", index, event, want[index].state, want[index].text)
		}
	}
}

// A WHOLE TURN WRITES ONE EVENT, whatever it did inside.
func TestATurnWritesOneEventAtItsEnd(t *testing.T) {
	fixture := newTeamFixture(t, true)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	web := teamAgent(t, fixture, fixture.web, completer, nil)
	events, err := web.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	web.settleTeamEvents()
	written := teamEvents(t, fixture)
	if len(written) != 1 || written[0].State != teams.StateFinished {
		t.Fatalf("a turn wrote %+v, want one finished event", written)
	}
}

func TestOnlyAManagedMemberWritesEvents(t *testing.T) {
	managed := newTeamFixture(t, true)
	manager := teamAgent(t, managed, managed.manager, nil, nil)
	manager.teamBoundary()
	manager.teamTurnEnded(context.Background(), &eventHub{})
	manager.settleTeamEvents()
	if events := teamEvents(t, managed); len(events) != 0 {
		t.Fatalf("the manager wrote events about itself: %+v", events)
	}

	unmanaged := newTeamFixture(t, false)
	web := teamAgent(t, unmanaged, unmanaged.web, nil, nil)
	web.teamBoundary()
	web.teamTurnEnded(context.Background(), &eventHub{})
	web.settleTeamEvents()
	if events := teamEvents(t, unmanaged); len(events) != 0 {
		t.Fatalf("a member of a team with no manager wrote events: %+v", events)
	}
}

// ONE EVENT UP AND ONE DOWN, however many questions a batch raises.
func TestAWaitIsOneEventUpAndOneDown(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	permission := Question{Ask: AskPermission, Head: "needs your ok to run bash", Blocking: Blocking{Turn: true}}
	first := web.teamAsking(permission)
	second := web.teamAsking(permission)
	quiet := web.teamAsking(Question{Ask: AskChoice, Head: "not blocking"})
	quiet()
	first()
	first()
	second()
	web.settleTeamEvents()
	events := teamEvents(t, fixture)
	if len(events) != 2 {
		t.Fatalf("%d events, want one asking and one back to work: %+v", len(events), events)
	}
	if events[0].State != teams.StateAsking || events[0].Text != "needs your ok to run bash" {
		t.Errorf("the wait was written as %+v", events[0])
	}
	if events[1].State != teams.StateRunning {
		t.Errorf("the end of the wait was written as %+v", events[1])
	}

	web.teamAsking(Question{Ask: AskClarification, Head: "Which colour?", Blocking: Blocking{Turn: true}})
	web.settleTeamEvents()
	if events := teamEvents(t, fixture); events[len(events)-1].Text != "asks: Which colour?" {
		t.Errorf("a question was written as %+v", events[len(events)-1])
	}
}

// A MEMBER HELD ON A PERMISSION PROMPT READS AS ASKING, and as running again
// once its journal moves past the prompt.
func TestAMemberHeldOnAPromptReadsAsAsking(t *testing.T) {
	fixture := newTeamFixture(t, true)
	now := time.Now()
	callAt := now.Add(-time.Minute)
	writeJournal(t, fixture.web,
		sessionEntry{Type: "message", Role: "user", Content: "next", Timestamp: callAt.Add(-time.Second).Format(time.RFC3339Nano)},
		sessionEntry{Type: "message", Role: "assistant", Timestamp: callAt.Format(time.RFC3339Nano),
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"make"}`}}}},
	)
	holdJournal(t, fixture.web)
	appendTraffic(t, fixture, teams.Entry{At: callAt.Add(time.Second), Kind: teams.KindEvent, From: "web", To: teams.ToManager,
		Member: convKeyOf(t, fixture.web), Text: "needs your ok to run bash", State: teams.StateAsking})
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(fixture.teamID)
	log, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", teamStateLook)
	web := memberStates(team, nil, now, log, nil)[convKeyOf(t, fixture.web)]
	if web.State != teams.StateAsking || web.Question != "needs your ok to run bash" {
		t.Fatalf("a member held on a prompt reads %+v", web)
	}

	writeJournal(t, fixture.web,
		sessionEntry{Type: "message", Role: "user", Content: "next", Timestamp: callAt.Add(-time.Second).Format(time.RFC3339Nano)},
		sessionEntry{Type: "message", Role: "assistant", Timestamp: callAt.Format(time.RFC3339Nano),
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"make"}`}}}},
		sessionEntry{Type: "message", Role: "tool", ToolCallID: "c1", Content: "ok", Timestamp: callAt.Add(2 * time.Second).Format(time.RFC3339Nano)},
	)
	if web := memberStates(team, nil, now, log, nil)[convKeyOf(t, fixture.web)]; web.State != teams.StateRunning {
		t.Fatalf("a member whose prompt was answered reads %+v", web)
	}
}

// A PROCESS THAT DIED ON THE PROMPT IS NOT STILL ASKING. Nothing holds the
// transcript lock, so the asking event is stale and the member reads idle.
func TestADeadMemberWaitingOnAPromptReadsIdle(t *testing.T) {
	fixture := newTeamFixture(t, true)
	now := time.Now()
	callAt := now.Add(-time.Minute)
	writeJournal(t, fixture.web,
		sessionEntry{Type: "message", Role: "user", Content: "next", Timestamp: callAt.Add(-time.Second).Format(time.RFC3339Nano)},
		sessionEntry{Type: "message", Role: "assistant", Timestamp: callAt.Format(time.RFC3339Nano),
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"make"}`}}}},
	)
	appendTraffic(t, fixture, teams.Entry{At: callAt.Add(time.Second), Kind: teams.KindEvent, From: "web", To: teams.ToManager,
		Member: convKeyOf(t, fixture.web), Text: "needs your ok to run bash", State: teams.StateAsking})
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(fixture.teamID)
	log, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", teamStateLook)
	web := memberStates(team, nil, now, log, nil)[convKeyOf(t, fixture.web)]
	if web.State != teams.StateIdle || web.Question != "" {
		t.Fatalf("a member whose process died on a prompt reads %+v, want idle", web)
	}
}

// AN ASKING EVENT OLDER THAN THE BOUND IS IDLE EVEN WHILE THE LOCK IS HELD.
func TestAnOldAskingEventReadsIdle(t *testing.T) {
	fixture := newTeamFixture(t, true)
	now := time.Now()
	callAt := now.Add(-31 * time.Minute)
	writeJournal(t, fixture.web,
		sessionEntry{Type: "message", Role: "user", Content: "next", Timestamp: callAt.Add(-time.Second).Format(time.RFC3339Nano)},
		sessionEntry{Type: "message", Role: "assistant", Timestamp: callAt.Format(time.RFC3339Nano),
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"make"}`}}}},
	)
	holdJournal(t, fixture.web)
	appendTraffic(t, fixture, teams.Entry{At: callAt.Add(time.Second), Kind: teams.KindEvent, From: "web", To: teams.ToManager,
		Member: convKeyOf(t, fixture.web), Text: "needs your ok to run bash", State: teams.StateAsking})
	file, err := teams.Load(fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(fixture.teamID)
	log, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, "", teamStateLook)
	web := memberStates(team, nil, now, log, nil)[convKeyOf(t, fixture.web)]
	if web.State != teams.StateIdle {
		t.Fatalf("an asking event older than the bound reads %+v, want idle", web)
	}
}

// ── the start's card ────────────────────────────────────────────────────────

func TestTheStartsCardSaysWhoIsStartedAndWhatItCosts(t *testing.T) {
	call := ai.ToolCall{ID: "s1", Type: "function", Function: ai.ToolCallFunction{Name: teamStartToolName,
		Arguments: `{"handle":"@Lexer","brief":"Rewrite the lexer so string escapes are handled in one pass\nthen the tests"}`}}
	if head := ConsentHead(call.Function.Name, call.Function.Arguments); head != "◆ manager wants to start @lexer" {
		t.Errorf("the head is %q", head)
	}
	if head := ConsentHead(call.Function.Name, argsText(call)); head != "◆ manager wants to start @lexer" {
		t.Errorf("the head off the event's args is %q", head)
	}
	if said := gloss(call); said != "team_start @lexer: Rewrite the lexer so string escapes are handled in one pass" {
		t.Errorf("the gloss is %q", said)
	}
	if reason := consentReason(call, approval.Decision{}); reason != teamStartCost {
		t.Errorf("the reason is %q", reason)
	}
	if rule := consentRule(call, approval.Decision{Rule: "a rule said so"}); rule != "a rule said so" {
		t.Errorf("a policy's own words were replaced: %q", rule)
	}
	bash := ai.ToolCall{Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"ls"}`}}
	if head := ConsentHead("bash", bash.Function.Arguments); head != "needs your ok to run bash" {
		t.Errorf("an ordinary head changed: %q", head)
	}
	if reason := consentReason(bash, approval.Decision{}); reason != ConsentFallbackReason {
		t.Errorf("an ordinary reason changed: %q", reason)
	}
	if head := ConsentHead(teamStartToolName, `{"brief":"no handle"}`); head != "needs your ok to run team_start" {
		t.Errorf("a start with no handle reads %q", head)
	}
}

func TestAStopWithNoReasonStillCarriesOne(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	if text, failed, _ := manager.teamStopTool(context.Background(), json.RawMessage(`{"handle":"web"}`)); failed {
		t.Fatal(text)
	}
	entries, _ := teams.ReadTraffic(fixture.profile, fixture.teamID, teamLogStart, 0)
	if len(entries) != 1 || entries[0].Kind != teams.KindStop || strings.TrimSpace(entries[0].Text) == "" {
		t.Fatalf("the stop was written as %+v", entries)
	}
}

// ── the brief ───────────────────────────────────────────────────────────────

// A STARTED MEMBER IS HANDED ITS BRIEF ON ITS FIRST REQUEST, marked as the
// manager's, recorded as the session's note, and replayed as a line a surface
// can draw as a quoted card.
func TestAStartedMemberIsHandedItsBriefAsTheManagers(t *testing.T) {
	fixture := newTeamFixture(t, true)
	brief := "Rewrite the lexer.\n\nDone is: the escape tests pass."
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindStart, From: teams.FromManager, To: "parser", Text: brief})
	time.Sleep(2 * time.Millisecond)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("starting"), nil },
	}}
	parser := teamAgent(t, fixture, fixture.parser, completer, nil)
	events, err := parser.Submit(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	request := userTextIn(completer.request(0))
	if !strings.Contains(request, teamBriefWord+" #1: Rewrite the lexer.") || !strings.Contains(request, "not the person's words") {
		t.Fatalf("the first request did not carry the marked brief:\n%s", request)
	}
	var asides []DisplayEntry
	for _, entry := range parser.Transcript() {
		if entry.Role == "user" && strings.Contains(entry.Text, "Rewrite the lexer") {
			t.Fatalf("the brief was recorded as the person's words: %+v", entry)
		}
		if entry.Role == "aside" && entry.Team != nil {
			asides = append(asides, entry)
		}
	}
	if len(asides) != 1 || len(asides[0].Team) != 1 {
		t.Fatalf("the brief replays as %+v", asides)
	}
	line := asides[0].Team[0]
	if line.Kind != teams.KindStart || line.From != teams.FromManager || line.To != "" || line.Team != "harbor" || line.Text != brief {
		t.Fatalf("the brief's line is %+v", line)
	}

	web := teamAgent(t, fixture, fixture.web, nil, nil)
	if news := web.teamBoundary(); strings.Contains(news, "Rewrite the lexer") {
		t.Fatalf("another member was handed the brief: %q", news)
	}
}

// THE PARSE AND THE COMPOSER AGREE on every shape a delivery has.
func TestADeliveryReadsBackLineByLine(t *testing.T) {
	member := teamRole{id: "t", name: `the "harbor"`, handle: "web", managed: true}
	manager := teamRole{id: "t", name: "harbor", handle: "boss", manager: true, managed: true}
	entries := []teams.Entry{
		{Kind: teams.KindStart, From: teams.FromManager, To: "web", Text: "the brief\nsecond line"},
		{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "a note: with a colon"},
		{Kind: teams.KindDirective, From: teams.FromManager, To: teams.ToEveryone, Text: "freeze"},
		{Kind: teams.KindNote, From: "parser", To: teams.ToRoom, Text: "tokens ready"},
	}
	var lines []string
	for _, entry := range entries {
		lines = append(lines, teamLine(member, entry))
	}
	managerLines := []string{
		teamLine(manager, teams.Entry{Kind: teams.KindNote, From: "web", To: teams.ToManager, Text: "blocked"}),
		teamLine(manager, teams.Entry{Kind: teams.KindNote, From: "web", To: "parser", Text: "shape?"}),
	}
	text := teamNewsGroup(member, lines) + "\n\n" + teamNewsGroup(manager, managerLines)
	got := teamNewsLines(text)
	want := []TeamLine{
		{Team: `the "harbor"`, From: teams.FromManager, Kind: teams.KindStart, Text: "the brief\nsecond line"},
		{Team: `the "harbor"`, From: teams.FromManager, Kind: teams.KindNote, Text: "a note: with a colon"},
		{Team: `the "harbor"`, From: teams.FromManager, To: teams.ToEveryone, Kind: teams.KindDirective, Text: "freeze"},
		{Team: `the "harbor"`, From: "parser", To: teams.ToRoom, Kind: teams.KindNote, Text: "tokens ready"},
		{Team: "harbor", From: "web", Kind: teams.KindNote, Text: "blocked"},
		{Team: "harbor", From: "web", To: "parser", Kind: teams.KindNote, Text: "shape?"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d lines read back, want %d:\n%s\n%+v", len(got), len(want), text, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("line %d reads %+v, want %+v", index, got[index], want[index])
		}
	}
	if teamNewsLines("a task landed") != nil {
		t.Error("an ordinary aside was read as a delivery")
	}
}
