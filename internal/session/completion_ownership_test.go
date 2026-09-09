package session

// SAME-OWNER COMPLETION, AT THE REAL DOOR (calibration-02, cell
// 018-revision-midwork-aforge).
//
// The live trace these cases are written from is that cell's own
// `transcript.jsonl`, lines 4-37: a person asked for a one-minute command AND a
// report; the model started the command in the background (job 1), wrote the
// report, took the person's revision, wrote `report.csv` and removed
// `report.md` — every artifact they asked for — and the write seam fired on the
// second file. The mark's reader drew `(waiting)` (line 23), which is not a done
// shape, so the two-minds decline could not apply, and task 2 was admitted
// (line 37, `seam:write decision:moved`) carrying a brief whose whole content
// was "wait for ./slow-build.sh … verify the deliverables". The conversation
// answered correctly 42 seconds later when the job's own note woke it (line 40);
// the task went on to spawn repair and audit children and the cell hit its 180s
// cap.
//
// THE CELL WAS A PERSON'S CONVERSATION, so every case here sets
// [Config.Interactive] and none of them uses a steward: what decides these
// endings is two runtime facts and one typed answer, and a principal that a
// person's session never has cannot be the mechanism (handoff_remainder.go).

import (
	"context"
	"encoding/json"
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

// theAsk is the person's own sentence from the trace, shortened to the two
// clauses that matter: a command they want DONE, and a report.
const theAsk = "First run ./slow-build.sh here — it takes about a minute and I want it done. " +
	"While it runs, write report.csv with a header line service,port and one row per service."

// awaitingAgent is [writeSeamAgent] with the trace's own posture: a person is
// steering, so [Agent.who] answers a [Person] and no steward reads any ending.
func awaitingAgent(t *testing.T, completer Completer) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.Interactive = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
	})
	if agent.steward() != nil {
		t.Fatal("a person's session was given a steward; these cases would then prove the wrong mechanism")
	}
	return agent
}

// revisionScript is the trace's turn: the workspace writes that spend the write
// allowance ([writeAllowanceCalls] of them, which is what the seam counts), the
// `(waiting)` sketch its mark reader actually drew, and whatever the case wants
// said to the dowry ask. Every round after the writes answers in words, so a turn
// that is NOT moved ends the way the live one did — by stopping with the job
// still out.
func revisionScript(count int, dowry func() string) []step {
	var writes atomic.Int64
	steps := make([]step, count)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse("(waiting)\nWaiting for the build that was started to finish."), nil
			}
			if askedForHandoff(messages) {
				return textResponse(dowry()), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("Finish the remaining step: wait for the build and verify the report."), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			round := writes.Add(1)
			if round > writeAllowanceCalls {
				return textResponse("report.csv is written and report.md is gone; the build is still running."), nil
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

// theOffer is what the dowry ask carries when this conversation has something of
// its own running, asserted as a whole so that a case cannot pass on an ask the
// model was never shown.
func theOffer(t *testing.T, completer *scriptedCompleter) string {
	t.Helper()
	for index := range completer.requests() {
		request := completer.request(index)
		if !askedForHandoff(request) {
			continue
		}
		return messageText(request[len(request)-1])
	}
	return ""
}

// jobStillRunning is the wake this whole road rests on, read from the registry
// rather than from anything this package remembered.
func jobStillRunning(t *testing.T, agent *Agent, id int) bool {
	t.Helper()
	one := agent.jobs.find(id)
	if one == nil {
		return false
	}
	return one.info().state == jobRunning
}

// endJob settles one planted job the way its reaper would, so a case can put the
// settled-job race exactly where it wants it.
func endJob(t *testing.T, agent *Agent, id int) {
	if t != nil {
		t.Helper()
	}
	one := agent.jobs.find(id)
	one.mu.Lock()
	one.state = jobExited
	one.mu.Unlock()
}

// queueDirection puts a person's words on the steering queue WITHOUT letting the
// model read them, which is the state [Agent.Steer] leaves behind between the
// moment somebody types and the next step boundary.
func queueDirection(agent *Agent, words string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.steering = append(agent.steering, steerMessage(&turnSteer{
		note: SteerNote{ID: agent.steerSeq.Add(1), Words: words},
	}))
}

// ── the failure, closed ─────────────────────────────────────────────────────

// A CONVERSATION WHOSE ONLY REMAINDER IS THE COMMAND IT WAS ASKED TO RUN STOPS
// WITHOUT DELEGATING, AND THE COMMAND IS LEFT EXACTLY WHERE IT WAS.
//
// This is the trace's own shape with the one line the ask now teaches: the model
// answers the dowry with the operation it is waiting on, by number.
func TestAWaitOnItsOwnCommandAdmitsNoTask(t *testing.T) {
	completer := &scriptedCompleter{steps: revisionScript(12, func() string { return "AWAITING 1" })}
	agent := awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	liveJob(agent, 1, jobKindBash, "./slow-build.sh")

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// THE ROAD WAS ENTERED. The seam fired and the handover asked its dowry —
	// without this the case would pass for a turn that never reached the door.
	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	// AND THE ASK CARRIED THE OFFER, by number and by name.
	if offer := theOffer(t, completer); !strings.Contains(offer, "job 1 (./slow-build.sh)") ||
		!strings.Contains(offer, awaitOnlyToken+" 1") {
		t.Fatalf("the dowry ask did not offer the live operation:\n%s", offer)
	}
	// AND NOTHING MOVED.
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted for a turn whose only remainder was its own command", count)
	}
	if saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the person was told work was moving when none did: %q", noticeTexts(collected))
	}
	// AND THE OBLIGATION IS STILL OPEN: the command was not stopped, nothing was
	// marked done, and the wake that brings the result back is still coming.
	if !jobStillRunning(t, agent, 1) {
		t.Fatal("the command this conversation is awaiting is no longer running")
	}
	if !agent.turnIsWaitingOnItsOwnWork() {
		t.Fatal("the wake this decline rests on is gone")
	}
}

