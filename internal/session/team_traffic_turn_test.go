package session

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// THE TRAFFIC A TURN WRITES NAMES THAT TURN, and these tests pin it against the
// two places the same facts are recorded independently: the person's message id
// on the conversation's own journal line, and the run id its journal records as
// `usage_run`. Nothing here sleeps, spends or reads a clock; every id is read
// back from the real journal after the turn, so a stamp that was merely
// plausible would fail.

// directiveStep is a model step that sends the web member a directive.
func directiveStep(text string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		args, _ := json.Marshal(map[string]string{"to": "web", "text": text, "kind": "directive"})
		return toolResponse("call-send", teamSendToolName, string(args)), nil
	}
}

func finishingStep() step {
	return func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }
}

func TestTeamClosingReportCarriesItsOpeningMessageAndRun(t *testing.T) {
	fixture := newTeamFixture(t, true)
	report := func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("close-report", teamCloseReportToolName, `{"done":"The requested work is complete"}`), nil
	}
	manager := teamAgent(t, fixture, fixture.manager, &scriptedCompleter{steps: []step{report, finishingStep()}}, nil)
	manager.teamBoundary()
	collect(t, mustSubmit(t, manager, "Report what is ready"))
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	ids, runs := journalPersonIDs(t, fixture.manager), journalRunIDs(t, fixture.manager)
	packets := trafficOf(t, fixture, teams.KindPacket)
	if len(ids) != 1 || len(runs) != 1 || len(packets) != 1 {
		t.Fatalf("message ids %v runs %v packets %+v", ids, runs, packets)
	}
	if p := packets[0]; p.Turn != ids[0] || p.Run != runs[0] || p.RunBy != convKeyOf(t, fixture.manager) {
		t.Fatalf("closing packet provenance %+v does not match message %s and run %s", p.Provenance, ids[0], runs[0])
	}
}

// journalPersonIDs is the durable id on each person line of the journal at
// path, oldest first, read the way a restarted process reads it.
func journalPersonIDs(t *testing.T, path string) []string {
	t.Helper()
	var ids []string
	for _, entry := range personEntries(ReadTranscript(path).Entries) {
		if entry.Person == nil || entry.Person.Composed == nil || entry.Person.Composed.MessageID == "" {
			t.Fatalf("a person line in %s has no durable id: %+v", path, entry)
		}
		ids = append(ids, entry.Person.Composed.MessageID)
	}
	return ids
}

// journalRunIDs is each turn's `usage_run` id in the journal at path.
func journalRunIDs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var runs []string
	for _, line := range strings.Split(string(raw), "\n") {
		var v struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		_ = json.Unmarshal([]byte(line), &v)
		if v.Type == "usage_run" {
			runs = append(runs, v.ID)
		}
	}
	return runs
}

// trafficOf is the fixture's lines of one kind, oldest first.
func trafficOf(t *testing.T, fixture teamFixture, kind string) []teams.Entry {
	t.Helper()
	var out []teams.Entry
	for _, entry := range threadLog(t, fixture) {
		if entry.Kind == kind {
			out = append(out, entry)
		}
	}
	return out
}

