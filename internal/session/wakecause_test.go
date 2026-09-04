package session

// A TURN WOKEN BY A RESULT IS READ AGAINST THAT RESULT'S REQUEST.
//
// THE MEASURED FAILURE these cases are written from: a live run on 2026-09-04
// (host-live-02). A build was delegated; while it ran the person asked for a
// checksum word reversed and was answered (journal line 35); the task landed and
// the woken turn reported the marker (line 43); and the end-of-turn reader was
// then shown the CHECKSUM question as the ask, said it had not been answered
// (line 46), and the turn was carried on into repeating it (line 56).

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	delegateAsk  = "delegate this: run ./slow-build.sh, wait for it, and tell me the marker it writes to build.log"
	checksumAsk  = "while that runs — tell me the checksum word in NOTES.txt spelled backwards in capitals"
	reversedWord = "RABANNIC"
	markerLine   = "task 1 finished: the marker value is QUARTZLINE"
)

// asksSeen collects every digest a remains-reader was actually shown, which is
// the only place the ask a turn was judged against can be observed from outside.
type asksSeen struct {
	mu   sync.Mutex
	seen []string
}

func (a *asksSeen) add(messages []ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, messageText(messages[len(messages)-1]))
}

func (a *asksSeen) all() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

// lastAsk is what the newest reading was shown, and "" when nobody was asked.
func (a *asksSeen) lastAsk() string {
	all := a.all()
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

// delegateThenAnswerSteps is the measured shape as a script: hand the build off,
// answer the unrelated question inline, and report the marker when the landing
// wakes a turn. Everything else is the sidecar's.
func delegateThenAnswerSteps(reader *asksSeen, remains func() string) []step {
	steps := make([]step, 60)
	for index := range steps {
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				reader.add(messages)
				return textResponse(remains()), nil
			}
			if answer, handled := checkpointSidecar(messages, remains); handled {
				return answer, nil
			}
			asked := userTextIn(messages)
			switch {
			case strings.Contains(asked, "QUARTZLINE"):
				return textResponse("Task 1's report landed: the marker is QUARTZLINE."), nil
			case strings.Contains(asked, "checksum word"):
				return textResponse("CINNABAR spelled backwards in capitals: " + reversedWord), nil
			case strings.Contains(asked, "slow-build.sh") && !alreadyProposed(messages):
				return proposeAs("call-task-1", "run the slow build", slowBuildBrief)(ctx, messages)
			}
			return textResponse("Task 1 is running; this conversation stays free."), nil
		}
	}
	return steps
}

// alreadyProposed says this turn has already made its proposal, so the step
// after it answers in words instead of proposing again.
func alreadyProposed(messages []ai.Message) bool {
	for _, message := range messages {
		if message.Role == "tool" && strings.Contains(messageText(message), "task 1 ") {
			return true
		}
	}
	return false
}

// landNode finishes one node and settles it, which is the real road a report
// takes to the conversation ([Agent.reportTaskNode], [Agent.deliverTaskNote]).
func landNode(node *TaskNode, state TaskState, report string) {
	node.finish(report, nil, "", "")
	node.graph.complete(node, state)
}

// ── the defect ──────────────────────────────────────────────────────────────

