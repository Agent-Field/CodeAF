package session

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/manual"
)

// heard records text as the person's newest message, where a turn they open
// or steer records it ([Agent.rememberAskLocked]).
func heard(agent *Agent, text string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.rememberAskLocked(userText(text))
}

// programConversation is a conversation that carries a program called
// senior-dev, with the run road linked, which is where a `via` can be
// honoured and so where the person naming it is read. The engine is a double
// nothing here starts.
func programConversation(t *testing.T, mutate func(*Config)) *Agent {
	t.Helper()
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Delegates = testPrograms("senior-dev")
		if mutate != nil {
			mutate(config)
		}
	})
	return agent
}

// proposeOutputs is every propose_task result a turn showed, in order, clean
// or refused.
func proposeOutputs(events []Event) []string {
	var outputs []string
	for _, event := range events {
		if event.Tool == "propose_task" && (event.Kind == EventToolEnd || event.Kind == EventToolFailed) {
			outputs = append(outputs, event.Output)
		}
	}
	return outputs
}

// A PROPOSAL THAT LEAVES OUT THE PROGRAM THE PERSON NAMED IS TURNED BACK ONCE.
// The person asked for senior-dev and the model proposed the work for its own
// worker, which is the proposal a model writes by habit: it is told who was
// named and both ways to answer. The next proposal for the same message, made
// after the model has read that, passes as it is, because "don't use
// senior-dev" is an ask too, and only the model can read which one it was.
func TestAProposalLeavingOutTheProgramThePersonNamedIsTurnedBackOnce(t *testing.T) {
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	completer := &routedCompleter{parent: []step{
		proposeCall("Fix the dropped retries", "the scheduler drops retries under load"),
		proposeCall("Fix the dropped retries", "the scheduler drops retries under load"),
		finalText("started"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Delegates = testPrograms("senior-dev")
	})
	nodes := make(ranNodes, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) { nodes <- node })

	collected := collect(t, mustSubmit(t, agent, "the scheduler drops retries under load; fix it with senior-dev"))

	outputs := proposeOutputs(collected)
	if len(outputs) != 2 {
		t.Fatalf("want two proposal results, got %d: %q", len(outputs), outputs)
	}
	if want := programNamedSentence("senior-dev"); outputs[0] != want {
		t.Fatalf("the first proposal read %q, want %q", outputs[0], want)
	}
	for _, want := range []string{"the person named senior-dev", "`via: \"senior-dev\"`", "propose it again unchanged"} {
		if !strings.Contains(outputs[0], want) {
			t.Fatalf("the bounce does not say %q: %q", want, outputs[0])
		}
	}
	if !strings.HasPrefix(outputs[1], "task ") {
		t.Fatalf("the second proposal for the same message was not let through: %q", outputs[1])
	}
	nodes.await(t)
	if admitted(graph) != 1 {
		t.Fatalf("the graph admitted %d nodes, want the one second proposal", admitted(graph))
	}
}

// proposalsInOneMessage is the model sending several proposals in one message,
// which is how the hand-off page tells it to work in parallel. Each call has an
// id of its own, as a provider's calls in one message do.
func proposalsInOneMessage(titles ...string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		calls := make([]ai.ToolCall, len(titles))
		for index, title := range titles {
			arguments, _ := json.Marshal(taskArguments{
				Title:       title,
				Summary:     "two lines the person reads",
				Brief:       title + "\n" + taskBriefMark,
				Deliverable: "the fix, on the branch the run leaves",
				Acceptance:  "the issue's own reproduction passes",
			})
			calls[index] = ai.ToolCall{
				ID:       "call-task-" + strconv.Itoa(index),
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "propose_task", Arguments: string(arguments)},
			}
		}
		return callsResponse(calls...), nil
	}
}

