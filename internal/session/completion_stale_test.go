package session

// WHO OWNS THE DECISION THAT A REQUEST IS FINISHED (calibration handoff-before
// cell 004-revision-midwork-aforge, and handoff-after cell 001 after it).
//
// Both cells are the same conversation: a person asked for a one-minute command
// AND a report, changed the report from Markdown to CSV while the command ran,
// and the model wrote `report.csv`, removed `report.md` and finished. Both cells
// then made a task out of it and both hit the 180-second cap.
//
// The two traces failed through the SAME line for two different reasons, which
// is what these cases pin. In 004 (`transcript.jsonl` lines 33-44) the mark's
// reader drew `B` — one part, no division — the continuation answered
// `NOTHING LEFT TO DO` (`carry rung:draft outcome:nothing-left`), and the drop
// was refused because the drawing was not the word `(done)`; the ceiling wrote
// `seam:write decision:moved carry:ask` and a cold worker was started on the
// person's ORIGINAL sentence, CSV and all. In 001 the reader could not be
// reached at all (`role:markreader message:context canceled`) and the identical
// refusal followed from a reading that never happened.
//
// So the law these cases hold: a drawing REFUSES a completion claim only when it
// says work remains, never merely by failing to agree — and the claim is bounded
// by the turn's own budget rather than by a second reader.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// finishedScript is the trace's own turn with the drawing the case wants: the
// workspace writes that fire the write seam — [writeAllowanceCalls] of them,
// which is the count the seam has — then a conversation that says in words that
// the artifacts are written. The dowry is answered with the remains token, which
// is what the live model answered.
func finishedScript(count int, sketch func() (string, error)) []step {
	var writes atomic.Int64
	steps := make([]step, count)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return sketchResponse(sketch())
			}
			if askedForHandoff(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("Finish the remaining step: verify the report."), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			round := writes.Add(1)
			if round > writeAllowanceCalls {
				return textResponse("report.csv is written and report.md is gone."), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
				Text string `json:"content"`
			}{Path: fmt.Sprintf("report%d.csv", round), Text: "service,port\nkestrel,8431\n"})
			return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
				"Writing the revised report."), nil
		}
	}
	return steps
}

// sketchResponse turns one scripted drawing into what the reader's road expects,
// so a case can say "the reader was down" in the same breath as "it drew this".
func sketchResponse(shape string, err error) (*ai.Response, error) {
	if err != nil {
		return nil, err
	}
	return textResponse(shape), nil
}

// finishedAgent is [awaitingAgent] with a session file, because what these cases
// assert is partly what the file says the seam decided.
func finishedAgent(t *testing.T, completer Completer, path string) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.Interactive = true
		config.SessionFile = path
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
	})
	return agent
}

// grindingFinishedScript is [finishedScript]'s turn that DOES NOT STOP: the
// writes that fire the seam, then an unbroken run of ordinary tool calls. It is
// what a turn looks like when its own claim was wrong, which is the case the
// bound exists for.
func grindingFinishedScript(count int, dowry string, sketch string) []step {
	var writes, ground atomic.Int64
	steps := make([]step, count)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			switch {
			case askedForSketch(messages):
				return textResponse(sketch), nil
			case askedForHandoff(messages):
				return textResponse(dowry), nil
			case askedToWriteHandoff(messages):
				return textResponse("Finish the remaining step: verify the report."), nil
			case askedForRemains(messages):
				return textResponse(checkpointNothingLeft), nil
			}
			if round := writes.Add(1); round <= writeAllowanceCalls {
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
					Text string `json:"content"`
				}{Path: fmt.Sprintf("report%d.csv", round), Text: "service,port\nkestrel,8431\n"})
				return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
					"Writing the revised report."), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", ground.Add(1))})
			return toolResponseWithText(fmt.Sprintf("look-%d", ground.Load()), "ls", string(arguments),
				"Looking at the next path."), nil
		}
	}
	return steps
}

// countTheNamer is [answerTheNamerOffTheQueue] that also COUNTS, because "the
// namer was never asked" is an assertion about money and the only way to make it
// is to watch the errand lane itself.
func countTheNamer(completer *scriptedCompleter) *atomic.Int64 {
	var named atomic.Int64
	completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isNameCall(messages) {
			return nil, false
		}
		named.Add(1)
		return textResponse(""), true
	}
	return &named
}

