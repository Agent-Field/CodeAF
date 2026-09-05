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
	"fmt"
	"path/filepath"
	"reflect"
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
		"\n1. task 1 was for: port the language server" +
		"\n2. task 3 was for: fix the release dates in NOTES.md"
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
		"\n1. they asked: summarise the release notes" +
		"\n2. task 4 was for: port the language server"
	if got := agent.turnAsk(); got != want {
		t.Errorf("the mixed turn owes\n%s\nwant\n%s", got, want)
	}
}

// ── a goal the person moved while the work ran ──────────────────────────────

// A REVISED TASK IS JUDGED BY WHAT IT WAS REVISED TO, AND STILL CITED BY WHAT
// WAS FIRST ASKED.
//
// The person asked for JSON, said CSV instead into the task's room while it ran
// and the worker folded that in ([TaskNode.reviseAssignment]), and the work came
// home as CSV. The admitted request still says JSON — it is frozen, and that is
// what makes it a citation — so a reader given only that would reject correct
// work and try to restore a goal nobody holds any more.
//
// It goes through the real assignment, the real landing and the real delivery:
// what has to hold is that the tag the session receives was composed from THIS
// task's own snapshot.
func TestARevisedTaskIsJudgedByItsRevisionAndCitedByItsOriginal(t *testing.T) {
	const original = "export the release inventory as JSON"
	const said = "actually, make it CSV instead — the importer cannot read JSON"

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.personAsk = "and how big is that file?"
	agent.mu.Unlock()
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "export the inventory", summary: "one", request: original,
		brief: "write the inventory out as JSON", deliverable: "inventory.json",
		acceptance: "inventory.json parses",
	})
	node := graph.node(id)

	// THE PERSON SAYS IT INTO THE ROOM AND THE WORKER FOLDS IT IN.
	heard := node.heardDirection(said, directionFromPerson, spokenSource{})
	direction := heard.id
	if !heard.fresh() {
		t.Fatal("the person's direction was not recorded")
	}
	version, err := node.reviseAssignment(direction, 0, assignmentEdit{
		work:        "write it as CSV",
		deliverable: "inventory.csv",
		acceptance:  "inventory.csv parses as CSV with a header row",
	})
	if err != nil {
		t.Fatalf("revising the assignment: %v", err)
	}

	// AND IT LANDS, down the road a runner uses.
	landNode(node, TaskDone, "inventory.csv written")
	waitFor(t, "the landing to reach the conversation", func() bool {
		return len(queuedText(agent)) > 0
	})
	agent.drainSteering(nil)

	asked := agent.turnAsk()
	if !strings.Contains(asked, "inventory.csv") || !strings.Contains(asked, "CSV") {
		t.Errorf("the revised target is not what the result is read against:\n%s", asked)
	}
	if !strings.Contains(asked, fmt.Sprintf("revision %d", version)) {
		t.Errorf("the reader is not told which version it is judging:\n%s", asked)
	}
	if !strings.Contains(asked, said) {
		t.Errorf("the person's own words are not what the target rests on:\n%s", asked)
	}
	// The admitted ask is present as HISTORY, labelled, and not as a thing owed.
	if !strings.Contains(asked, "First asked (history, superseded below): "+original) {
		t.Errorf("the admitted ask is not carried as history:\n%s", asked)
	}
	// AND THE CITATION IS UNTOUCHED. Request is what a surface prints beside the
	// answer, and it stays the person's own first words.
	tags := agent.takeReplyTags()
	if len(tags) != 1 || tags[0].Request != original {
		t.Fatalf("the surface's citation = %#v, want the original request verbatim", tags)
	}
	if tags[0].Revision != version || tags[0].Obligation == "" {
		t.Errorf("the effective target did not survive to the surface: %#v", tags[0])
	}
}