// EVERY PROPOSAL OF ONE MESSAGE IS TURNED BACK. "fix issues #31 and #32 with
// senior-dev" is two proposals in one message, sent together before the model
// has read either result. Only the first used to be turned back: the second
// passed as though the model had read the bounce, went up with no `via`, and
// its countdown admitted it to codeaf's own worker against the person's ask.
// Both are turned back, and the proposals of the message the model writes
// after reading them pass as they are.
func TestEveryProposalOfOneMessageIsTurnedBack(t *testing.T) {
	registerBeltRunEngine(t, newBeltRunDouble("unused"))
	completer := &routedCompleter{parent: []step{
		proposalsInOneMessage("Fix issue #31", "Fix issue #32"),
		proposalsInOneMessage("Fix issue #31", "Fix issue #32"),
		finalText("started"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Delegates = testPrograms("senior-dev")
	})
	nodes := make(ranNodes, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) { nodes <- node })

	outputs := proposeOutputs(collect(t, mustSubmit(t, agent, "fix issues #31 and #32 with senior-dev")))

	if len(outputs) != 4 {
		t.Fatalf("want four proposal results, got %d: %q", len(outputs), outputs)
	}
	for _, output := range outputs[:2] {
		if output != programNamedSentence("senior-dev") {
			t.Fatalf("a proposal sent beside the one turned back read %q, want the bounce too: %q", output, outputs[:2])
		}
	}
	for _, output := range outputs[2:] {
		if !strings.HasPrefix(output, "task ") {
			t.Fatalf("a proposal written after the bounce was read was not let through: %q", output)
		}
	}
	nodes.await(t)
	nodes.await(t)
	if admitted(graph) != 2 {
		t.Fatalf("the graph admitted %d nodes, want the two proposals of the second message", admitted(graph))
	}
}

// THE NAME IS HEARD HOWEVER A PERSON TYPES IT: as its command, in any case,
// with a space or nothing where it has a hyphen. And a word that only shares
// part of it is not the name.
func TestTheProgramIsHeardHoweverThePersonSpellsIt(t *testing.T) {
	config := Config{Delegates: testPrograms("senior-dev")}
	for _, asked := range []string{
		"/senior-dev fix the flaky retry",
		"fix the flaky retry with senior dev",
		"have Senior-Dev fix the flaky retry",
		"SENIORDEV should take this one",
		"use senior_dev for it",
		"senior-dev",
	} {
		if got := config.programNamedIn(asked); got != "senior-dev" {
			t.Errorf("%q named %q, want senior-dev", asked, got)
		}
	}
	for _, asked := range []string{
		"fix the flaky retry",
		"a senior developer wrote this",
		"ask a senior about the dev branch",
		"dev senior",
		"",
	} {
		if got := config.programNamedIn(asked); got != "" {
			t.Errorf("%q named %q, want nothing", asked, got)
		}
	}
	for _, asked := range []string{"/senior-dev fix the retry", "fix the retry with senior dev", "Senior-Dev, fix the retry"} {
		agent := programConversation(t, nil)
		heard(agent, asked)
		if bounce := agent.programAskBounce(taskSpec{title: "t"}).text(); bounce != programNamedSentence("senior-dev") {
			t.Errorf("%q: the proposal without via read %q, want the bounce", asked, bounce)
		}
	}
}