// A DIRECTIVE CARRIES THE MESSAGE THAT OPENED ITS TURN, byte for byte what the
// journal holds, whichever door the person's message came through, and it
// survives the process: the ids are read back from the journal after Close.
func TestTeamTrafficCarriesTheJournaledMessageAndRunOfItsTurn(t *testing.T) {
	for name, send := range map[string]func(*Agent) (string, <-chan Event){
		"plain submit": func(a *Agent) (string, <-chan Event) { return "", mustSubmit(t, a, "ship it") },
		"receipt road": func(a *Agent) (string, <-chan Event) {
			receipt, events, err := a.SubmitComposed(context.Background(), Submission{Text: "ship it", ClientID: "door-1"})
			if err != nil {
				t.Fatal(err)
			}
			return receipt.MessageID, events
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newTeamFixture(t, true)
			manager := teamAgent(t, fixture, fixture.manager, &scriptedCompleter{steps: []step{directiveStep("start on the header"), finishingStep()}}, nil)
			manager.teamBoundary()
			receiptID, events := send(manager)
			collect(t, events)
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}

			ids, runs := journalPersonIDs(t, fixture.manager), journalRunIDs(t, fixture.manager)
			sent := trafficOf(t, fixture, teams.KindDirective)
			if len(ids) != 1 || len(runs) != 1 || len(sent) != 1 {
				t.Fatalf("ids %v runs %v traffic %+v", ids, runs, sent)
			}
			line := sent[0]
			if line.Turn != ids[0] || line.Run != runs[0] || line.RunBy != convKeyOf(t, fixture.manager) {
				t.Errorf("the directive says turn %q run %q by %q; the journal holds message %q run %q (conversation %q)",
					line.Turn, line.Run, line.RunBy, ids[0], runs[0], convKeyOf(t, fixture.manager))
			}
			if receiptID != "" && receiptID != line.Turn {
				t.Errorf("the receipt said %q and the traffic %q", receiptID, line.Turn)
			}
			if line.Turn == line.Run {
				t.Error("a message id and a run id are the same string")
			}
			if strings.Contains(line.Turn, "door-1") {
				t.Error("a door's client id leaked into the turn")
			}
		})
	}
}

// TWO TURNS THAT SAID THE SAME WORDS ARE TWO TURNS. The words, the directive's
// text and (as far as a test can arrange it) the moment are identical; only the
// ids tell them apart, and each directive names its own journal line.
func TestTeamTrafficKeepsTwoIdenticalTurnsApart(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, &scriptedCompleter{steps: []step{
		directiveStep("same"), finishingStep(), directiveStep("same"), finishingStep(),
	}}, nil)
	manager.teamBoundary()
	collect(t, mustSubmit(t, manager, "go"))
	collect(t, mustSubmit(t, manager, "go"))
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	ids, runs, sent := journalPersonIDs(t, fixture.manager), journalRunIDs(t, fixture.manager), trafficOf(t, fixture, teams.KindDirective)
	if len(ids) != 2 || len(runs) != 2 || len(sent) != 2 || ids[0] == ids[1] || runs[0] == runs[1] {
		t.Fatalf("ids %v runs %v traffic %+v", ids, runs, sent)
	}
	for i, line := range sent {
		if line.Text != "same" || line.Turn != ids[i] || line.Run != runs[i] {
			t.Errorf("directive %d: %+v, want turn %q run %q", i, line, ids[i], runs[i])
		}
	}
}

// A STEER IS NOT THE OPENER. The message that arrives while the turn runs lands
// in the journal with its own id, and the turn's traffic keeps naming the
// message that opened it, before and after the steer.
func TestTeamTrafficSteerDoesNotChangeTheTurnsOpener(t *testing.T) {
	fixture := newTeamFixture(t, true)
	steered := make(chan struct{})
	released := make(chan struct{})
	first := func(ctx context.Context, m []ai.Message) (*ai.Response, error) {
		close(steered)
		<-released
		return directiveStep("before the steer lands")(ctx, m)
	}
	manager := teamAgent(t, fixture, fixture.manager, &scriptedCompleter{steps: []step{first, directiveStep("after the steer landed"), finishingStep()}}, nil)
	manager.teamBoundary()
	events := mustSubmit(t, manager, "open")
	<-steered
	steer := mustSubmit(t, manager, "actually, also this")
	close(released)
	collect(t, events)
	collect(t, steer)
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	ids, runs, sent := journalPersonIDs(t, fixture.manager), journalRunIDs(t, fixture.manager), trafficOf(t, fixture, teams.KindDirective)
	if len(ids) != 2 || len(runs) != 1 || len(sent) != 2 {
		t.Fatalf("ids %v runs %v traffic %+v", ids, runs, sent)
	}
	for _, line := range sent {
		if line.Turn != ids[0] || line.Run != runs[0] {
			t.Errorf("%q says turn %q run %q, want the opener %q run %q (the steer is %q)", line.Text, line.Turn, line.Run, ids[0], runs[0], ids[1])
		}
	}
}