// A RESULT IS DELIVERED WITH THE TARGET IT WAS COMPOSED AGAINST, WHATEVER THE
// NODE BECOMES WHILE IT IS IN FLIGHT.
//
// A landing is not announced at the instant it happens: there is a claim to win
// and a reader to find, and a node can be revised again in that gap — a revision
// does not raise the attempt, so the claim still stands and the delivery still
// goes out. Reading the live node at the far end would hand the OLD result the
// NEW target.
func TestAResultInFlightKeepsTheTargetItWasComposedAgainst(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "export the inventory", summary: "one", request: "export it as JSON",
		brief: "write it out", deliverable: "inventory.json", acceptance: "it parses",
	})
	node := graph.node(id)

	first := node.heardDirection("make it CSV instead", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(first, 0, assignmentEdit{deliverable: "inventory.csv"}); err != nil {
		t.Fatalf("first revision: %v", err)
	}

	// THE REPORT IS COMPOSED HERE, against the assignment as it stands.
	_, attempt, tag := node.resultOf()

	// AND THE PERSON MOVES THE GOAL AGAIN BEFORE IT IS DELIVERED.
	second := node.heardDirection("no, TSV in the end", directionFromPerson, spokenSource{}).id
	if _, err := node.reviseAssignment(second, 1, assignmentEdit{deliverable: "inventory.tsv"}); err != nil {
		t.Fatalf("second revision: %v", err)
	}

	agent.deliverTaskNote(node, attempt, tag, "task 1 finished: inventory.csv written")
	agent.drainSteering(nil)

	asked := agent.turnAsk()
	if !strings.Contains(asked, "inventory.csv") {
		t.Errorf("the delivered result lost the target it was composed against:\n%s", asked)
	}
	if strings.Contains(asked, "inventory.tsv") {
		t.Errorf("a revision made after the result was composed was substituted onto it:\n%s", asked)
	}
	if got := agent.takeReplyTags(); len(got) != 1 || got[0].Revision != 1 {
		t.Errorf("the delivered tag = %#v, want the version the report was composed at", got)
	}
}

// A LATE RESULT DOES NOT OVERRULE WHAT THE PERSON HAS ASKED FOR SINCE.
//
// Both are owed and both are listed, in the order they arrived — but the list
// says whose line is whose, so the reader is not left to infer that the newest
// line in the room is the authority when the newest line is a slow task's target.
func TestALateResultDoesNotOverruleTheirNewerWords(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.opened = false
	agent.forgetOwedLocked()
	agent.rememberOwedLocked(userText("stop the export, just tell me the row count"))
	agent.mu.Unlock()

	note := wakeNote("task 4 finished")
	note.replyTags = []TaskReplyTag{{ID: 4, Title: "export", Request: "export the inventory as JSON"}}
	if !agent.enqueueNote(note) {
		t.Fatal("the note was refused")
	}
	agent.drainSteering(nil)

	asked := agent.turnAsk()
	if !strings.HasPrefix(asked, owedAsksLead) {
		t.Fatalf("two owed asks were run together without saying how to read them:\n%s", asked)
	}
	if !strings.Contains(asked, "1. they asked: stop the export") {
		t.Errorf("the person's own line is not named as theirs:\n%s", asked)
	}
	if !strings.Contains(asked, "2. task 4 was for: export the inventory as JSON") {
		t.Errorf("the result's line is not named as that task's own target:\n%s", asked)
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
	// AND A NODE NOBODY HAS SAID ANYTHING TO ANSWERS THE UNREVISED PAIR, so its
	// tag is the two fields this build always sent.
	graph := newTaskGraph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "export it", summary: "one",
		request: "export it as JSON", brief: "one", acceptance: "one"})
	tag := graph.node(id).resultTag()
	if tag.Obligation != "" || tag.Revision != 0 {
		t.Errorf("an unrevised node composed %#v, want no effective target", tag)
	}
	if tag.Request != "export it as JSON" {
		t.Errorf("the citation = %q, want the person's own words", tag.Request)
	}
}

// AND THE ADDITIVE FIELDS SURVIVE THE JOURNAL.
//
// A session resumed from disk draws the same citation and holds the same target:
// both ride the note's own record ([sessionFile.appendNote]), and a field that
// only existed in memory would leave a resumed window citing one thing and a
// reader judging another.
func TestTheEffectiveTargetSurvivesTheJournalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	journal, _, err := openSessionFile(path, "/tmp/work", "model", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	want := []TaskReplyTag{{
		ID: 9, Title: "export the inventory", Request: "export it as JSON",
		Obligation: obligationText("export it as JSON", "make it CSV instead", "inventory.csv", "it parses as CSV", 1),
		Revision:   1,
	}}
	journal.appendNote(textMessage("user", "task 9 finished"), noteMarks{tags: want})
	journal.appendMessage(textMessage("assistant", "inventory.csv is written."))
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, replayed, err := openSessionFile(path, "/tmp/work", "model", "session-id")
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	for _, entry := range shapeEntries(replayed.messages, resumed) {
		if entry.Role != "assistant" {
			continue
		}
		if !reflect.DeepEqual(entry.ReplyTags, want) {
			t.Fatalf("resumed reply tags = %#v, want %#v", entry.ReplyTags, want)
		}
		return
	}
	t.Fatal("resumed transcript has no assistant reply")
}

// itoaTask spells a task id the way a landing note does.
func itoaTask(id uint64) string { return "task " + itoa(int(id)) }