// NO BOUNCE WHERE NOTHING WAS NAMED, OR WHERE A `via` COULD NOT BE HONOURED.
// A message that names no program is proposed as it always was; a task node,
// a build with no run road and a build carrying no program would refuse the
// `via` the bounce asks for, so a bounce there is a round trip to nowhere.
// And a proposal that already names a program is never turned back.
func TestNoBounceWhereNothingWasNamedOrNoProgramCouldBe(t *testing.T) {
	plain := programConversation(t, nil)
	heard(plain, "the scheduler drops retries under load; fix it")
	if bounce := plain.programAskBounce(taskSpec{}).text(); bounce != "" {
		t.Fatalf("a message naming no program was bounced: %q", bounce)
	}

	named := "the scheduler drops retries under load; fix it with senior-dev"
	inTask := programConversation(t, func(config *Config) { config.InTask = true })
	heard(inTask, named)
	if bounce := inTask.programAskBounce(taskSpec{}).text(); bounce != "" {
		t.Fatalf("a task node was bounced toward a program it cannot name: %q", bounce)
	}

	noPrograms := programConversation(t, func(config *Config) { config.Delegates = nil })
	heard(noPrograms, named)
	if bounce := noPrograms.programAskBounce(taskSpec{}).text(); bounce != "" {
		t.Fatalf("a build carrying no program was bounced: %q", bounce)
	}

	withVia := programConversation(t, nil)
	heard(withVia, named)
	if bounce := withVia.programAskBounce(taskSpec{via: "senior-dev"}).text(); bounce != "" {
		t.Fatalf("a proposal naming the program was bounced: %q", bounce)
	}

	registerBeltRunEngine(t, nil)
	noRoad, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Delegates = testPrograms("senior-dev") })
	heard(noRoad, named)
	if bounce := noRoad.programAskBounce(taskSpec{}).text(); bounce != "" {
		t.Fatalf("a build with no run road was bounced: %q", bounce)
	}
}

// modelReadTheResults is the turn sending its next request, which is the one
// moment a model reads what its last batch returned ([episode.decisionBegins]).
func modelReadTheResults(agent *Agent) { agent.stepSeq.Add(1) }

// refusedWith is what a proposal of spec reads back from the door refusals,
// and "" when none of them turns it around.
func refusedWith(agent *Agent, spec taskSpec) string {
	refusal := agent.refuseProposedTask(spec)
	if refusal == nil {
		return ""
	}
	text, _, _ := refusal.Commit(context.Background())
	return text
}

// ONCE IS PER MESSAGE. A second message of the person's that names the program
// again is a new ask and earns its own bounce; a proposal for the same message
// made after the model has read the bounce does not.
func TestTheBounceIsOncePerMessageOfThePersons(t *testing.T) {
	agent := programConversation(t, nil)
	heard(agent, "fix the flaky retry with senior-dev")
	if agent.programAskBounce(taskSpec{}) == nil {
		t.Fatal("the first proposal for the message was not bounced")
	}
	modelReadTheResults(agent)
	if bounce := agent.programAskBounce(taskSpec{}).text(); bounce != "" {
		t.Fatalf("the proposal made after the bounce was read was bounced again: %q", bounce)
	}
	heard(agent, "no really, give it to senior-dev")
	if agent.programAskBounce(taskSpec{}) == nil {
		t.Fatal("a new message naming the program again was not bounced")
	}
}

// AND NOT BEFORE THE MODEL HAS READ IT. Every proposal of one message is
// staged before any of their results is read, so a second proposal from the
// same step is turned back beside the first rather than passing as though the
// bounce had been read. And a bounce withdrawn before it went ahead (the reply
// carrying it was cut) was never read at all: it takes its mark back, and the
// next proposal is turned back in its place.
func TestTheBounceIsNotSpentUntilTheModelHasReadIt(t *testing.T) {
	agent := programConversation(t, nil)
	heard(agent, "fix issues #31 and #32 with senior-dev")
	first, second := agent.programAskBounce(taskSpec{}), agent.programAskBounce(taskSpec{})
	if first == nil || second == nil {
		t.Fatalf("two proposals of one step were not both bounced: %q, %q", first.text(), second.text())
	}

	first.Withdraw()
	second.Withdraw()
	modelReadTheResults(agent)
	if agent.programAskBounce(taskSpec{}) == nil {
		t.Fatal("the proposal after a withdrawn bounce passed, and the model never read that bounce")
	}

	withdrawnLate := programConversation(t, nil)
	heard(withdrawnLate, "fix issues #31 and #32 with senior-dev")
	first, second = withdrawnLate.programAskBounce(taskSpec{}), withdrawnLate.programAskBounce(taskSpec{})
	second.Withdraw()
	first.Withdraw()
	modelReadTheResults(withdrawnLate)
	if withdrawnLate.programAskBounce(taskSpec{}) == nil {
		t.Fatal("siblings withdrawn in the other order left a mark behind")
	}

	arguments, _ := json.Marshal(taskArguments{Title: "t", Summary: "s", Brief: "b", Deliverable: "d", Acceptance: "a"})
	throughTheDoor := programConversation(t, nil)
	heard(throughTheDoor, "fix issues #31 and #32 with senior-dev")
	hold := bare.NewHold()
	hold.Withdraw()
	if _, _, err := throughTheDoor.proposeTask(bare.WithHold(context.Background(), hold), arguments); err != nil {
		t.Fatalf("the withdrawn proposal errored the turn: %v", err)
	}
	modelReadTheResults(throughTheDoor)
	if bounce := refusedWith(throughTheDoor, taskSpec{}); bounce != programNamedSentence("senior-dev") {
		t.Fatalf("after a proposal withdrawn from the door, the next one read %q, want the bounce", bounce)
	}
}

