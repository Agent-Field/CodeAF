package session

// THE REPAIR LOOP, THE LADDER, AND THE WORDS A PERSON READS.
//
// Three laws are exercised here and they are three separate files' worth of
// behaviour meeting at one node (task_audit.go):
//
//   - work that came back short is HANDED BACK — same worktree, fresh worker,
//     the gaps in front of it — and lands on the next answer;
//   - an auditor that stopped one word short is NUDGED before anybody pays for
//     a second investigation;
//   - and none of the machinery's vocabulary reaches the person or the chat
//     model, on any path.
//
// The end-to-end tests drive the real executor over a real git repository and a
// real `go test`, for the reason the audit tests do: "it was repaired in the
// same working copy and then it merged" is not a claim a stub can make.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// pricedText is a final answer that cost something, so a test can watch the
// node's bill grow across the workers and the checkers it took to land it.
func pricedText(text string, cost float64) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		response := textResponse(text)
		response.Usage.Cost = &cost
		return response, nil
	}
}

// nodeLane answers the node's own lane by WHAT THE REQUEST CONTAINS rather than
// by how many came before it.
//
// Three different agents speak on that lane and they interleave: the first run,
// the repair round, and the harness's own title call for each of them
// (title.go). A positional script over all of that would be a test asserting the
// order those happen to fall in, and it would break for the wrong reason the
// first time one of them takes an extra turn. So the fake answers the question
// it was actually asked: am I repairing, and have I written anything yet.
func nodeLane(turns int, run func(repairing, wrote bool) *ai.Response) []step {
	steps := make([]step, turns)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if len(messages) > 0 && strings.Contains(messageText(messages[0]), titleSystem) {
				return textResponse("the greeting task"), nil
			}
			repairing, wrote := false, false
			for _, message := range messages {
				if strings.Contains(messageText(message), repairHeading) {
					repairing = true
				}
				if message.Role == "tool" {
					wrote = true
				}
			}
			return run(repairing, wrote), nil
		}
	}
	return steps
}

// writeResponse and pricedResponse are [writeCall] and [pricedText] as the
// responses they build, for a lane that decides which one to send.
func writeResponse(id, path, content string) *ai.Response {
	arguments, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	return toolResponse(id, "write", string(arguments))
}

func pricedResponse(text string, cost float64) *ai.Response {
	response := textResponse(text)
	response.Usage.Cost = &cost
	return response
}

// childSaw reports whether ANY request on the node's own lane carried a piece
// of text. It is how a repair round's instruction is observed from the outside:
// the instruction sits in the child's context for every turn it takes, so one
// scan over the lane answers "was it ever told this".
func (c *routedCompleter) childSaw(needle string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, request := range c.childRequests {
		for _, message := range request {
			if strings.Contains(messageText(message), needle) {
				return true
			}
		}
	}
	return false
}

// childWorkTurns is how many times the node's lane was asked to WORK — the
// harness's own title call rides the same lane and is not a turn anybody took on
// the task. It is where "the work was handed back" and "it never was" are told
// apart: a repair round is a second worker on the same lane.
func (c *routedCompleter) childWorkTurns() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	turns := 0
	for _, request := range c.childRequests {
		if len(request) > 0 && strings.Contains(messageText(request[0]), titleSystem) {
			continue
		}
		turns++
	}
	return turns
}

// taskUpdatesUntilLanded collects every update one node sends, up to and
// including the one that lands it. It is [lastTaskUpdate] keeping the whole
// life rather than only the end, because what a running node said WHILE it was
// running is the thing under test here (TaskNotice.Mending).
func taskUpdatesUntilLanded(t *testing.T, updates <-chan Event, id uint64) []TaskNotice {
	t.Helper()
	var seen []TaskNotice
	deadline := time.After(30 * time.Second)
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatal("the task lane closed before the node landed")
			}
			if event.Task == nil || event.Task.ID != id {
				continue
			}
			seen = append(seen, *event.Task)
			if event.Task.State.settled() {
				return seen
			}
		case <-deadline:
			t.Fatalf("task %d never landed; %d updates seen", id, len(seen))
			return nil
		}
	}
}

// ── the repair loop ─────────────────────────────────────────────────────────

