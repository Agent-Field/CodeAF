package session

// THE FOURTH MOMENT: a turn priced while it is running.
//
// The three that existed before all decide with no evidence in front of them —
// the prompt's law, the route judge's read of a request, and a proposal the model
// remembers to make. This one reads the only thing that is a fact mid-turn: what
// the answer has cost so far, in finished tool rounds, against what handing it
// over costs.
//
// So these tests pin the four things that could quietly stop being true: that the
// meter fires at the marks and nowhere else, that the question reaches the model
// once per mark and reads as a question about any kind of work at all, that a
// ceiling really does end the turn with the person's own words on the one task it
// starts, and that the turns which must never be checkpointed never are.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the meter ───────────────────────────────────────────────────────────────

// THE MARKS ARE WHERE THE POLICY SAYS THEY ARE, AND NOTHING FIRES BETWEEN THEM.
//
// This is the deterministic half of the whole mechanism: no model, no content, no
// clock. A counter and two constants decide, so a drift in either is visible here
// before it is visible anywhere a person could be interrupted by it.
func TestTheMeterFiresOnlyAtTheGeometricMarks(t *testing.T) {
	want := map[int]int{}
	at := checkpointPrice
	for mark := 1; mark <= checkpointMarks; mark++ {
		want[at] = mark
		at *= checkpointRatio
	}
	if len(want) != checkpointMarks {
		t.Fatalf("two marks share a round, so the ladder is not geometric: %v", want)
	}

	meter := &checkpointMeter{}
	// Well past the last mark, because the bound is the point: a turn that runs
	// for ever must not be interrupted for ever.
	last := at * checkpointRatio
	for round := 1; round <= last; round++ {
		got := meter.round()
		if expected, marked := want[round]; marked {
			if got != expected {
				t.Fatalf("round %d gave mark %d, want %d", round, got, expected)
			}
			continue
		}
		if got != 0 {
			t.Fatalf("round %d gave mark %d, but no mark stands there", round, got)
		}
	}
	if meter.rounds != last {
		t.Errorf("the meter counted %d rounds of %d", meter.rounds, last)
	}
	if meter.marks != checkpointMarks {
		t.Errorf("the meter climbed to %d of %d marks", meter.marks, checkpointMarks)
	}
}

// THE PRICE IS A FLOOR AND THE LADDER IS SHORT. Both are policy rather than
// arithmetic, and both are the kind of number somebody tunes without reading the
// argument for it, so they are pinned to the band the design was chosen in.
func TestTheHandoffPriceStaysAFloorAndTheLadderStaysShort(t *testing.T) {
	if checkpointPrice < 8 || checkpointPrice > 12 {
		t.Errorf("the handoff price is %d rounds; the fixed cost it is counted off — the ten git "+
			"commands that open and close a task's worktree — puts it between 8 and 12", checkpointPrice)
	}
	if checkpointRatio < 2 {
		t.Errorf("the ratio is %d, so the marks do not get dearer and the harness measures its own "+
			"impatience rather than the turn's cost", checkpointRatio)
	}
	if checkpointMarks != 3 {
		t.Errorf("a turn gets %d marks; two questions and a ceiling is the whole design, and a third "+
			"question is the harness talking to itself (looped.go reached the same number)", checkpointMarks)
	}
}

// ── the question the model is asked ─────────────────────────────────────────