// THE READER IS SHOWN THE TASK'S OWN REQUEST, AND NOT THE QUESTION THAT WAS
// ANSWERED A TURN EARLIER.
func TestAWokenTurnIsReadAgainstTheRequestItsResultBelongsTo(t *testing.T) {
	var reader asksSeen
	completer := &scriptedCompleter{steps: delegateThenAnswerSteps(&reader, func() string {
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	// 1. The work is delegated and the turn ends.
	first, err := agent.Submit(context.Background(), delegateAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	approveTasks(t, agent, first)
	node := theRunningNode(t, graph)

	// 2. An unrelated question is asked and answered while it runs.
	second, err := agent.Submit(context.Background(), checksumAsk)
	if err != nil {
		t.Fatalf("second Submit: %v", err)
	}
	collect(t, second)
	if !strings.Contains(transcriptText(agent), reversedWord) {
		t.Fatalf("the checksum question was never answered:\n%s", transcriptText(agent))
	}
	answeredOnce := strings.Count(transcriptText(agent), reversedWord)

	// 3. The task lands, and its report starts a turn on its own.
	landNode(node, TaskDone, "the marker value is QUARTZLINE")
	waitFor(t, "the landing to be answered", func() bool {
		return strings.Contains(transcriptText(agent), "the marker is QUARTZLINE")
	})
	waitFor(t, "the woken turn to be read for what remains", func() bool {
		return reader.lastAsk() != ""
	})

	// THE ASK IS THE TASK'S OWN REQUEST.
	shown := reader.lastAsk()
	if !strings.Contains(shown, node.request()) {
		t.Errorf("the reader was not shown the request its result belongs to.\nwant to contain: %q\ngot:\n%s",
			node.request(), shown)
	}
	// AND NOT THE QUESTION THAT WAS ALREADY ANSWERED. This is the negative half:
	// with the ask taken from the newest thing typed, the checksum question is
	// what a reader is handed and what it reports as undone.
	head, _, _ := strings.Cut(shown, checkpointDigestDone)
	if strings.Contains(head, "checksum") {
		t.Errorf("the reader was asked about an unrelated question answered a turn earlier:\n%s", head)
	}
	if strings.Contains(transcriptText(agent), checkpointCarryOnLead) {
		t.Errorf("the woken turn was carried on:\n%s", transcriptText(agent))
	}
	if got := strings.Count(transcriptText(agent), reversedWord); got != answeredOnce {
		t.Errorf("the reversed word was said %d times, want the one answer already given (%d)", got, answeredOnce)
	}
}

// A FAILED RESULT IS STILL READ AGAINST ITS OWN REQUEST, AND STILL REMEDIATED.
func TestAFailedResultIsReadAgainstTheRequestItWasFor(t *testing.T) {
	const left = "the build never produced a marker, so nothing has been reported"

	var reader asksSeen
	var readings int
	var mu sync.Mutex
	completer := &scriptedCompleter{steps: delegateThenAnswerSteps(&reader, func() string {
		mu.Lock()
		defer mu.Unlock()
		readings++
		if readings == 1 {
			return left
		}
		return checkpointNothingLeft
	})}
	agent := checkpointAgent(t, completer)
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	first, err := agent.Submit(context.Background(), delegateAsk)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	approveTasks(t, agent, first)
	node := theRunningNode(t, graph)

	second, err := agent.Submit(context.Background(), checksumAsk)
	if err != nil {
		t.Fatalf("second Submit: %v", err)
	}
	collect(t, second)

	landNode(node, TaskFailed, "./slow-build.sh exited 127")
	waitFor(t, "the failure to be read for what remains", func() bool {
		return reader.lastAsk() != ""
	})
	waitFor(t, "the turn to be carried on", func() bool {
		return strings.Contains(transcriptText(agent), checkpointCarryOnLead+left)
	})

	if shown := reader.lastAsk(); !strings.Contains(shown, node.request()) {
		t.Errorf("a failed result was not read against its own request.\nwant to contain: %q\ngot:\n%s",
			node.request(), shown)
	}
}

// ── what a turn owes, at the seam ───────────────────────────────────────────

// A BATCH OF LANDINGS KEEPS EVERY REQUEST IT OWES, ONCE EACH.
//
// It drives the real queue and the real drain ([Agent.drainSteering] coalesces
// through [batchSessionNotes]), because what has to survive is the tag on the
// note and not a value a test handed in.
func TestABatchOfLandingsKeepsEveryRequestItOwes(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.personAsk = "something else entirely"
	agent.mu.Unlock()

	for _, landing := range []struct {
		id      uint64
		request string
	}{
		{1, "port the language server"},
		{2, "port the language server"},
		{3, "fix the release dates in NOTES.md"},
	} {
		note := wakeNote(itoaTask(landing.id) + " finished")
		note.replyTags = []TaskReplyTag{{ID: landing.id, Title: "part", Request: landing.request}}
		if !agent.enqueueNote(note) {
			t.Fatalf("note %d was refused", landing.id)
		}
	}
	agent.drainSteering(nil)

	want := owedAsksLead +
		"\n1. port the language server" +
		"\n2. fix the release dates in NOTES.md"
	if got := agent.turnAsk(); got != want {
		t.Errorf("the batch owes\n%s\nwant\n%s\n— numbered in arrival order, repeats written once", got, want)
	}
}

// A WAKE THAT CARRIES NO RESULT IS UNCHANGED.
//
// A job exiting and a watch firing wake a turn with nothing to attribute it to,
// and the person's newest message is what those endings have always been read
// against.
func TestAWakeWithNoResultKeepsThePersonsOwnAsk(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.personAsk = "watch the log and tell me when the checks go green"
	agent.mu.Unlock()

	if !agent.enqueueNote(wakeNote("job 3 exited 0")) {
		t.Fatal("the job note was refused")
	}
	agent.drainSteering(nil)

	if got, want := agent.turnAsk(), "watch the log and tell me when the checks go green"; got != want {
		t.Errorf("a tagless wake is read against %q, want the person's own ask %q", got, want)
	}
}

// AND A TURN THE PERSON OPENED OWES WHAT THEY TYPED, plus anything that lands in
// it — which is the mixed turn, and the reason the person's own message is on
// this list rather than beside it.
func TestATurnThePersonOpenedOwesTheirWordsAndWhatLandsInIt(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	agent.mu.Lock()
	agent.forgetOwedLocked()
	agent.rememberOwedLocked(userText("summarise the release notes"))
	agent.mu.Unlock()
	if got := agent.turnAsk(); got != "summarise the release notes" {
		t.Fatalf("a person's own turn owes %q", got)
	}

	note := wakeNote("task 4 finished")
	note.replyTags = []TaskReplyTag{{ID: 4, Title: "part", Request: "port the language server"}}
	if !agent.enqueueNote(note) {
		t.Fatal("the note was refused")
	}
	agent.drainSteering(nil)

	want := owedAsksLead +
		"\n1. summarise the release notes" +
		"\n2. port the language server"
	if got := agent.turnAsk(); got != want {
		t.Errorf("the mixed turn owes\n%s\nwant\n%s", got, want)
	}
}

// ── a goal the person moved while the work ran ──────────────────────────────

// A REVISED TASK IS JUDGED BY WHAT IT WAS REVISED TO, AND STILL CITED BY WHAT
// WAS FIRST ASKED.
//
// The person asked for JSON, said CSV instead into the task's room while it ran,
// and the work came home as CSV. The frozen admitted request still says JSON, so
// a reader given only that would reject correct work and try to restore a goal
// nobody holds any more.
//
// THE TAG IS THE WHOLE OF IT: [TaskReplyTag.Obligation] is composed from the
// assignment's snapshot where the report is, Request keeps the person's original
// words for the row a surface draws, and the reader is given the first.
func TestARevisedTaskIsJudgedByItsRevisionAndCitedByItsOriginal(t *testing.T) {
	const original = "export the release inventory as JSON"
	const revised = "actually, make it CSV instead — the importer cannot read JSON"

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.personAsk = "and how big is the file?"
	agent.mu.Unlock()

	// What the delivery composes, from one snapshot of the finished assignment.
	obligation := obligationText(original, revised, "inventory.csv", "the file parses as CSV with a header row", 2)
	note := wakeNote("task 7 finished: inventory.csv written")
	note.replyTags = []TaskReplyTag{{
		ID: 7, Title: "export the inventory", Request: original,
		Obligation: obligation, Revision: 2,
	}}
	if !agent.enqueueNote(note) {
		t.Fatal("the note was refused")
	}
	agent.drainSteering(nil)

	asked := agent.turnAsk()
	if !strings.Contains(asked, "CSV") || !strings.Contains(asked, "inventory.csv") {
		t.Errorf("the revised target is not what the result is read against:\n%s", asked)
	}
	if !strings.Contains(asked, "revision 2") {
		t.Errorf("the reader is not told which version it is judging:\n%s", asked)
	}
	// The original is present as HISTORY and labelled as superseded — not as the
	// thing still owed.
	if !strings.Contains(asked, "First asked (history, superseded below): "+original) {
		t.Errorf("the admitted ask is not carried as history:\n%s", asked)
	}
	// AND THE CITATION IS UNTOUCHED. Request is what a surface draws beside the
	// answer, and it stays the person's own first words.
	tags := agent.takeReplyTags()
	if len(tags) != 1 || tags[0].Request != original {
		t.Fatalf("the surface's citation = %#v, want the original request verbatim", tags)
	}
	if tags[0].Revision != 2 || tags[0].Obligation == "" {
		t.Errorf("the effective target did not survive to the surface: %#v", tags[0])
	}
}

// AND AN UNREVISED TASK COMPOSES NO EFFECTIVE TARGET AT ALL, so its tag is the
// one this build already sends and every reader is unchanged.
func TestAnUnrevisedTaskCarriesNoEffectiveTarget(t *testing.T) {
	if got := obligationText("export it as JSON", "", "", "", 0); got != "" {
		t.Errorf("an unrevised assignment composed %q, want nothing", got)
	}
	// A version with no applied words of the person's is not an obligation
	// either: the model's own brief is not their authority.
	if got := obligationText("export it as JSON", "   ", "inventory.csv", "", 3); got != "" {
		t.Errorf("a revision with none of the person's words composed %q, want nothing", got)
	}
	// AND THE NODE ANSWERS THE UNREVISED PAIR ON THIS BRANCH. Population is the
	// steering merge's (see [TaskNode.obligationNow]); until then a delivered tag
	// carries the admitted ask alone, which is what these tests assert everywhere
	// else.
	node := &TaskNode{}
	if text, revision := node.obligationNow(); text != "" || revision != 0 {
		t.Errorf("obligationNow = %q/%d on a branch with no assignment module", text, revision)
	}
}

// itoaTask spells a task id the way a landing note does.
func itoaTask(id uint64) string { return "task " + itoa(int(id)) }