// WORK THAT CAME BACK SHORT IS FINISHED, NOT THROWN AWAY. The node writes the
// package and forgets the test the acceptance asked for; the checker runs `go
// test`, sees no test files, and says so; the SAME worktree gets a fresh worker
// with those words in its instruction; it writes the test; a fresh checker runs
// the same command and it passes. One node, one branch, one merge — and the
// bill for every agent it took is on that one node.
func TestRefutedWorkIsRepairedInPlaceAndLandsWhenItHolds(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and a test for it", "go test ./..."),
			finalText("handed off"),
		},
		child: nodeLane(8, func(repairing, wrote bool) *ai.Response {
			switch {
			// The repair round, in the same worktree: the missing half, and then
			// its own account of what it did.
			case repairing && !wrote:
				return writeResponse("call-test", "greet_test.go",
					"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n")
			case repairing:
				return pricedResponse("Added greet_test.go, which covers the greeting.", 0.02)
			// The first run: the package, and no test — the near miss this whole
			// loop exists for.
			case !wrote:
				return writeResponse("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n")
			default:
				return pricedResponse("Wrote greet.go with the greeting.", 0.01)
			}
		}),
		// Both checkers run the repository's own verification and answer on what
		// it printed, so the refutation and the verification are both real.
		audit: []step{
			bashCall("call-verify", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok",
				"REFUTED — go test ./... reports no test files: the acceptance asks for a test and there is none"),
			bashCall("call-verify-again", "go test ./..."),
			verdictFromEvidence("ok  \t",
				"VERIFIED — go test ./... ok · the greeting test runs",
				"REFUTED — go test ./... still reports no test files"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 1
	})
	graph := agent.graph()
	updates := agent.TaskUpdates()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q — the repaired work did not land", notice.State, notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, want merged", notice.Merge)
	}
	// BOTH HALVES CAME HOME, which is the whole claim: the repair round worked
	// where the first run had worked, so the branch carries one piece of work.
	for _, name := range []string{"greet.go", "greet_test.go"} {
		if _, err := os.Stat(filepath.Join(repo, name)); err != nil {
			t.Fatalf("%s is not on the person's branch after the repair: %v", name, err)
		}
	}
	if len(notice.Changed) != 2 {
		t.Fatalf("changed = %v, want both files the node wrote across its rounds", notice.Changed)
	}
	// ONE NODE. A repair round is not a second task, and nothing about it shows
	// up as one.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes in the graph, want 1: a repair round is the same node", count)
	}
	if _, err := os.Stat(filepath.Join(repo, ".aforge-v3", "tasks", "2")); !os.IsNotExist(err) {
		t.Fatal("a repair round opened a second working copy")
	}

	// THE WORKER WAS TOLD WHAT WAS MISSING, VERBATIM, AND THAT THE WORK STANDS.
	if !completer.childSaw(repairHeading) {
		t.Fatal("the repair round was not given the review's heading")
	}
	if !completer.childSaw("no test files") {
		t.Fatal("the repair round was not given the checker's own evidence")
	}
	if !completer.childSaw(repairStands) {
		t.Fatal("the repair round was not told the work so far stands")
	}
	if !completer.childSaw("write greet.go and a test for it") {
		t.Fatal("the repair round was not given the original brief")
	}

	// THE CHECKER WAS TOLD NOTHING. A second audit that knew it was looking at a
	// repair is a second audit with a reason to be satisfied.
	second := completer.auditAskedAt(2)
	if len(second) == 0 {
		t.Fatal("no second check ran after the repair round")
	}
	for _, message := range second {
		text := messageText(message)
		if strings.Contains(text, repairHeading) || strings.Contains(text, "no test files") ||
			strings.Contains(text, "round") {
			t.Fatalf("the second checker was primed with the repair: %q", text)
		}
	}

	// THE BILL IS THE NODE'S, ACROSS EVERY AGENT IT TOOK.
	if notice.CostUSD < 0.03 {
		t.Fatalf("the node cost %.4f, want at least the two workers' 0.03 accrued to one node", notice.CostUSD)
	}
	// And the report leads with the repaired work's own account, with the last
	// check's evidence in plain words under it.
	if !strings.HasPrefix(notice.Report, "Added greet_test.go") {
		t.Fatalf("report = %q, want the repair round's own account first", notice.Report)
	}
	if !strings.Contains(notice.Report, "go test ./... ok") {
		t.Fatalf("report = %q, want the evidence it landed on under the account", notice.Report)
	}

	// ── the surface's one line ──
	//
	// MENDING IS SET WHILE THE ROUND RUNS AND EMPTY EVERYWHERE ELSE, and every
	// update carrying it is a RUNNING one: nothing landed and nothing was undone.
	var mending []string
	for _, update := range taskUpdatesUntilLanded(t, updates, 1) {
		if update.Mending == "" {
			continue
		}
		if update.State != TaskRunning {
			t.Fatalf("a %s update carried Mending %q: the gap is only ever a running node's news", update.State, update.Mending)
		}
		mending = append(mending, update.Mending)
	}
	if len(mending) == 0 {
		t.Fatal("no update said what was being finished while the repair round ran")
	}
	for _, line := range mending {
		if !strings.Contains(line, "no test files") {
			t.Fatalf("Mending = %q, want the gap the checker named", line)
		}
		assertPlainWords(t, "the mending line", line)
	}
	if notice.Mending != "" {
		t.Fatalf("the landed node still says it is mending %q", notice.Mending)
	}
}