// AND THE SAME LIVE COMMAND SUPPRESSES NOTHING WHEN THERE IS REAL WORK LEFT.
//
// A rule of the form "a job is running, so never delegate" would pass the case
// above and lose this one — a conversation with a build in flight and a rename
// still to do is exactly the turn the seam exists for. The decline is the
// model's typed answer about the REMAINDER, never the presence of an operation.
func TestSubstantiveRemainderStillHandsOverWhileTheSameCommandRuns(t *testing.T) {
	const brief = "Finish the rename: the three call sites in internal/parser are untouched " +
		"and the golden suite has never been run."

	completer := &scriptedCompleter{steps: revisionScript(12, func() string { return brief })}
	agent := awaitingAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })
	liveJob(agent, 1, jobKindBash, "./slow-build.sh")

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted for a turn with real work left; want exactly one", count)
	}
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the seam never said its line; notices were %q", noticeTexts(collected))
	}
}

// ── and every way a claim can be wrong hands the work over ──────────────────

// A CLAIM THE RUNTIME CANNOT VERIFY IS NOT A DECLINE.
//
// Each of these is a way the model's answer can be wrong — a number nobody
// offered, a number for something that has already ended, a claim made about a
// request the person has since changed, and a claim made by a conversation that
// has nothing of its own running at all. Every one of them must end with the
// work handed over, because the direction this errs in is never to drop work.
func TestAnUnverifiableAwaitClaimNeverDropsTheWork(t *testing.T) {
	for _, one := range []struct {
		name  string
		plant func(t *testing.T, agent *Agent)
		dowry func(agent *Agent) string
	}{{
		// A NUMBER NOBODY OFFERED. Job 1 is live; the model names job 7.
		name:  "invented id",
		plant: func(_ *testing.T, agent *Agent) { liveJob(agent, 1, jobKindBash, "./slow-build.sh") },
		dowry: func(*Agent) string { return "AWAITING 7" },
	}, {
		// THE SETTLED-JOB RACE. The offer was made over a live job and the job
		// ended while the model was drafting; its news is already on its way, so
		// the honest answer is that the wait is over.
		name:  "ended between the offer and the answer",
		plant: func(_ *testing.T, agent *Agent) { liveJob(agent, 1, jobKindBash, "./slow-build.sh") },
		dowry: func(agent *Agent) string {
			endJob(nil, agent, 1)
			return "AWAITING 1"
		},
	}, {
		// NEW DIRECTION. The person spoke after the offer went out, so the epoch
		// the claim was made under is not the request being answered.
		name:  "the request moved under it",
		plant: func(_ *testing.T, agent *Agent) { liveJob(agent, 1, jobKindBash, "./slow-build.sh") },
		dowry: func(agent *Agent) string {
			if _, err := agent.Steer("actually make it JSON, not CSV"); err != nil {
				panic("steer: " + err.Error())
			}
			return "AWAITING 1"
		},
	}, {
		// NOTHING RUNNING AT ALL. No offer was made, so there is no door here for
		// a claim to walk through.
		name:  "no operation of its own",
		plant: func(*testing.T, *Agent) {},
		dowry: func(*Agent) string { return "AWAITING 1" },
	}} {
		t.Run(one.name, func(t *testing.T) {
			var agent *Agent
			completer := &scriptedCompleter{steps: revisionScript(14, func() string { return one.dowry(agent) })}
			agent = awaitingAgent(t, completer)
			ran := make(ranNodes, 2)
			graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })
			one.plant(t, agent)

			events, err := agent.Submit(context.Background(), theAsk)
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collect(t, events)
			ran.await(t)

			if count := admitted(graph); count != 1 {
				t.Fatalf("%d tasks were admitted; an unverifiable await must hand the work over", count)
			}
		})
	}
}