// THE CHECKPOINT ASKS A JUDGEMENT, NOT A THRESHOLD.
//
// This is task_escalation_test.go's pin on prompts/system.md, applied to the
// sentence that says the same thing at the moment it is needed. aforge is a
// general harness: a research sweep, a writing project and a mechanical change
// are one shape of problem to this question, and the moment it says "after N
// rounds" or reaches for a worked example about files it stops being true for two
// of the three.
func TestTheCheckpointStaysGeneralAndNamesNoThreshold(t *testing.T) {
	if strings.ContainsAny(checkpointNote, "0123456789") {
		t.Errorf("the checkpoint carries a number, so it reads as a threshold the model can count "+
			"toward rather than a question about the work:\n%s", checkpointNote)
	}
	for _, narrow := range []string{
		"file", "code", "repo", "test", "commit", "function", "package",
	} {
		if strings.Contains(strings.ToLower(checkpointNote), narrow) {
			t.Errorf("the checkpoint says %q, which narrows a general law to coding work:\n%s",
				narrow, checkpointNote)
		}
	}
	// AND IT NAMES THE HAND. The measured failure is a model that has propose_task
	// and does not reach for it, so a checkpoint that only said "hand it over"
	// would be the prompt's own advice repeated at a model that has already not
	// taken it.
	if !strings.Contains(checkpointNote, "propose_task") {
		t.Errorf("the checkpoint never names the hand it wants used:\n%s", checkpointNote)
	}
	// AND IT ASKS FOR THE DOWRY, which is the half of the law that stops a handoff
	// throwing the turn's findings away.
	if !strings.Contains(checkpointNote, "cannot see any of this") {
		t.Errorf("the checkpoint never says the taker cannot see this turn, so a handoff has no "+
			"reason to carry what it found:\n%s", checkpointNote)
	}
}

// AND THE LINE A PERSON READS IS THAT LINE.
//
// Pinned as an exact string rather than as a shape, exactly as the mid-answer
// handoff line is: somebody reads this on a turn they did not ask to be
// interrupted on, and the wording IS the feature.
func TestTheCeilingLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "this is running long · moving it to a task that is watched and can split"
	if checkpointCeilingNote != want {
		t.Fatalf("the ceiling line reads %q, want %q", checkpointCeilingNote, want)
	}
	if plain := plainWords(checkpointCeilingNote); plain != checkpointCeilingNote {
		t.Errorf("the line carries machinery vocabulary; plainly it would read %q", plain)
	}
	if strings.Contains(checkpointCeilingNote, "\n") {
		t.Error("the line is more than one line")
	}
	if checkpointCeilingNote != strings.ToLower(checkpointCeilingNote) {
		t.Errorf("the line is not lowercase: %q", checkpointCeilingNote)
	}
	if strings.HasSuffix(checkpointCeilingNote, ".") {
		t.Errorf("the line ends in a full stop, which makes a remark into an announcement: %q",
			checkpointCeilingNote)
	}
	if !strings.Contains(checkpointCeilingNote, " · ") {
		t.Errorf("the line has no middle dot, so it is not the observation-then-promise the surface "+
			"already speaks in: %q", checkpointCeilingNote)
	}
}

// ── a turn that is checkpointed ─────────────────────────────────────────────

// grindingSteps answers every request with one more tool call — a different path
// each time, so the loop detector has nothing to say about it — and answers the
// handoff ask with the brief it is given.
//
// It is a script rather than a stub so that the transcript the checkpoint lands
// in is the real one: the note has to survive a drain, a request and a batch to
// count as having reached the model.
func grindingSteps(count int, brief string) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForHandoff(messages) {
				return textResponse(brief), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			return toolResponse(fmt.Sprintf("call-%d", round), "ls", string(arguments)), nil
		}
	}
	return steps
}

// THE HANDOFF BRIEF IS NOT SPOKEN INTO THE ROOM.
//
// It is a worker's instruction, not a word to the person, and the turn it is
// written at the end of has a stream observer installed that types deltas into
// the room in the assistant's voice. Left on that stream the brief would paint
// itself over the top of the answer it is ending, which is the fault
// [provider.WithoutStream] exists to prevent everywhere else in this package.
func TestTheHandoffBriefIsNotStreamedIntoTheRoom(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	steps := grindingSteps(rounds+2, "Finish it\nwhat is left")
	streamed := make(chan bool, 4)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForHandoff(messages) {
				streamed <- provider.Streaming(ctx)
			}
			return inner(ctx, messages)
		}
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: steps}, func(config *Config) {
		config.AskConsent = true
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	select {
	case on := <-streamed:
		if on {
			t.Error("the handoff brief was asked for on the turn's own stream, so it types itself " +
				"into the room in the model's voice")
		}
	default:
		t.Fatal("the ceiling never asked for a handoff brief")
	}
}