// AN ASK FOR A PROGRAM IS NEVER TOO SMALL. "fix this file with senior-dev" is
// on the spawn floor as a one-file fix, and the floor ran before `via` was
// read, so the person who asked for senior-dev by name was refused with "do it
// here". The proposal that names the program they asked for passes the floor;
// one that leaves it out is bounced once and then meets the floor as any
// proposal does; and a program the person did not ask for lifts nothing.
func TestAnAskForAProgramIsNeverTooSmall(t *testing.T) {
	asked := "fix this file with senior-dev"
	if !trivialAsk(asked) {
		t.Fatalf("%q is off the floor, so this test would prove nothing", asked)
	}
	agent := programConversation(t, nil)
	heard(agent, asked)
	if refusal := refusedWith(agent, taskSpec{via: "senior-dev"}); refusal != "" {
		t.Fatalf("the proposal naming the program the person asked for was refused: %q", refusal)
	}
	if refusal := refusedWith(agent, taskSpec{}); refusal != programNamedSentence("senior-dev") {
		t.Fatalf("the proposal leaving the program out read %q, want the bounce", refusal)
	}
	modelReadTheResults(agent)
	if refusal := refusedWith(agent, taskSpec{}); refusal != spawnFloorRefusal {
		t.Fatalf("the second proposal leaving the program out read %q, want the floor", refusal)
	}

	unasked := programConversation(t, nil)
	heard(unasked, "fix this file")
	if refusal := refusedWith(unasked, taskSpec{via: "senior-dev"}); refusal != spawnFloorRefusal {
		t.Fatalf("a program nobody asked for lifted the floor: %q", refusal)
	}
	if refusal := refusedWith(unasked, taskSpec{via: "nosuch"}); refusal != spawnFloorRefusal {
		t.Fatalf("a program this build does not carry lifted the floor: %q", refusal)
	}
}

// THE PROGRAMS PAGE QUOTES THE BOUNCE AS THE MODEL READS IT, so a person
// asking why their proposal came back, and the chat answering from the page,
// both read the sentence that was actually sent.
func TestTheProgramsPageQuotesTheBounceWordForWord(t *testing.T) {
	page, found := manual.Chat().Page("delegates")
	if !found {
		t.Fatal("there is no chat manual page called delegates")
	}
	if want := programNamedSentence("senior-dev"); !strings.Contains(page, want) {
		t.Fatalf("the programs page does not quote the bounce %q", want)
	}
}

// AND THE WHOLE DOOR SAYS IT: a proposal called for a one-file fix the person
// gave to senior-dev is refused with the bounce, and never with the floor's
// "do it here", which is what the person was told before.
func TestTheDoorBouncesARequestedProgramBeforeTheFloor(t *testing.T) {
	agent := programConversation(t, nil)
	heard(agent, "fix this file with senior-dev")
	arguments, _ := json.Marshal(taskArguments{Title: "t", Summary: "s", Brief: "b", Deliverable: "d", Acceptance: "a"})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil {
		t.Fatalf("proposeTask errored the turn: %v", err)
	}
	if !isError || result != programNamedSentence("senior-dev") {
		t.Fatalf("the proposal read %q (error %v), want the bounce", result, isError)
	}
}