// A REPAIR ROUND IS NOT A SECOND CHANCE FOREVER. The work stays short, the
// rounds run out, and the node lands failed exactly as it did before the loop
// existed — except that the report now carries what was missing on EVERY round,
// which is the difference between a person who can act on it and one who cannot.
func TestWorkThatStaysShortLandsIncompleteWithEveryRoundsGaps(t *testing.T) {
	repo := newGoModuleRepo(t)
	writeFile(t, filepath.Join(repo, "hollow_test.go"),
		"package greet\n\nimport \"testing\"\n\nfunc TestHollow(t *testing.T) {\n\tt.Fatal(\"nothing was fixed\")\n}\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "failing")
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Fix the failing test", "make TestHollow pass"),
			finalText("handed off"),
		},
		child: []step{
			finalText("All done — the test passes now."),
			finalText("Had another look; I still think it passes."),
		},
		audit: []step{
			verdict("REFUTED — go test ./... still fails: TestHollow says nothing was fixed"),
			verdict("REFUTED — go test ./... still fails: TestHollow is untouched after the second pass"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 1
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "fix it"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, want failed once the rounds are spent (report %q)", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("report = %q, want it to lead with the plain word", notice.Report)
	}
	// EVERY ROUND'S GAPS, in the order they were found, told apart as rounds.
	if !strings.Contains(notice.Report, "nothing was fixed") {
		t.Fatalf("the first round's gap is missing from the report: %q", notice.Report)
	}
	if !strings.Contains(notice.Report, "untouched after the second pass") {
		t.Fatalf("the second round's gap is missing from the report: %q", notice.Report)
	}
	if !strings.Contains(notice.Report, repairedAgainLead) {
		t.Fatalf("the report does not read as two attempts: %q", notice.Report)
	}
	// The work is kept, exactly as a refuted node's always was.
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want aborted", notice.Merge)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatal("an incomplete node's branch was deleted: the work is gone")
	}
	// And the whole landing is in plain words, on all three surfaces.
	assertPlainLanding(t, notice, "task 1 · incomplete")

	// THE STEERING NOTE ASKS RATHER THAN SPENDS. The gaps and the branch are in
	// front of the model; what it must not do is start another task on its own.
	note := taskNote(notice, "", TaskSettleAsk, landingAddress{person: true})
	if !strings.Contains(note, "offer them a follow-up in their own words") {
		t.Fatalf("the note does not tell the model to ask: %s", note)
	}
}

// ROUNDS OFF IS YESTERDAY'S FRONTIER. With the row at 0 the first finding lands
// the node, nothing is handed back, and the node's lane is never asked a second
// time.
func TestRepairRoundsOffLandsTheFirstFinding(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go."),
		},
		audit: []step{
			verdict("REFUTED — the acceptance asks for a test and there is none"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, want failed with the loop off", notice.State)
	}
	if calls := completer.auditCalls(); calls != 1 {
		t.Fatalf("the check ran %d times, want 1: a verdict is never re-rolled", calls)
	}
	// Two turns is ONE run of the node — the write, then its last words. A repair
	// round would be a third.
	if turns := completer.childWorkTurns(); turns != 2 {
		t.Fatalf("the node's lane worked %d turns, want 2: nothing was handed back", turns)
	}
	if completer.childSaw(repairHeading) {
		t.Fatal("a repair round ran with the rounds set to 0")
	}
	if notice.Mending != "" {
		t.Fatalf("Mending = %q with the loop off", notice.Mending)
	}
	assertPlainLanding(t, notice, "the loop-off landing")
}