// A TURN NOBODY TYPED HAS A RUN AND NO MESSAGE. A wake started by the session's
// own note stamps the run it is and says nothing about a person, and it is not
// charged to the last thing the person said.
func TestTeamTrafficWakeTurnCarriesItsRunAndNoPersonMessage(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, &scriptedCompleter{steps: []step{
		directiveStep("from the person's turn"), finishingStep(), directiveStep("from the wake"), finishingStep(),
	}}, nil)
	manager.teamBoundary()
	collect(t, mustSubmit(t, manager, "go"))
	if started, why := manager.teamWakeWith("a member reported"); !started {
		t.Fatalf("no wake: %s", why)
	}
	manager.mu.Lock()
	done := manager.done
	manager.mu.Unlock()
	if done != nil {
		<-done
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	ids, runs, sent := journalPersonIDs(t, fixture.manager), journalRunIDs(t, fixture.manager), trafficOf(t, fixture, teams.KindDirective)
	if len(ids) != 1 || len(runs) != 2 || len(sent) != 2 {
		t.Fatalf("ids %v runs %v traffic %+v", ids, runs, sent)
	}
	if sent[0].Turn != ids[0] || sent[0].Run != runs[0] {
		t.Errorf("the person's turn: %+v", sent[0])
	}
	if sent[1].Turn != "" || sent[1].Run != runs[1] || sent[1].RunBy == "" {
		t.Errorf("the wake turn: %+v, want run %q and no turn", sent[1], runs[1])
	}
}

// AN EVENT WRITTEN AFTER ITS TURN ENDED, AND AFTER THE NEXT ONE BEGAN AND ENDED,
// STILL NAMES THE TURN THAT RAISED IT. The writer is held while two real turns
// run, then released: the lines it writes must carry the origin each event was
// queued with, never the agent's state at write time.
func TestTeamTrafficMemberEventsKeepTheirOriginAfterLaterTurns(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, &scriptedCompleter{steps: []step{finishingStep(), finishingStep()}}, nil)
	web.teamBoundary()
	// Hold the writer: with a writer "running", nothing is written until we say.
	hold := make(chan struct{})
	web.team.events.mu.Lock()
	web.team.events.done = hold
	web.team.events.mu.Unlock()

	collect(t, mustSubmit(t, web, "first words"))
	collect(t, mustSubmit(t, web, "first words"))
	if got := len(trafficOf(t, fixture, teams.KindEvent)); got != 0 {
		t.Fatalf("%d events were written while the writer was held", got)
	}
	web.writeTeamEvents(hold)
	if err := web.Close(); err != nil {
		t.Fatal(err)
	}
	ids, runs, events := journalPersonIDs(t, fixture.web), journalRunIDs(t, fixture.web), trafficOf(t, fixture, teams.KindEvent)
	if len(ids) != 2 || len(runs) != 2 || len(events) != 2 || ids[0] == ids[1] {
		t.Fatalf("ids %v runs %v events %+v", ids, runs, events)
	}
	for i, event := range events {
		if event.State != teams.StateFinished || event.Turn != ids[i] || event.Run != runs[i] || event.RunBy != convKeyOf(t, fixture.web) {
			t.Errorf("event %d: %+v, want turn %q run %q", i, event, ids[i], runs[i])
		}
	}
}

// A QUESTION'S TWO EVENTS NAME THE TURN THAT ASKED, including the "no longer
// waiting" one, which is raised by whoever answers, later.
func TestTeamTrafficAskingAndResumeShareTheAskersOrigin(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	asker := turnOrigin{run: "run-asking", turn: "msg-asking"}
	ctx := withTurnOrigin(context.Background(), asker)
	down := web.teamAsking(ctx, Question{Ask: AskPermission, Head: "needs your ok", Blocking: Blocking{Turn: true}})
	// The answer comes from another turn's context entirely.
	later := withTurnOrigin(context.Background(), turnOrigin{run: "run-later", turn: "msg-later"})
	_ = later
	down()
	web.settleTeamEvents()
	events := trafficOf(t, fixture, teams.KindEvent)
	if len(events) != 2 || events[0].State != teams.StateAsking || events[1].State != teams.StateRunning {
		t.Fatalf("events %+v", events)
	}
	for _, event := range events {
		if event.Turn != "msg-asking" || event.Run != "run-asking" {
			t.Errorf("%s event names turn %q run %q", event.State, event.Turn, event.Run)
		}
	}
}