// askedForHandoff reports whether this request ends on the ceiling's ask.
func askedForHandoff(messages []ai.Message) bool {
	if len(messages) == 0 {
		return false
	}
	return strings.Contains(messageText(messages[len(messages)-1]), "[handing over]")
}

// checkpointsInTranscript counts the checkpoints that actually reached the
// transcript, which is where the next request is assembled from.
func checkpointsInTranscript(a *Agent) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := 0
	for _, message := range a.messages {
		if strings.Contains(messageText(message), checkpointNote) {
			seen++
		}
	}
	return seen
}

// checkpointsInRequest counts them in one request the model was actually sent.
func checkpointsInRequest(messages []ai.Message) int {
	seen := 0
	for _, message := range messages {
		if strings.Contains(messageText(message), checkpointNote) {
			seen++
		}
	}
	return seen
}

func noticeTexts(events []Event) []string {
	var said []string
	for _, event := range events {
		if event.Kind == EventNotice {
			said = append(said, event.Text)
		}
	}
	return said
}

func saidSomething(said []string, want string) bool {
	for _, line := range said {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// ONE CHECKPOINT PER MARK, IN FRONT OF THE MODEL, AND NOT ONE BETWEEN.
//
// The turn here runs past the first mark and stops short of the second, which is
// the case that matters most: a model that answers "this is one job" and carries
// on must not be asked again on its very next step, because a harness that
// climbed faster than its own question could be read would be measuring latency
// rather than cost.
func TestOneCheckpointReachesTheModelAtTheFirstMarkAndNotAgainBeforeTheSecond(t *testing.T) {
	// Every round up to one short of the second mark: the first mark fires, the
	// second does not.
	rounds := checkpointMarkAt(2) - 1
	completer := &scriptedCompleter{steps: append(grindingSteps(rounds, ""), finalText("done"))}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if seen := checkpointsInTranscript(agent); seen != 1 {
		t.Fatalf("the turn carried %d checkpoints over %d rounds; one mark stands at %d and the next "+
			"at %d, so exactly one belongs here", seen, rounds, checkpointMarkAt(1), checkpointMarkAt(2))
	}
	// AND IT REACHED THE WIRE. A note in the transcript that no request carried is
	// a note the model never read.
	last := completer.request(completer.requests() - 1)
	if seen := checkpointsInRequest(last); seen != 1 {
		t.Fatalf("the last request carried the checkpoint %d times, want once", seen)
	}
	// The mark fires at the price and not before it: a request assembled one round
	// earlier cannot have carried it.
	early := completer.request(checkpointMarkAt(1) - 1)
	if seen := checkpointsInRequest(early); seen != 0 {
		t.Errorf("a request assembled before the mark already carried the checkpoint %d times", seen)
	}
}

// AND THE SECOND MARK ASKS AGAIN, ONCE.
func TestTheSecondMarkAsksOnceMore(t *testing.T) {
	rounds := checkpointMarkAt(2)
	completer := &scriptedCompleter{steps: append(grindingSteps(rounds, ""), finalText("done"))}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if seen := checkpointsInTranscript(agent); seen != 2 {
		t.Fatalf("the turn carried %d checkpoints over %d rounds, want 2", seen, rounds)
	}
}

// A MODEL THAT ANSWERS "SEVERAL PARTS" GOES DOWN THE ROAD THAT ALREADY EXISTS.
//
// The checkpoint adds no door: the model reaches for the hand it already has, the
// proposal is offered the way every proposal is offered, and — because the
// handoff came out of work already done — it draws the mid-answer line the
// escalation lane already writes ([taskEscalationNote]). Nothing here is new
// except the reason the model looked up.
func TestAnsweringSeveralPartsProposesThroughTheExistingRoadWithTheEscalationLine(t *testing.T) {
	steps := grindingSteps(checkpointMarkAt(1), "")
	steps = append(steps,
		proposeCall("Finish the four pieces", "what the turn already found, part by part"),
		finalText("handed over"))
	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	if seen := checkpointsInTranscript(agent); seen != 1 {
		t.Errorf("the turn carried %d checkpoints, want the one that prompted the handoff", seen)
	}
	if count := admitted(graph); count != 1 {
		t.Errorf("%d tasks were admitted, want exactly one", count)
	}
	if !saidSomething(noticeTexts(collected), taskEscalationNote) {
		t.Errorf("the handoff never drew the mid-answer line; notices were %q",
			noticeTexts(collected))
	}
	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Error("the ceiling line was drawn on a turn that handed itself over before the ceiling")
	}
	if !strings.Contains(node.spec.brief, "what the turn already found") {
		t.Errorf("the worker's brief lost the findings: %q", node.spec.brief)
	}
	waitDoneNode(t, node)
}

// ── the ceiling ─────────────────────────────────────────────────────────────

// AT THE CEILING THE HARNESS STOPS ASKING.
//
// The guarantee this whole file exists to make: past the last mark the turn ends,
// exactly one task carries the work, the person's own sentence rides it verbatim,
// and the line they read is that line. Everything the model said about carrying
// on is over — it was asked twice.
func TestAtTheCeilingTheTurnEndsAndTheWorkMovesToOneWatchedTask(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const brief = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	rounds := checkpointMarkAt(checkpointMarks)
	// One step past the ceiling, so a turn that failed to stop would be visible as
	// a turn that kept calling tools rather than as a turn that ran out of script.
	completer := &scriptedCompleter{steps: grindingSteps(rounds+4, brief)}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	// The node is recorded and left running: a node that LANDS posts its report
	// into the session, which wakes a turn of its own, and this test is about the
	// turn that just ended.
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// EXACTLY ONE TASK, on the one road.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at the ceiling, want exactly one", count)
	}
	// THE PERSON'S ASK RIDES IT VERBATIM. It is the one thing on this road no
	// writer may touch: the brief is the model's, the request is theirs.
	if node.spec.request != asked {
		t.Errorf("the task's request is %q, want the person's own words %q", node.spec.request, asked)
	}
	if !strings.Contains(node.instruction(), asked) {
		t.Errorf("the document the worker reads does not carry the person's words:\n%s", node.instruction())
	}
	// AND THE DOWRY WITH IT.
	if !strings.Contains(node.spec.brief, "everything this turn already found out") {
		t.Errorf("the task lost what the turn learned: %q", node.spec.brief)
	}
	// ARMED TO SPLIT, which is what the line promises.
	if !node.spec.wide {
		t.Error("the ceiling's task is not armed to split, so the line promises something it did not do")
	}
	// THE LINE, EXACTLY.
	if !saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Fatalf("the ceiling never said its line; notices were %q", noticeTexts(collected))
	}
	// THE TURN IS OVER, and the transcript is not left with a question nobody
	// answered — the next turn would open on it and answer it.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointCeilingNote) {
		t.Errorf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	// AND IT STOPPED WHERE IT SAID IT WOULD. The script had four more rounds left
	// in it; what stands past the ceiling is the handoff brief and the errand that
	// names the session (title.go), and neither of those is another tool round.
	if completer.requests() > rounds+2 {
		t.Errorf("the turn made %d requests past a ceiling standing at %d rounds", completer.requests(), rounds)
	}
	// THE MODEL WAS ASKED TWICE BEFORE THE HARNESS DECIDED, and no more.
	if seen := checkpointsInTranscript(agent); seen != checkpointMarks-1 {
		t.Errorf("the model was asked %d times before the ceiling, want %d", seen, checkpointMarks-1)
	}
}