// ── the ladder ──────────────────────────────────────────────────────────────

// THE AUDITOR THAT STOPPED ONE WORD SHORT IS ASKED FOR THE WORD. It has already
// read the diff and run the verification; a fresh auditor would re-pay all of
// it. So the second call is a NUDGE on the same lane — it can see its own first
// reply — and the verdict it gives lands the node.
func TestNudgeGetsTheWordWithoutAFreshAuditor(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go."),
		},
		audit: []step{
			// The failure seen in the wild: an auditor that did the work and then
			// stopped mid-sentence.
			verdict("Let me be targeted:"),
			verdict("VERIFIED — go test ./... ok · 1 file"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q — the nudged word was not read", notice.State, notice.Report)
	}
	if calls := completer.auditCalls(); calls != 2 {
		t.Fatalf("the checker was asked %d times, want 2: the nudge, and nothing after it", calls)
	}
	// THE SECOND CALL IS THE SAME AUDITOR: its own first reply is in the context,
	// which is exactly the investigation a fresh one would have had to buy again.
	nudged := completer.auditAskedAt(1)
	if len(nudged) == 0 {
		t.Fatal("there was no second call to read")
	}
	if !strings.Contains(messageText(nudged[len(nudged)-1]), auditNudge) {
		t.Fatalf("the second call did not demand the word: %q", messageText(nudged[len(nudged)-1]))
	}
	same := false
	for _, message := range nudged {
		if strings.Contains(messageText(message), "Let me be targeted:") {
			same = true
		}
	}
	if !same {
		t.Fatal("the second call went to a fresh auditor: the whole investigation was re-paid")
	}
	assertPlainLanding(t, notice, "a nudged landing")
}

// A PROVIDER THAT NEVER DELIVERED HAS NOBODY TO NUDGE. The first rung is for a
// reply that did not parse; a call that did not land skips straight to the rung
// that builds a new auditor, which is what it did before the nudge existed.
func TestAProviderErrorSkipsTheNudge(t *testing.T) {
	verdict := parseAuditVerdict("")
	if verdict.answered {
		t.Fatal("an empty reply parsed as an answer")
	}
	// The distinction is drawn in auditOnce on the turn's own EventError, and
	// TestAuditProviderErrorLandsUnverified is where it is watched end to end:
	// two calls, not four, for an auditor whose provider never answered.
}

// ── the vocabulary law ──────────────────────────────────────────────────────

// machineryVocabulary is the list the law names, and the two literals it
// exempts. It lives in the test rather than in the package because it is an
// ASSERTION about the package, not a thing the package consults: a build that
// grew a fourth landing would have to add its wording here to pass, which is the
// whole point of writing the law down as a test.
var machineryVocabulary = []string{
	"auditor", "audit", "verdict", "VERIFIED", "REFUTED", "unverified",
}

// The two handles a person or a model may have to TYPE. They are addresses, not
// findings (task_audit.go's vocabulary law), so they are removed before the scan
// rather than treated as violations.
var machineryHandles = []string{"task.audit", "reaudit"}

// assertPlainWords holds one string to the law.
func assertPlainWords(t *testing.T, what, text string) {
	t.Helper()
	scanned := text
	for _, handle := range machineryHandles {
		scanned = strings.ReplaceAll(scanned, handle, "")
	}
	lowered := strings.ToLower(scanned)
	for _, word := range machineryVocabulary {
		if strings.Contains(lowered, strings.ToLower(word)) {
			t.Fatalf("%s says %q, which is the harness's own vocabulary and not the person's:\n%s", what, word, text)
		}
	}
}

// assertPlainLanding holds ALL THREE SURFACES of one landing to the law: the row
// this project's index keeps, the report on the notice a surface draws, and the
// note the chat model reads off the steering lane.
func assertPlainLanding(t *testing.T, notice TaskNotice, what string) {
	t.Helper()
	assertPlainWords(t, what+" · the index row's outcome", taskOutcome(notice.Report))
	assertPlainWords(t, what+" · the notice's report", notice.Report)
	assertPlainWords(t, what+" · the steering note", taskNote(notice, "file:///tmp/task.jsonl", TaskSettleAsk, landingAddress{person: true}))
}