// handoffWrites counts the requests that reached the mastermind who turns a
// draft into a worker's instruction.
func handoffWrites(completer *scriptedCompleter) int {
	wrote := 0
	for index := range completer.requests() {
		if askedToWriteHandoff(completer.request(index)) {
			wrote++
		}
	}
	return wrote
}

// turnRequests is every request that was the TURN talking, with the sidecar
// errands — the drawing, the dowry, the writer, the end-of-turn reader — taken
// out. The last of them is the request the final answer was written from, which
// is what a case asking "did the model actually see this" has to look at.
func turnRequests(completer *scriptedCompleter) []int {
	var mine []int
	for index := range completer.requests() {
		request := completer.request(index)
		if askedForSketch(request) || askedForHandoff(request) ||
			askedToWriteHandoff(request) || askedForRemains(request) {
			continue
		}
		mine = append(mine, index)
	}
	return mine
}

// requestCarries reports whether one request put these words in front of the
// model, anywhere in the transcript it was sent.
func requestCarries(completer *scriptedCompleter, index int, words string) bool {
	for _, message := range completer.request(index) {
		if strings.Contains(messageText(message), words) {
			return true
		}
	}
	return false
}

// ── the frozen cells, closed ────────────────────────────────────────────────

// A DRAWING THAT SHOWED NO REMAINDER NEVER STOOD IN FOR A READER SAYING WORK WAS
// LEFT — cell 004's own shape.
//
// The reader drew one part and no division; the model, holding the whole
// transcript, said the request was discharged. Nothing in the building says work
// remains, so nothing is handed to anybody: the turn's own answer stands and the
// file records the drop.
func TestADrawingWithNoRemainderInItDoesNotForceAHandover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		return "B\nB is the report the person asked for.", nil
	})}
	agent := finishedAgent(t, completer, path)
	named := countTheNamer(completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// THE ROAD WAS ENTERED, so this cannot pass for a turn that never reached the
	// door the cell failed at.
	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted for a request the model said was discharged", count)
	}
	if saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the person was told work was moving when none did: %q", noticeTexts(collected))
	}
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingNothing)
	}
	// AND THE DROP PAID FOR NEITHER OF THE TWO ERRANDS BELOW IT. A finished
	// request must not buy a name for a task nobody starts, nor a mastermind's
	// instruction for a worker nobody opens: both stand under the decision now
	// (checkpoint.go's [Agent.handOverRunningTurn]), and this is the assertion
	// that keeps them there.
	if got := named.Load(); got != 0 {
		t.Fatalf("the namer was asked %d times for a request that was already finished", got)
	}
	if got := handoffWrites(completer); got != 0 {
		t.Fatalf("the brief writer was asked %d times for a task nobody started", got)
	}
}

// AND A READER NOBODY COULD REACH IS NOT A READER SAYING WORK REMAINS — cell
// 001's own shape.
//
// The mark reader was cancelled and drew nothing whatever. A reading that never
// happened is silence, and silence used to be spent as evidence against the one
// reader that had actually read the work.
func TestAReaderNobodyCouldReachDoesNotForceAHandover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		return "", errors.New("the reader is down")
	})}
	agent := finishedAgent(t, completer, path)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted behind a reader that never answered", count)
	}
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingNothing)
	}
	// AND IT IS WRITTEN DOWN AS A READER THAT FAILED, which is the fact it is:
	// the turn was alive and the reading was not. The case below this one is the
	// same error word under a turn nobody owns, and the file has to tell them
	// apart.
	marks := journaledMarks(t, path)
	if len(marks) != 1 || marks[0].Decision != checkpointDecisionFailed {
		t.Fatalf("the mark journaled %+v, want one %q", marks, checkpointDecisionFailed)
	}
}