// AND THE CEILING STILL HANDS THE WORK OVER WHEN THE BRIEF CANNOT BE WRITTEN.
//
// A provider fault, an empty reply, a model that answers with nothing: the dowry
// is lost and the work is not. The task starts on the person's own sentence,
// which is [unshaped]'s answer to the same failure at the typed door — a task
// that could not start at all would be the guarantee broken.
func TestTheCeilingHandsOverEvenWhenNobodyCanWriteTheBrief(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	rounds := checkpointMarkAt(checkpointMarks)
	completer := &scriptedCompleter{steps: grindingSteps(rounds+2, "   ")}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
	if node.spec.brief != asked {
		t.Errorf("with no brief written the task should run on the person's own words; it has %q",
			node.spec.brief)
	}
}

// ── the turns that are never checkpointed ───────────────────────────────────

// A NODE, A SCREENLESS SESSION AND THE SESSION'S OWN VOICE ARE ALL LEFT ALONE.
//
// Each for its own reason, and each of them is a turn that would be made worse by
// a ceiling: a node already runs under a step cap, a deadline and a checker; a
// session with nobody watching has no one to read the line; and a turn the
// session started for itself is the session spending money on its own sentence.
func TestTheTurnsThatMustNeverBeCheckpointedAreNot(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks) + 2

	for _, shape := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"a node", func(config *Config) { config.AskConsent = true; config.InTask = true }},
		{"a session nobody is watching", func(config *Config) { config.AskConsent = false }},
	} {
		t.Run(shape.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: append(grindingSteps(rounds, ""), finalText("done"))}
			agent, _ := newTestAgent(t, completer, shape.mutate)
			graph := stubbedGraph(agent, func(node *TaskNode) {
				node.finish("done", nil, "", "")
				node.graph.complete(node, TaskDone)
			})

			events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)

			if seen := checkpointsInTranscript(agent); seen != 0 {
				t.Errorf("%s was checkpointed %d times", shape.name, seen)
			}
			if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
				t.Errorf("%s hit the ceiling", shape.name)
			}
			if count := admitted(graph); count != 0 {
				t.Errorf("%s had %d tasks started over the top of it", shape.name, count)
			}
			// The turn ran to the end of its own script rather than being stopped.
			if completer.requests() <= rounds {
				t.Errorf("%s made only %d requests of %d, so something ended it early",
					shape.name, completer.requests(), rounds)
			}
		})
	}
}