// NO PATH TO A PERSON CARRIES THE MACHINERY'S WORDS.
//
// Every landing this build can reach is composed here from the same functions
// the executor composes it from — including the cases where the CHECKER's own
// text is full of the vocabulary, which is the case a translation layer has to
// survive — and all three surfaces are read for every banned word.
func TestNoMachineryWordReachesAPersonOnAnyPath(t *testing.T) {
	// Verdicts as they really arrive, parsed by the real parser, with the
	// machinery in the evidence where a model actually puts it.
	held := parseAuditVerdict("VERIFIED — the audit ran go test ./... and it is ok · 3 files")
	shortFirst := parseAuditVerdict("REFUTED — the report covers 10 companies; the acceptance asks for 11 and amp-labs is missing")
	shortAgain := parseAuditVerdict("REFUTED — amp-labs is there now, but the audit shows no revenue figure for it")
	essay := parseAuditVerdict("I read the diff and honestly it looks VERIFIED to me, but I would not swear to it.")

	for _, landing := range []struct {
		what   string
		state  TaskState
		report string
	}{
		{"a node that holds", TaskDone, held.doneOutcome()},
		{"a node that came back short once", TaskFailed, gapsOutcome([][]string{shortFirst.evidence})},
		{"a node that came back short twice", TaskFailed, gapsOutcome([][]string{shortFirst.evidence, shortAgain.evidence})},
		{"a node with a finding and no evidence", TaskFailed, gapsOutcome(nil)},
		{"a node cut off mid-check", TaskUnverified, withReport(taskCutMidCheck, held.checkedSoFar())},
		{"a node nobody could judge", TaskUnverified, essay.lookOutcome(TaskFacts{})},
		{"a node nobody could judge twice", TaskUnverified, essay.twice().lookOutcome(TaskFacts{})},
		{"a node with no answer at all", TaskUnverified, auditVerdict{}.lookOutcome(TaskFacts{})},
		{"a node a person accepted", TaskDone, acceptedLine("I read the diff myself and it holds", TaskAskOwnerPerson)},
		{"a node a person turned down", TaskFailed, refutedLine("it is missing the eleventh company", TaskAskOwnerPerson)},
		{"a node nothing checked", TaskDone, "nothing checked this work: the task.audit setting is off"},
		{"a node that ran out of time", TaskFailed, "ran out of time"},
		{"a node somebody stopped", TaskFailed, "stopped before it finished"},
	} {
		notice := TaskNotice{
			ID: 4, Title: "Research the eleven companies", State: landing.state,
			Report: landing.report, Branch: "task/research", Merge: mergeAborted,
		}
		assertPlainLanding(t, notice, landing.what)
	}

	// AND THE PLAIN VERSION STILL SAYS WHAT HAPPENED. Stripping the machinery
	// must not strip the facts with it — a card that said nothing would pass a
	// word test and fail the person.
	gaps := gapsOutcome([][]string{shortFirst.evidence, shortAgain.evidence})
	for _, fact := range []string{"amp-labs", "10 companies", "no revenue figure", incompleteLead, repairedAgainLead} {
		if !strings.Contains(gaps, fact) {
			t.Fatalf("the plain gaps lost %q:\n%s", fact, gaps)
		}
	}
	if outcome := held.doneOutcome(); !strings.Contains(outcome, "go test ./... and it is ok") {
		t.Fatalf("the plain evidence lost what was run: %q", outcome)
	}
	if look := essay.lookOutcome(TaskFacts{}); !strings.Contains(look, "I read the diff") {
		t.Fatalf("the plain non-answer lost what the checker said: %q", look)
	}
}