// AND A DIRECTION THE PERSON TYPED DURING THE DECISION IS STILL THERE TO BE READ.
//
// The drop is the outcome that KEEPS the turn, so words queued while the seam was
// deciding reach the model at the very next boundary. A handover would have sent
// them nowhere: the worker opens on a brief written before they were typed.
func TestAQueuedDirectionSurvivesTheDrop(t *testing.T) {
	const revision = "one more thing — sort the rows by port"

	var agent *Agent
	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		// The person types WHILE the seam is deciding, which is the window
		// [Agent.awaitGroundNow] exists for on the other road.
		queueDirection(agent, revision)
		return "B\nB is the report the person asked for.", nil
	})}
	agent = awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted while the person's newest words were unread", count)
	}
	// AND THE WORDS WERE PUT TO THE MODEL, which is a different claim from their
	// being in the transcript: text this package appended proves only that this
	// package appended it. So the REQUESTS are read.
	//
	// THE LAST TURN REQUEST IS THE ONE THE FINAL ANSWER WAS WRITTEN FROM, with
	// the sidecar errands taken out ([turnRequests]), and the direction has to be
	// in it — a revision that reached the model only after it had already
	// answered is a revision that reached nobody.
	mine := turnRequests(completer)
	if len(mine) == 0 {
		t.Fatal("the turn made no requests of its own")
	}
	carried := -1
	for _, index := range mine {
		if requestCarries(completer, index, revision) {
			carried = index
			break
		}
	}
	if carried < 0 {
		t.Fatal("the direction typed during the decision was never put to the model")
	}
	if last := mine[len(mine)-1]; !requestCarries(completer, last, revision) {
		t.Fatalf("request %d wrote the final answer without the direction in front of it", last)
	}
	// AND IT ARRIVED BEFORE THE TURN WAS ASKED WHETHER THE ASK WAS FINISHED, so
	// the reading that ends the turn is a reading of the request the person
	// actually has.
	for index := range completer.requests() {
		if askedForRemains(completer.request(index)) && index < carried {
			t.Fatalf("the turn's ending was read at request %d, before the direction landed at %d",
				index, carried)
		}
	}
}

// ── the bound on a believed claim ───────────────────────────────────────────

// ONE ACCEPTED COMPLETION PER REQUEST, AND AGREEMENT BUYS NO SECOND ONE.
//
// This is the case that says what the bound actually is. The drawing here SAYS
// `(done)` — two readers agreeing, which is the strongest evidence this road
// ever has — and the claim is charged all the same, because two readers can be
// wrong at once and that is the path with nobody left to catch it. So the drop
// is granted once; the turn then carries on grinding, disproving itself; and the
// ceiling comes back at [checkpointPrice] more worked rounds and moves the work.
func TestAgreementDoesNotBuyASecondCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: grindingFinishedScript(40,
		checkpointNothingLeft, "(done)\nEverything asked for is written.")}
	agent := finishedAgent(t, completer, path)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	// THE FIRST CLAIM WAS BELIEVED AND THE SECOND WAS NOT.
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 2 {
		t.Fatalf("the seam journaled %d endings, want the drop and the ceiling that met it: %+v",
			len(ceilings), ceilings)
	}
	if ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the first ending was %q, want %q", ceilings[0].Decision, checkpointCeilingNothing)
	}
	if ceilings[1].Decision != checkpointCeilingMoved {
		t.Fatalf("the second ending was %q, want %q", ceilings[1].Decision, checkpointCeilingMoved)
	}
	// AND THE ROUND IT CAME BACK ON IS THE PRICE, not some later rung of the
	// ladder that would have fired anyway.
	if gap := ceilings[1].Rounds - ceilings[0].Rounds; gap > checkpointPrice {
		t.Fatalf("the ceiling came back %d rounds later, want no more than %d", gap, checkpointPrice)
	}
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted for a turn that worked on past its own claim; want one", count)
	}
}

// AND A NEW DIRECTION IS NOT BOUND BY THE CLAIM MADE BEFORE IT.
//
// The bound is one claim per REQUEST, and a person who types something else has
// changed the request. Without that, somebody revising their ask mid-turn would
// be handed to a worker for the sole reason that the turn had already finished
// the sentence before it — which is the failure this road exists to prevent,
// wearing the bound as a disguise.
func TestANewDirectionArrivesWithTheClaimUnspent(t *testing.T) {
	const revision = "one more thing — sort the rows by port"

	path := filepath.Join(t.TempDir(), "session.jsonl")
	var agent *Agent
	var claims atomic.Int64
	completer := &scriptedCompleter{steps: grindingFinishedScript(40,
		checkpointNothingLeft, "(done)\nEverything asked for is written.")}
	// THE DIRECTION IS TYPED THE MOMENT THE FIRST DROP IS TAKEN, which is the
	// window the person is actually in: they watched the turn say it was done and
	// asked for one more thing. The turn then grinds on — disproving its first
	// claim, which is what brings the ceiling back — and STOPS once the second
	// claim has been made, because a turn that ground on past THAT one would be
	// handed over for the reason the case above pins and this one is not about.
	steps := completer.steps
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForHandoff(messages) && claims.Add(1) == 1 {
				queueDirection(agent, revision)
			}
			if claims.Load() >= 2 && !askedForSketch(messages) && !askedForHandoff(messages) &&
				!askedToWriteHandoff(messages) && !askedForRemains(messages) {
				return textResponse("The rows are sorted by port and the report is written."), nil
			}
			return inner(ctx, messages)
		}
	}
	agent = finishedAgent(t, completer, path)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted; a revision inherited the claim spent on the sentence before it", count)
	}
	ceilings := journaledCeilings(t, path)
	if len(ceilings) < 2 {
		t.Fatalf("the seam journaled %d endings, want the drop and the one the revision earned: %+v",
			len(ceilings), ceilings)
	}
	for _, ending := range ceilings {
		if ending.Decision != checkpointCeilingNothing {
			t.Fatalf("an ending was %q, want every one to be %q: %+v",
				ending.Decision, checkpointCeilingNothing, ceilings)
		}
	}
}