// AND A CONVERSATION WITH NOTHING RUNNING IS NEVER EVEN OFFERED THE LINE, which
// is what keeps this door shut for every turn the failure was not about.
func TestNoLiveOperationMeansNoOfferAtAll(t *testing.T) {
	completer := &scriptedCompleter{steps: revisionScript(14, func() string { return "finish the rename" })}
	agent := awaitingAgent(t, completer)
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ran.await(t)

	if offer := theOffer(t, completer); strings.Contains(offer, awaitOnlyToken) {
		t.Fatalf("a session with nothing of its own running was taught the await line:\n%s", offer)
	}
}

// ── the validator, on its own ───────────────────────────────────────────────

// THE GRAMMAR IS EXACT: the token, single spaces, plain positive numbers, and
// nothing else in the reply. Everything else is a brief and is handed over.
func TestReadAwaitClaimTakesOneExactLine(t *testing.T) {
	for _, one := range []struct {
		answer string
		want   []int
	}{
		{"AWAITING 1", []int{1}},
		{"AWAITING 1 4", []int{1, 4}},
		{"AWAITING 12 7 3", []int{12, 7, 3}},
		{"  AWAITING 2\n", []int{2}},
		// A CLAIM WITH ANYTHING AFTER IT SAYS TWO THINGS, and dropping the second
		// half would drop real work.
		{"AWAITING 1\nAlso rewrite the parser.", nil},
		{"AWAITING 1. It should be done shortly.", nil},
		{"AWAITING 1\nAWAITING 2", nil},
		// AND NO SPELLING THE ASK DID NOT TEACH IS ACCEPTED.
		{"AWAITING1", nil},
		{"AWAITING  1", nil},
		{"AWAITING 1,4", nil},
		{"AWAITING 1, 4", nil},
		{"AWAITING\t1", nil},
		{"AWAITING #1", nil},
		{"AWAITING +1", nil},
		{"AWAITING -1", nil},
		{"AWAITING 0", nil},
		{"AWAITING 01", nil},
		{"AWAITING 1 1", nil},
		{"AWAITING 99999999999999999999", nil},
		{"awaiting 1", nil},
		{"AWAITING", nil},
		{"AWAITING the build", nil},
		{"We are awaiting job 1.", nil},
		{"NOTHING LEFT TO DO", nil},
		{"", nil},
	} {
		got, ok := readAwaitClaim(one.answer)
		if len(one.want) == 0 {
			if ok {
				t.Errorf("%q read as an await claim of %v", one.answer, got)
			}
			continue
		}
		if !ok || fmt.Sprint(got) != fmt.Sprint(one.want) {
			t.Errorf("%q read as %v (ok=%v), want %v", one.answer, got, ok, one.want)
		}
	}
}