// THE TRANSLATION IS A NET UNDER THE LAW, and it translates rather than deletes:
// a sentence with a hole in it is not plainer than one with a machinery word in
// it, it is just less true.
func TestPlainWordsTranslatesRatherThanDeletes(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"the auditor ran go test", "the checker ran go test"},
		{"the AUDIT shows nothing", "the check shows nothing"},
		{"REFUTED: it does not build", "not confirmed: it does not build"},
		{"this is UNVERIFIED", "this is unchecked"},
		{"the verdict was VERIFIED", "the answer was confirmed"},
		{"nothing to translate here", "nothing to translate here"},
	} {
		if got := plainWords(c.raw); got != c.want {
			t.Fatalf("plainWords(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
	// The long stem goes first, or "unverified" becomes "un" plus something else
	// entirely.
	if got := plainWords("unverified"); strings.Contains(got, "confirmed") {
		t.Fatalf("plainWords ate the stem: %q", got)
	}
}

// ── the belt ────────────────────────────────────────────────────────────────

// LOOKING AROUND IS NOT MUTATING, AND A REFUSAL POINTS AT THE HANDS THAT CAN.
// The audit that died in the wild spent a step on a refused `pwd` and then went
// looking with the wrong tool; both halves of that are fixed here.
func TestAuditBeltAllowsOrientationAndSaysWhereToLook(t *testing.T) {
	for _, allowed := range []string{
		"pwd", "wc -l greet.go", "head -n 40 greet.go", "cat go.mod",
	} {
		if refusal, ok := auditRefusal(allowed, auditReadCommands); !ok {
			t.Fatalf("the checker may not run %q, which only reads: %s", allowed, refusal)
		}
	}
	// And the belt has not opened: orientation is reading, not writing.
	for _, refused := range []string{"tee out.txt", "sed -i s/a/b/ greet.go", "curl example.com", "cp a b"} {
		if _, ok := auditRefusal(refused, auditReadCommands); ok {
			t.Fatalf("the checker was allowed to run %q", refused)
		}
	}
	// EVERY REFUSAL POINTS AT THE READERS. A no that does not say where to go
	// costs another step, and the wrong reach happens because bash is what a
	// shell is for everywhere else.
	for _, refused := range []string{"", "ls /home", "go test ./... && rm -rf ."} {
		refusal, ok := auditRefusal(refused, auditReadCommands)
		if ok {
			t.Fatalf("%q was allowed", refused)
		}
		for _, hand := range []string{"read", "grep", "find", "ls"} {
			if !strings.Contains(refusal, hand) {
				t.Fatalf("the refusal of %q does not point at %s:\n%s", refused, hand, refusal)
			}
		}
		if !strings.Contains(refusal, "bash is only for verification commands") {
			t.Fatalf("the refusal of %q does not say what bash is for:\n%s", refused, refusal)
		}
	}
}

// ONE ANSWER CANNOT EAT THE REPLY BUDGET. A huge result is cut at the belt and
// the rest stays reachable through the same read hand.
func TestAnOversizedToolResultIsCutAtTheAuditBelt(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("a line of a very long file\n", 4000)
	writeFile(t, filepath.Join(dir, "huge.txt"), huge)
	if len(huge) <= auditResultLimit {
		t.Fatalf("the fixture is %d bytes, which is not oversized", len(huge))
	}

	byName := map[string]func(context.Context, json.RawMessage) (string, bool, error){}
	for _, tool := range auditBelt(dir, plainDoor(auditReadCommands), Place{Dir: t.TempDir()}) {
		byName[tool.Name] = tool.Execute
	}
	arguments := json.RawMessage(`{"command":` + strconv.Quote("cat huge.txt") + `}`)
	text, _, err := byName["bash"](context.Background(), arguments)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if len(text) > auditResultLimit+300 {
		t.Fatalf("the result was %d bytes, want it cut near %d", len(text), auditResultLimit)
	}
	if !strings.Contains(text, "whole output:") || !strings.Contains(text, "use read with offset/limit") {
		t.Fatalf("the cut does not provide an actionable continuation:\n%.200q", text)
	}
	// A result that FITS is handed over untouched: the cap is a ceiling, not a
	// rewrite.
	small := filepath.Join(dir, "small.txt")
	writeFile(t, small, "one line\n")
	text, _, err = byName["read"](context.Background(), json.RawMessage(`{"path":`+strconv.Quote(small)+`}`))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(text, "whole output:") {
		t.Fatalf("a small result was marked as cut:\n%q", text)
	}
}

// EVERY KIND OF RESULT USES THE SAME FILE READER. A non-file tool's long
// prose/data/code output is filed through the canonical stub path, then read
// with ordinary line offsets; there is no second result protocol to learn.
func TestABoundedResultCanBeReadPastItsFirstFragment(t *testing.T) {
	workspace := t.TempDir()
	droppings := Place{Dir: t.TempDir()}
	wantLate := "func relevantFinding() { return true }"
	whole := "source: reports/quarterly review.sql\n"
	for index := range 300 {
		whole += strings.Repeat("界,prose,data;", 6) + strconv.Itoa(index) + "\n"
	}
	whole += wantLate
	result := boundedResult(bare.Tool{
		Name:    "grep",
		Execute: func(context.Context, json.RawMessage) (string, bool, error) { return whole, false, nil },
	}, droppings, workspace)
	first, isError, err := result.Execute(context.Background(), nil)
	if err != nil || isError || len(first) > auditResultLimit {
		t.Fatalf("large result = (%d bytes, error %v, %v)", len(first), isError, err)
	}
	lead := "whole output: "
	start := strings.Index(first, lead)
	end := strings.Index(first[start+len(lead):], " — use read")
	if start < 0 || end < 0 {
		t.Fatalf("result has no ordinary file pointer:\n%s", first)
	}
	pointer := first[start+len(lead) : start+len(lead)+end]
	var reader bare.Tool
	for _, tool := range auditBelt(workspace, plainDoor(auditReadCommands), droppings) {
		if tool.Name == "read" {
			reader = tool
		}
	}
	page, pageError, pageErr := reader.Execute(context.Background(), json.RawMessage(`{"path":`+strconv.Quote(pointer)+`}`))
	if pageErr != nil || pageError || len(page) > auditResultLimit {
		t.Fatalf("first file page = (%d bytes, error %v, %v)", len(page), pageError, pageErr)
	}
	if !strings.Contains(page, "reports/quarterly review.sql") || !strings.Contains(page, "Use offset=") {
		t.Fatalf("first file page lost its path or continuation:\n%s", page)
	}
	for offset := 2; offset <= 300; offset++ {
		page, pageError, pageErr = reader.Execute(context.Background(), json.RawMessage(`{"path":`+strconv.Quote(pointer)+`,"offset":`+strconv.Itoa(offset)+`}`))
		if pageErr != nil || pageError || len(page) > auditResultLimit {
			t.Fatalf("page at line %d = (%d bytes, error %v, %v)", offset, len(page), pageError, pageErr)
		}
		if offset == 2 && strings.HasPrefix(page, "source: reports/") {
			t.Fatal("offset=2 repeated the first line")
		}
		if strings.Contains(page, wantLate) {
			return
		}
		if marker := strings.LastIndex(page, "Use offset="); marker >= 0 {
			digits := strings.TrimSpace(strings.TrimSuffix(page[marker+len("Use offset="):], "to continue.]"))
			if next, parseErr := strconv.Atoi(digits); parseErr == nil {
				offset = next - 1
			}
		}
	}
	t.Fatalf("late code was not reachable through %s", pointer)
}

func TestABoundedErrorKeepsItsMeaning(t *testing.T) {
	tool := boundedResult(bare.Tool{
		Name: "ls",
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return "permission denied: private data\n" + strings.Repeat("detail\n", 2000), true, nil
		},
	}, Place{Dir: t.TempDir()}, t.TempDir())
	text, isError, err := tool.Execute(context.Background(), nil)
	if err != nil || !isError {
		t.Fatalf("bounded error = (%q, %v, %v)", text, isError, err)
	}
	if !strings.HasPrefix(text, "permission denied: private data") || len(text) > auditResultLimit {
		t.Fatalf("bounded error lost its cause or bound: %d bytes, %.80q", len(text), text)
	}
	if !strings.Contains(text, "whole output:") {
		t.Fatalf("bounded error has no full-output path: %.200q", text)
	}
}

// A failed filing must leave an honest way to recover narrower evidence.
func TestABoundedResultExplainsWhenTheFullOutputCannotBeSaved(t *testing.T) {
	workspace := t.TempDir()
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, isFailure := range []bool{false, true} {
		tool := boundedResult(bare.Tool{Name: "grep", Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return strings.Repeat("observed evidence\n", 2000), isFailure, nil
		}}, Place{Dir: blocked}, workspace)
		text, isError, err := tool.Execute(context.Background(), nil)
		if err != nil || isError != isFailure || len(text) > auditResultLimit {
			t.Fatalf("result = (%d bytes, %v, %v)", len(text), isError, err)
		}
		if !strings.Contains(text, "full output could not be saved") || !strings.Contains(text, "ask for a narrower") || strings.Contains(text, "whole output:") {
			t.Fatalf("missing honest recovery: %s", text)
		}
	}
}