// ── and a turn nobody owns any more ─────────────────────────────────────────

// A CANCELLED TURN STARTS NOTHING, AND ITS READER'S ERROR IS NOT A READER'S
// ERROR — handoff-after/001's own line.
//
// The only evidence that cell left was `role:markreader message:context
// canceled`, and it was spent as a reading that failed. The turn had been let go
// of: nothing at the seam owned the request any more, and the road went on to
// pay for a brief and hand the person's original sentence to a cold worker.
// [Agent.launchRouteTask] takes no context, so a task started here outlives the
// turn by construction — which is why the check is at the seam and not below it.
func TestACancelledTurnHandsNothingOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	completer := &scriptedCompleter{steps: finishedScript(12, func() (string, error) {
		// THE TURN IS LET GO OF WHILE THE SEAM IS READING, which is where the
		// frozen cell's cancellation landed.
		stop()
		return "", context.Canceled
	})}
	agent := finishedAgent(t, completer, path)
	named := countTheNamer(completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(ctx, theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted by a turn nobody owned", count)
	}
	// AND NOTHING BELOW THE SEAM WAS EVEN ASKED: no dowry, no name, no brief.
	if got := handoffAsks(completer); got != 0 {
		t.Fatalf("the dowry was asked %d times under a cancelled turn", got)
	}
	if got := handoffWrites(completer); got != 0 {
		t.Fatalf("the brief writer was asked %d times under a cancelled turn", got)
	}
	if got := named.Load(); got != 0 {
		t.Fatalf("the namer was asked %d times under a cancelled turn", got)
	}
	// AND THE FILE SAYS WHICH OF THE TWO IT WAS. `abandoned` is the turn being
	// let go of; `failed` is a reader that could not answer, which the case above
	// pins — one error word, two facts, and an autopsy that could not tell them
	// apart is what made this cell unreadable.
	marks := journaledMarks(t, path)
	if len(marks) != 1 || marks[0].Decision != checkpointDecisionAbandoned {
		t.Fatalf("the mark journaled %+v, want one %q", marks, checkpointDecisionAbandoned)
	}
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingAbandoned {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingAbandoned)
	}
}

func TestCancellationDuringTheHandoffCannotStartATask(t *testing.T) {
	for _, at := range []string{"continuation", "brief writer"} {
		t.Run(at, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(t.TempDir(), "session.jsonl")
			steps := finishedScript(16, func() (string, error) { return "B\nComplete the report.", nil })
			for index, original := range steps {
				steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
					if askedForHandoff(messages) {
						if at == "continuation" {
							cancel()
							return nil, context.Canceled
						}
						return textResponse("Finish the report and check its rows."), nil
					}
					if at == "brief writer" && askedToWriteHandoff(messages) {
						cancel()
						return nil, context.Canceled
					}
					return original(ctx, messages)
				}
			}
			agent := finishedAgent(t, &scriptedCompleter{steps: steps}, path)
			graph := stubbedGraph(agent, func(*TaskNode) {})
			events, err := agent.Submit(ctx, theAsk)
			if err != nil {
				t.Fatal(err)
			}
			collect(t, events)
			if ctx.Err() == nil {
				t.Fatal("the scripted cancellation point was not reached")
			}
			if count := admitted(graph); count != 0 {
				t.Fatalf("cancellation during %s still admitted %d tasks", at, count)
			}
			rows := journaledCeilings(t, path)
			if len(rows) != 1 || rows[0].Decision != checkpointCeilingAbandoned {
				t.Fatalf("the canceled handoff journaled %+v", rows)
			}
		})
	}
}