// AND A TURN THE PERSON INTERRUPTS IS NOT CHECKPOINTED ON ITS WAY OUT.
//
// The interrupt is the person saying they do not want this; answering it with a
// task on the rail would be the harness having the last word.
func TestAnInterruptedTurnIsNotCheckpointed(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks) + 2
	steps := grindingSteps(rounds, "")
	// The interrupt lands as the ceiling's round is being answered, so the turn
	// reaches the checkpoint seam with a context that is already over.
	var agent *Agent
	steps[checkpointMarkAt(checkpointMarks)-1] = func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
		agent.Interrupt()
		return toolResponse("call-cut", "ls", `{"path":"."}`), nil
	}
	completer := &scriptedCompleter{steps: steps}
	agent, _ = newTestAgent(t, completer, func(config *Config) { config.AskConsent = true })
	graph := stubbedGraph(agent, func(node *TaskNode) {
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "work through the four things I listed and report back")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if saidSomething(noticeTexts(collected), checkpointCeilingNote) {
		t.Error("an interrupted turn was moved onto the rail")
	}
	if count := admitted(graph); count != 0 {
		t.Errorf("%d tasks were started out of an interrupted turn", count)
	}
	// And the turn ended where the interrupt landed rather than running on: the
	// stream closing is the turn being over ([collect]), and the script had rounds
	// left in it.
	if completer.requests() > checkpointMarkAt(checkpointMarks)+1 {
		t.Errorf("the turn made %d requests after an interrupt at round %d",
			completer.requests(), checkpointMarkAt(checkpointMarks))
	}
}