// AND THE GROUND AND THE FOUR CHECKS, each on its own, against a session holding
// one live command and one running turn.
func TestConfirmAwaitGrantsOnlyWhatItCanVerify(t *testing.T) {
	newSession := func(t *testing.T) (*Agent, []ownedOperation, awaitGround) {
		t.Helper()
		agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}}, func(*Config) {})
		liveJob(agent, 1, jobKindBash, "./slow-build.sh")
		agent.mu.Lock()
		agent.running, agent.turnSeq = true, 4
		agent.mu.Unlock()
		ground := agent.awaitGroundNow()
		if !ground.sound {
			t.Fatal("a running turn with nothing queued reads as no ground at all")
		}
		return agent, agent.awaitableOperations(), ground
	}

	t.Run("granted over a live offered operation on an unmoved request", func(t *testing.T) {
		agent, offered, at := newSession(t)
		decided := agent.confirmAwait([]int{1}, offered, at)
		if !decided.granted {
			t.Fatalf("a verifiable claim was refused: %s", decided.refused)
		}
		if !agent.stillGranted(decided) {
			t.Fatal("the same claim did not survive the recheck at consumption")
		}
	})
	t.Run("an id that was not offered", func(t *testing.T) {
		agent, offered, at := newSession(t)
		if decided := agent.confirmAwait([]int{7}, offered, at); decided.granted ||
			decided.refused != awaitUnknownID {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	t.Run("an operation that has since ended", func(t *testing.T) {
		agent, offered, at := newSession(t)
		endJob(t, agent, 1)
		if decided := agent.confirmAwait([]int{1}, offered, at); decided.granted ||
			decided.refused != awaitEnded {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	t.Run("a request that moved after the offer", func(t *testing.T) {
		agent, offered, at := newSession(t)
		agent.steerSeq.Add(1)
		if decided := agent.confirmAwait([]int{1}, offered, at); decided.granted ||
			decided.refused != awaitRequestMoved {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	t.Run("nothing was offered", func(t *testing.T) {
		agent, _, at := newSession(t)
		if decided := agent.confirmAwait([]int{1}, nil, at); decided.granted ||
			decided.refused != awaitNoOffer {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	t.Run("no claim at all", func(t *testing.T) {
		agent, offered, at := newSession(t)
		if decided := agent.confirmAwait(nil, offered, at); decided.granted ||
			decided.refused != awaitNotClaimed {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	// AND THE CONSUMPTION IS ITS OWN MOMENT. A grant taken a model call ago is
	// evidence about a request that may since have moved, so both facts are read
	// again where the road acts on them.
	t.Run("a grant that stops holding before it is consumed", func(t *testing.T) {
		agent, offered, at := newSession(t)
		decided := agent.confirmAwait([]int{1}, offered, at)
		if !decided.granted {
			t.Fatalf("the fixture's own premise is gone: %s", decided.refused)
		}
		endJob(t, agent, 1)
		if agent.stillGranted(decided) {
			t.Fatal("a grant survived the operation it named ending")
		}
	})
	t.Run("direction queued before the consumption", func(t *testing.T) {
		agent, offered, at := newSession(t)
		decided := agent.confirmAwait([]int{1}, offered, at)
		if !decided.granted {
			t.Fatalf("the fixture's own premise is gone: %s", decided.refused)
		}
		queueDirection(agent, "actually make it JSON")
		if agent.stillGranted(decided) {
			t.Fatal("a grant survived a correction the model has not read")
		}
	})
}

// DIRECTION THE MODEL HAS NOT READ IS NO GROUND AT ALL, AND THE EPOCH ALONE
// CANNOT SEE IT.
//
// [Agent.Steer] mints its number when the words are ENQUEUED and the words reach
// the model only at the next boundary. So a correction that arrived before the
// offer was built leaves the epoch identical at every later reading, and a check
// that compared epochs alone would grant an await over a request the model was
// answering blind. The queue is what says so.
func TestUnreadDirectionBeforeTheOfferIsNoGround(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}}, func(*Config) {})
	liveJob(agent, 1, jobKindBash, "./slow-build.sh")
	agent.mu.Lock()
	agent.running, agent.turnSeq = true, 4
	agent.mu.Unlock()

	queueDirection(agent, "actually make it JSON, not CSV")
	// THE EPOCH IS UNCHANGED ACROSS THE WHOLE CASE, which is the point: nothing
	// here is caught by comparing one to another.
	before := requestEpochAt(agent)
	ground := agent.awaitGroundNow()
	if ground.sound {
		t.Fatal("a session holding a correction the model has not read offered ground for an await")
	}
	if decided := agent.confirmAwait([]int{1}, agent.awaitableOperations(), ground); decided.granted ||
		decided.refused != awaitRequestMoved {
		t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
	}
	if after := requestEpochAt(agent); after != before {
		t.Fatalf("the epoch moved (%v to %v); this case must fail on the queue alone", before, after)
	}
}

func TestQueuedSubmitCannotAuthorizeAwait(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.running, agent.turnSeq = true, 4
	agent.hub = newEventHub()
	hub := agent.hub
	agent.mu.Unlock()
	defer hub.close()
	if _, err := agent.Submit(context.Background(), "Also produce a JSON copy."); err != nil {
		t.Fatal(err)
	}
	if agent.awaitGroundNow().sound {
		t.Fatal("an unread Submit message authorized waiting on the older request")
	}
}

// AND A SESSION THAT IS NOT RUNNING A TURN, OR IS CLOSED, IS NO GROUND EITHER.
func TestOnlyARunningOpenTurnIsGround(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}}, func(*Config) {})
	liveJob(agent, 1, jobKindBash, "./slow-build.sh")

	if agent.awaitGroundNow().sound {
		t.Fatal("a session with no turn running offered ground for an await")
	}
	agent.mu.Lock()
	agent.running, agent.turnSeq = true, 2
	agent.mu.Unlock()
	if !agent.awaitGroundNow().sound {
		t.Fatal("a running turn with nothing queued is not ground")
	}
	agent.mu.Lock()
	agent.closed = true
	agent.mu.Unlock()
	if agent.awaitGroundNow().sound {
		t.Fatal("a closed session offered ground for an await")
	}
}

// AND A WATCH IS NOT AWAITABLE, which is the kind policy stated as a case rather
// than only as a comment: a watch has no ending of its own to be owed.
func TestAWatchIsNeverOffered(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}}, func(*Config) {})
	liveJob(agent, 5, jobKindWatch, "pr checks")
	if operations := agent.awaitableOperations(); len(operations) != 0 {
		t.Fatalf("a watch was offered as awaitable: %v", operations)
	}
}

// ── and the ending really does come back ────────────────────────────────────

// THE DECLINE RESTS ON A WAKE, SO THE WAKE IS PROVED WITH A REAL COMMAND.
//
// Everything above plants a job. This one lets the model start a genuine
// background command through `bash` inside [Agent.Submit], held on a file nobody
// has created yet, declines the handover over it, and then creates that file.
// What is asserted is the whole return path: the command's own exit starts a
// turn here, that turn answers, and it does so without re-running the command
// and without starting a task.
func TestTheAwaitedCommandsEndingWakesTheConversation(t *testing.T) {
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
				return textResponse("(waiting)\nWaiting for the build that was started."), nil
			case askedForHandoff(messages):
				return textResponse("AWAITING 1"), nil
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
			return textResponse("report.csv is written; the build is still running."), nil
		}
	}

	completer := &scriptedCompleter{steps: steps}
	agent := awaitingAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	// THE WAKE LANE IS SUBSCRIBED BEFORE THE COMMAND CAN POSSIBLY END, and the
	// command is released whatever this case does next, so a failure leaves no
	// process behind.
	wakes := agent.Wakes()
	t.Cleanup(func() { _ = os.WriteFile(flag, []byte("go\n"), 0o600) })

	events, err := agent.Submit(context.Background(), theAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	if handoffAsks(completer) != 1 {
		t.Fatalf("the handover road was taken %d times, want exactly one", handoffAsks(completer))
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted over a command this conversation was awaiting", count)
	}
	if !jobStillRunning(t, agent, 1) {
		t.Fatal("the command was not left running by the decline")
	}

	// AND NOW THE COMMAND IS LET GO. Nothing else is touched: what happens next
	// is the registry's own ending and the session's own wake.
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
		t.Fatal("the awaited command ended and no turn was started for it")
	}

	// THE ENDING IS IN THE CONVERSATION AS THE JOB'S OWN NEWS, and the model
	// answered AFTER it — which is the whole return path this decline rests on:
	// `job 1 exited 0` arrives as a note and the turn it starts speaks.
	if !answeredAfterTheEnding(agent, "job 1 exited", "The build finished successfully and the report is ready.") {
		t.Fatalf("nothing was said after the command's ending:\n%s", transcriptText(agent))
	}
	// AND NOTHING WAS DONE TWICE. The command ran once and no task was started
	// for it after the wake either.
	if got := started.Load(); got < 1 {
		t.Fatal("the fixture never started the command")
	}
	if ran := commandsRun(completer, command); ran != 1 {
		t.Fatalf("the command was issued %d times, want exactly one", ran)
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted after the wake", count)
	}
}

// answeredAfterTheEnding reports that the conversation said something of its own
// after one note landed in it.
func answeredAfterTheEnding(agent *Agent, note, answer string) bool {
	messages := agent.snapshot()
	for index, message := range messages {
		if message.Role != "user" || !strings.Contains(messageText(message), note) {
			continue
		}
		for _, later := range messages[index+1:] {
			if later.Role == "assistant" && strings.Contains(messageText(later), answer) {
				return true
			}
		}
	}
	return false
}

// commandsRun counts how many times the script actually asked for one shell
// command, read off the assistant messages the completer was sent.
func commandsRun(completer *scriptedCompleter, command string) int {
	ran := 0
	for index := range completer.requests() {
		for _, message := range completer.request(index) {
			for _, call := range message.ToolCalls {
				if call.Function.Name == "bash" && strings.Contains(call.Function.Arguments, command) {
					ran++
				}
			}
		}
	}
	if ran == 0 {
		return 0
	}
	// The same assistant message is re-sent with every later request, so the
	// distinct call is what matters rather than how often it was replayed.
	return 1
}