// AN ABANDONED TURN'S LATE WRITE STILL NAMES ITSELF. A call made under the old
// turn's context after a new turn has begun stamps the old run.
func TestTeamTrafficLateCallKeepsItsOwnTurnsOrigin(t *testing.T) {
	fixture := newTeamFixture(t, true)
	manager := teamAgent(t, fixture, fixture.manager, nil, nil)
	manager.teamBoundary()
	old := withTurnOrigin(context.Background(), turnOrigin{run: "run-old", turn: "msg-old"})
	// A newer turn is current on the agent; nothing about it may leak.
	manager.mu.Lock()
	manager.turnSeq += 5
	manager.mu.Unlock()
	if said, failed, err := manager.teamSendTool(old, json.RawMessage(`{"to":"web","text":"late","kind":"directive"}`)); failed || err != nil {
		t.Fatalf("%q %v", said, err)
	}
	sent := trafficOf(t, fixture, teams.KindDirective)
	if len(sent) != 1 || sent[0].Turn != "msg-old" || sent[0].Run != "run-old" {
		t.Fatalf("traffic %+v", sent)
	}
	// A call outside any turn stamps nothing: unknown, and still listed.
	if said, failed, err := manager.teamSendTool(context.Background(), json.RawMessage(`{"to":"web","text":"no turn","kind":"note"}`)); failed || err != nil {
		t.Fatalf("%q %v", said, err)
	}
	if last := threadLog(t, fixture); last[len(last)-1].Provenance != (teams.Provenance{}) {
		t.Errorf("a call with no turn was stamped: %+v", last[len(last)-1])
	}
}

// THE OPENER IS EXACT OR IT IS EMPTY.
func TestOpeningMessageIsExactOrEmpty(t *testing.T) {
	person := ensureUserIdentity(userText("hello"))
	id := person.person.Composed.MessageID
	for name, test := range map[string]struct {
		user userMessage
		want string
	}{
		"a person's message":                   {person, id},
		"a message before ids existed":         {userText("old"), ""},
		"the session's own note":               {ensureUserIdentity(userMessage{message: textMessage("user", "n"), authored: true}), ""},
		"a woken turn":                         {userMessage{wake: true}, ""},
		"a woken turn answering one line":      {userMessage{wake: true, answersPerson: true, answersMessage: "msg-one"}, "msg-one"},
		"answersMessage on a turn nobody woke": {userMessage{answersMessage: "msg-one"}, ""},
		"a resumed opening":                    {func() userMessage { u := person; u.resumed = true; return u }(), ""},
	} {
		if got := openingMessage(test.user); got != test.want {
			t.Errorf("%s: %q, want %q", name, got, test.want)
		}
	}
}

// A WAKE ANSWERS A PERSON'S LINE ONLY WHEN IT WAS THE WHOLE DRAIN. Two lines,
// or a line beside a task's note, leave the wake with no message to name.
func TestDrainNamesTheOneLineItLandedAndNoOtherCombination(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	drain := func(messages ...userMessage) queueDrain {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		hub := agent.newReplayHubLocked()
		agent.steering = messages
		return agent.drainQueueLocked(hub, false)
	}
	one := drain(userText("just this"))
	if one.only == "" || !one.heard {
		t.Fatalf("one line: %+v", one)
	}
	entries := agent.Transcript()
	if last := entries[len(entries)-1]; last.Person == nil || last.Person.Composed == nil || last.Person.Composed.MessageID != one.only {
		t.Errorf("the drain named %q; the transcript holds %+v", one.only, last.Person)
	}
	if two := drain(userText("a"), userText("b")); two.only != "" || !two.heard {
		t.Errorf("two lines: %+v", two)
	}
	if mixed := drain(userText("a"), wakeNote("a task landed")); mixed.only != "" {
		t.Errorf("a line beside a note: %+v", mixed)
	}
	if note := drain(wakeNote("only a note")); note.only != "" || note.heard {
		t.Errorf("a note alone: %+v", note)
	}
}