// ── and a drop leaves this conversation's own work exactly where it was ─────

// A COMPLETION CLAIM IS NOT AN AWAIT CLAIM, AND NEITHER OF THEM STOPS ANYTHING.
//
// The await road (handoff_remainder.go) is the one that was written for a live
// command, and the model reaches it by naming the operation. This case is the
// OTHER answer to the same ask — `NOTHING LEFT TO DO` with a background command
// still running — which now takes the completion drop instead. Nothing about
// that road knows what a job is, so what is asserted here is that it does not
// have to: the command is not stopped, no ending is consumed, and the wake it
// was always going to send starts a turn that answers.
//
// IT IS DRIVEN THROUGH THE REAL LOOP with a real `bash` command held on a file
// nobody has created, exactly as [TestTheAwaitedCommandsEndingWakesTheConversation]
// is, because the claim is about the return path and a planted job proves none
// of it.
func TestACompletionDropLeavesTheRunningCommandOwed(t *testing.T) {
	flag := filepath.Join(t.TempDir(), "release")
	command := "while [ ! -f " + flag + " ]; do sleep 0.02; done; echo built"

	var started, writes atomic.Int64
	steps := make([]step, 24)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			for _, message := range messages {
				if message.Role == "user" && strings.Contains(messageText(message), "job 1 exited 0") {
					return textResponse("The build finished successfully and the report is ready."), nil
				}
			}
			switch {
			case askedForSketch(messages):
				return textResponse("(done)\nThe report the person asked for is written."), nil
			// THE ANSWER THE OLD TESTS NEVER MADE: the model says the request is
			// discharged rather than naming the operation it is waiting on.
			case askedForHandoff(messages):
				return textResponse(checkpointNothingLeft), nil
			case askedToWriteHandoff(messages):
				return textResponse("Finish the remaining step."), nil
			case askedForRemains(messages):
				return textResponse(checkpointNothingLeft), nil
			}
			if started.Add(1) == 1 {
				arguments, _ := json.Marshal(struct {
					Command    string `json:"command"`
					Background bool   `json:"background"`
				}{Command: command, Background: true})
				return toolResponseWithText("start-build", "bash", string(arguments),
					"Starting the build in the background."), nil
			}
			if round := writes.Add(1); round <= writeAllowanceCalls {
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
					Text string `json:"content"`
				}{Path: fmt.Sprintf("report%d.csv", round), Text: "service,port\n"})
				return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
					"Writing the report."), nil
			}
			return textResponse("report.csv is written."), nil
		}
	}

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: steps}
	agent := finishedAgent(t, completer, path)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	wakes := agent.Wakes()
	t.Cleanup(func() { _ = os.WriteFile(flag, []byte("go\n"), 0o600) })

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// THE COMPLETION ROAD TOOK IT, and not the await road — the two write
	// different words and a case that could not tell them apart would pass on
	// either.
	if ceilings := journaledCeilings(t, path); len(ceilings) != 1 ||
		ceilings[0].Decision != checkpointCeilingNothing {
		t.Fatalf("the seam journaled %+v, want one %q", ceilings, checkpointCeilingNothing)
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted for a request the model said was discharged", count)
	}
	// AND THE COMMAND IS UNTOUCHED: nothing was stopped and nothing was marked
	// done on its behalf.
	if !jobStillRunning(t, agent, 1) {
		t.Fatal("a drop taken on the completion road stopped the conversation's own command")
	}

	if err := os.WriteFile(flag, []byte("go\n"), 0o600); err != nil {
		t.Fatalf("releasing the command: %v", err)
	}
	select {
	case stream := <-wakes:
		if stream == nil {
			t.Fatal("the wake lane carried a nil stream")
		}
		var ended bool
		for event := range stream {
			if event.Kind == EventTurnDone {
				ended = true
			}
		}
		if !ended {
			t.Fatal("the woken turn never ended")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the command ended after a completion drop and no turn was started for it")
	}

	// AND THE ENDING WAS CONSUMED HERE. The job's own news landed in this
	// conversation and this conversation answered after it, which is the whole
	// guarantee a drop is allowed to rest on.
	if !answeredAfterTheEnding(agent, "job 1 exited", "The build finished successfully and the report is ready.") {
		t.Fatalf("nothing was said after the command's ending:\n%s", transcriptText(agent))
	}
	if ran := commandsRun(completer, command); ran != 1 {
		t.Fatalf("the command was issued %d times, want exactly one", ran)
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted after the wake", count)
	}
}
