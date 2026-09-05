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
	"strings"
	"sync/atomic"
	"testing"

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

// revisionScript is the trace's turn: two workspace writes, the `(waiting)`
// sketch its mark reader actually drew, and whatever the case wants said to the
// dowry ask. Every round after the writes answers in words, so a turn that is
// NOT moved ends the way the live one did — by stopping with the job still out.
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
			if round > writeAllowanceFiles {
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
			one := agent.jobs.find(1)
			one.mu.Lock()
			one.state = jobExited
			one.mu.Unlock()
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

// THE READING IS THE WHOLE LINE OR IT IS A BRIEF.
func TestReadAwaitClaimTakesTheWholeLineOrNothing(t *testing.T) {
	for _, one := range []struct {
		answer string
		want   []int
	}{
		{"AWAITING 1", []int{1}},
		{"AWAITING 1 4", []int{1, 4}},
		{"AWAITING 1, 4", []int{1, 4}},
		{"AWAITING 1,1", []int{1}},
		{"  AWAITING 2  ", []int{2}},
		// AND EVERYTHING ELSE IS A BRIEF, INCLUDING A CLAIM WITH PROSE AFTER IT:
		// an answer that says two things must not have its second half dropped.
		{"AWAITING 1\nAlso rewrite the parser.", nil},
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

// AND THE FOUR CHECKS, EACH ONE ON ITS OWN, against a session holding one live
// command and one turn.
func TestConfirmAwaitGrantsOnlyWhatItCanVerify(t *testing.T) {
	newSession := func(t *testing.T) (*Agent, []ownedOperation, requestEpoch) {
		t.Helper()
		agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{finalText("done")}}, func(*Config) {})
		liveJob(agent, 1, jobKindBash, "./slow-build.sh")
		agent.mu.Lock()
		agent.running, agent.turnSeq = true, 4
		agent.mu.Unlock()
		return agent, agent.awaitableOperations(), requestEpochAt(agent)
	}

	t.Run("granted over a live offered operation on an unmoved request", func(t *testing.T) {
		agent, offered, at := newSession(t)
		if decided := agent.confirmAwait([]int{1}, offered, at); !decided.granted {
			t.Fatalf("a verifiable claim was refused: %s", decided.refused)
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
		one := agent.jobs.find(1)
		one.mu.Lock()
		one.state = jobExited
		one.mu.Unlock()
		if decided := agent.confirmAwait([]int{1}, offered, at); decided.granted ||
			decided.refused != awaitEnded {
			t.Fatalf("granted=%v refused=%q", decided.granted, decided.refused)
		}
	})
	t.Run("a request that moved", func(t *testing.T) {
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
