package head

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestClassSelectorRecognitionTable(t *testing.T) {
	tests := []struct {
		message string
		class   string
		scope   string
		sweep   bool
		want    bool
	}{
		{message: "cancel the queued ones", class: classQueued, want: true},
		{message: "cancel the queued tasks", class: classQueued, want: true},
		{message: "cancel the waiting jobs", class: classQueued, want: true},
		{message: "cancel the queued finance tasks", class: classQueued, scope: "finance", want: true},
		{message: "restart the failed ones", class: classFailed, want: true},
		{message: "pause the running ones", class: classRunning, want: true},
		{message: "cancel everything", class: classAll, sweep: true, want: true},
		{message: "cancel all of them", class: classAll, want: true},
		{message: "cancel the rest", class: classAll, want: true},
		{message: "cancel the tasks", class: classAll, want: true},
		// Singular references still mean one thing the user has in mind.
		{message: "cancel the audio job", want: false},
		{message: "restart the failed one", want: false},
		{message: "stop that", want: false},
		{message: "pause it", want: false},
		{message: "do the tests first", want: false},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			class, ok := classSelector(test.message)
			if ok != test.want {
				t.Fatalf("classSelector(%q) = %+v %t, want %t", test.message, class, ok, test.want)
			}
			if !ok {
				return
			}
			if class.Class != test.class || class.Scope != test.scope || class.Sweeping != test.sweep {
				t.Fatalf("class = %+v, want class=%s scope=%q sweeping=%t",
					class, test.class, test.scope, test.sweep)
			}
		})
	}
}

// The live failure, verbatim. Both sentences named fourteen queued tasks; one
// cancelled a single unrelated node and the other found nothing at all.
//
// The reading that catches them is unchanged and so is the set it resolves; what
// changed is its authority. It no longer answers — it is rendered into the
// prompt as evidence above the message (hints.go), and the set is acted on by
// the control tool, which applies the same unit rule and the same consent gate
// the class path applies. So this test proves both halves: the sentence is READ
// as a set named by status, and acting on that set still asks once, still
// journals one ordinary command per unit, and still leaves the running leaf that
// nobody named alone.
func TestLiveFailureMessagesResolveTheWholeQueuedSet(t *testing.T) {
	for _, message := range []string{"cancel the queued ones", "cancel the queued tasks"} {
		t.Run(message, func(t *testing.T) {
			graph := openHeadStore(t)
			seedMixedBoard(t, graph)
			session := "live-" + strings.ReplaceAll(message, " ", "-")
			user := postUser(t, graph, session, message)

			reading := deterministicReading(t, New(nil, graph), user)
			if !strings.Contains(reading, "names a SET by status") ||
				!strings.Contains(reading, classQueued) {
				t.Fatalf("the loop was not told this names a queued set:\n%s", reading)
			}

			// What that reading resolves to, by filtering the board rather than by
			// ranking the words against whichever brief shares one with them.
			ids := queuedSetIDs(t, graph)
			if !equalTargets(ids, []string{"line-scan-b", "quiet-job", "quiet-job-two"}) {
				t.Fatalf("the queued set = %v, want the unstarted units only", ids)
			}

			head, client := beltHead(graph, beltTurn{calls: []ai.ToolCall{
				beltCall("c1", beltToolControl, map[string]any{"verb": "cancel", "ids": ids})}})
			if err := head.answer(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			if _, tooled := client.counts(); tooled == 0 {
				t.Fatal("the message never reached the loop")
			}
			reply := waitForAgentReply(t, graph, session, user.Seq)
			if !strings.Contains(reply.Body, `"kind":"confirm"`) {
				t.Fatalf("class set skipped its confirm: %q", reply.Body)
			}
			// The count is the whole reason a person wants to be asked, so it is
			// still in the question before anything moves.
			if !strings.Contains(reply.Body, "Cancel 5 tasks") {
				t.Fatalf("confirm did not count the set: %q", reply.Body)
			}
			if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
				t.Fatalf("the gate journaled before the answer: %+v", commands)
			}
			answer := postUser(t, graph, session, "1")
			if err := head.answer(context.Background(), answer); err != nil {
				t.Fatal(err)
			}
			targets := pendingTargets(t, graph, store.CommandCancel)
			want := []string{"line-scan-b", "quiet-job", "quiet-job-two"}
			if !equalTargets(targets, want) {
				t.Fatalf("cancelled targets = %v, want %v", targets, want)
			}
		})
	}
}

func TestOverGateClassSetAsksOnceAndKeepingCancelsNothing(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	user := postUser(t, graph, "keep", "cancel the queued ones")
	head, _ := beltHead(graph, beltTurn{calls: []ai.ToolCall{
		beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": queuedSetIDs(t, graph)})}})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, "keep", user.Seq)
	if len(reply.Options) != 2 || !strings.HasPrefix(reply.Options[1].Label, "keep ") {
		t.Fatalf("confirm options = %+v", reply.Options)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("set acted before its confirm: %+v", commands)
	}
	answer := postUser(t, graph, "keep", "2")
	if err := head.answer(context.Background(), answer); err != nil {
		t.Fatal(err)
	}
	if commands, _ := graph.PendingCommands(20); len(commands) != 0 {
		t.Fatalf("declined set still journaled commands: %+v", commands)
	}
	settled := waitForAgentReply(t, graph, "keep", answer.Seq)
	if !strings.Contains(settled.Body, "Keeping them") {
		t.Fatalf("decline receipt = %q", settled.Body)
	}
}

// A set small enough to be under every gate is simply done, and the receipt
// counts it and names it. The model said nothing after the tool acted, so what
// the person reads is assembled from what the tool reported and nothing else.
func TestUnderGateClassSetActsDirectlyWithCountingReceipt(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	spliceSurgeryJob(t, graph, "beta", "Beta report", "write the beta report")
	session := "small"
	user := postUser(t, graph, session, "cancel the queued ones")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"alpha", "beta"}})}},
		beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if strings.Contains(reply.Body, `"kind":"confirm"`) {
		t.Fatalf("small set asked anyway: %q", reply.Body)
	}
	if !strings.Contains(reply.Body, "Cancelling 2 tasks") ||
		!strings.Contains(reply.Body, "Alpha report") || !strings.Contains(reply.Body, "Beta report") {
		t.Fatalf("receipt does not count and name: %q", reply.Body)
	}
	if !equalTargets(pendingTargets(t, graph, store.CommandCancel), []string{"alpha", "beta"}) {
		t.Fatalf("small set targets = %v", pendingTargets(t, graph, store.CommandCancel))
	}
}

// The scope arm: "the queued line-scan tasks" is a set narrowed to one job, and
// the reading says which job in the user's own words. Acting on it may not reach
// a single node of anybody else's work.
func TestScopedClassSelectorTouchesOnlyThatJob(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	session := "scoped"
	user := postUser(t, graph, session, "cancel the queued line-scan tasks")

	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, `scoped to "line scan"`) {
		t.Fatalf("the reading dropped the scope the user named:\n%s", reading)
	}

	// The scope resolved against the board, and then the same status filter
	// inside it. The running sibling is not in the class and is not reached.
	set, err := New(nil, graph).classSet(classIntent{Class: classQueued}, "line-scan",
		[]store.Status{store.Pending})
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"line-scan-b"}) {
		t.Fatalf("scoped set = %v, want only the queued line-scan leaf", unitIDs(set))
	}

	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "cancel", "ids": unitIDs(set)}))
	if failed {
		t.Fatalf("scoped control refused: %s", result)
	}
	if run.confirm != nil {
		t.Fatalf("a scoped set of one asked anyway: %+v", run.confirm)
	}
	if !equalTargets(pendingTargets(t, graph, store.CommandCancel), []string{"line-scan-b"}) {
		t.Fatalf("scoped targets = %v, want only the queued line-scan leaf",
			pendingTargets(t, graph, store.CommandCancel))
	}
}

func TestRestartTheFailedOnesResolvesTheFailedSet(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	spliceSurgeryJob(t, graph, "beta", "Beta report", "write the beta report")
	spliceSurgeryJob(t, graph, "gamma", "Gamma report", "write the gamma report")
	failNode(t, graph, "alpha")
	failNode(t, graph, "beta")
	session := "failed"
	user := postUser(t, graph, session, "restart the failed ones")

	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "names a SET by status") || !strings.Contains(reading, classFailed) {
		t.Fatalf("the loop was not told this names the failed set:\n%s", reading)
	}
	set, err := New(nil, graph).classSet(classIntent{Class: classFailed}, "", []store.Status{store.Failed})
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"alpha", "beta"}) {
		t.Fatalf("failed set = %v, want the two that failed", unitIDs(set))
	}

	run := &beltRun{head: New(nil, graph), user: user}
	if result, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "restart", "ids": unitIDs(set)})); failed {
		t.Fatalf("restarting the failed set refused: %s", result)
	}
	if !equalTargets(pendingTargets(t, graph, store.CommandRestart), []string{"alpha", "beta"}) {
		t.Fatalf("restart targets = %v", pendingTargets(t, graph, store.CommandRestart))
	}
}

func TestQueuedClassLeavesRunningLeavesAndResidentInternalsAlone(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	set, err := New(&fakeClient{}, graph).classSet(
		classIntent{Class: classQueued}, "", []store.Status{store.Pending})
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"line-scan-b", "quiet-job", "quiet-job-two"}) {
		t.Fatalf("queued units = %v", unitIDs(set))
	}
	if set.Affected != 5 || set.Jobs != 3 {
		t.Fatalf("queued set affected/jobs = %d/%d, want 5/3", set.Affected, set.Jobs)
	}
}

func TestEverythingSweepsTheResidentsOwnWorkToo(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	set, err := New(&fakeClient{}, graph).classSet(
		classIntent{Class: classQueued, Sweeping: true}, "", []store.Status{store.Pending})
	if err != nil {
		t.Fatal(err)
	}
	if !containsTarget(unitIDs(set), "practice-job") {
		t.Fatalf("everything left practice behind: %v", unitIDs(set))
	}
}

// The unit rule arrived at from ids instead of from a status word. A verb that
// cannot legally touch what is running must reach the queued leaves of a job
// individually and leave the running one where it is — the identical safety
// property the class filter has, and the reason a set is safe to act on at all.
func TestTheUnitRuleNeverReachesThroughRunningWork(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	set, err := New(nil, graph).beltSet([]string{"line-scan"}, store.CommandReprioritize, true)
	if err != nil {
		t.Fatal(err)
	}
	if !equalTargets(unitIDs(set), []string{"line-scan-b"}) {
		t.Fatalf("units = %v, want only the leaf that has not started", unitIDs(set))
	}
	if set.Affected != 1 {
		t.Fatalf("affected = %d, want the one leaf the verb may touch", set.Affected)
	}
}

// A verb and a class that cannot be true at once. The old path said "I can't
// restart queued work" and stopped; the tool says the same thing to the loop and
// journals nothing, which is the half that matters — a refusal the model must
// speak to rather than a change nobody asked for.
func TestRestartRefusesAClassItCannotTouch(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	user := postUser(t, graph, "mismatch", "restart the queued ones")

	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "names a SET by status") || !strings.Contains(reading, classQueued) {
		t.Fatalf("the loop was not told this names the queued set:\n%s", reading)
	}
	if statuses := classStatuses(classIntent{Class: classQueued},
		surgeryAllowedStatuses(store.CommandRestart)); len(statuses) != 0 {
		t.Fatalf("restart claims it can touch queued work: %v", statuses)
	}

	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "restart", "ids": []string{"alpha"}}))
	if !failed || !strings.Contains(result, "restart only applies to failed or cancelled work") {
		t.Fatalf("restarting queued work was not refused: %q", result)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("impossible class still journaled commands: %+v", commands)
	}
}

// The other half of the live failure: a class word beside content that resolves
// to nothing confident. The verb knows what it wants to do and not what to do it
// to, and the answer is the candidates — never a stray match acted on silently.
func TestClassWordBesideContentAsksInsteadOfActing(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "line-scan", "Line scan", "scan the lines for defects")
	user := postUser(t, graph, "guard", "cancel the queued invoice reconciliation")

	// It is not a set — no plural marker — so the reading refuses to name one and
	// says so, which is what keeps the sentence off the class path entirely.
	if _, isClass := classSelector(user.Body); isClass {
		t.Fatal("a singular content reference was read as a set")
	}
	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "mentions a status word without naming a set") {
		t.Fatalf("the uncertain middle was not reported to the loop:\n%s", reading)
	}
	// And nothing is named on a coincidence of English: below the anchor floor
	// the overlap is not a reference, and offering one as though it were is the
	// whole of how a request about invoices cancelled a line scan.
	if strings.Contains(reading, "the words rank against") {
		t.Fatalf("a below-floor match was offered as the job they meant:\n%s", reading)
	}

	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "cancel", "describes": user.Body}))
	if failed && !strings.Contains(result, "matches") {
		t.Fatalf("a described cancel failed for the wrong reason: %q", result)
	}
	if strings.Contains(strings.ToLower(result), "cancelling") {
		t.Fatalf("a weak class-adjacent match acted silently: %q", result)
	}
	if run.acted {
		t.Fatalf("describing work changed something: %q", result)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("weak match journaled a command: %+v", commands)
	}
}

// And the confident reference is still simply done. One plausible match is not
// ambiguity: the reading ranks it, the id is in front of the loop, and a small
// cheap cancel costs the person no question at all.
func TestConfidentContentReferenceStillActsWithoutAsking(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "line-scan", "Line scan", "scan the lines for defects")
	spliceSurgeryJob(t, graph, "invoices", "Invoice reconciliation", "reconcile the invoices")
	user := postUser(t, graph, "plain", "cancel the invoice reconciliation")

	reading := deterministicReading(t, New(nil, graph), user)
	if !strings.Contains(reading, "the words rank against: invoices") {
		t.Fatalf("the reading did not put the job the words name in front of the loop:\n%s", reading)
	}

	run := &beltRun{head: New(nil, graph), user: user}
	result, failed := run.execute(beltToolControl, beltArguments(t, map[string]any{
		"verb": "cancel", "ids": []string{"invoices"}}))
	if failed {
		t.Fatalf("an ordinary cancel refused: %s", result)
	}
	if run.confirm != nil {
		t.Fatalf("ordinary surgery became chattier: %+v", run.confirm)
	}
	if !equalTargets(pendingTargets(t, graph, store.CommandCancel), []string{"invoices"}) {
		t.Fatalf("ordinary surgery landed elsewhere: %v", pendingTargets(t, graph, store.CommandCancel))
	}
}

// A set is journaled as one ordinary command per unit rather than as a new
// batched kind, so every replay path already knows the shape: rebuilding the
// graph from its events alone produces the same set, to the id.
func TestClassSurgeryReplaysThroughRebuild(t *testing.T) {
	path := t.TempDir() + "/graph.db"
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	spliceSurgeryJob(t, graph, "beta", "Beta report", "write the beta report")
	session := "replay"
	user := postUser(t, graph, session, "cancel the queued ones")
	head, _ := beltHead(graph,
		beltTurn{calls: []ai.ToolCall{beltCall("c1", beltToolControl, map[string]any{
			"verb": "cancel", "ids": []string{"alpha", "beta"}})}},
		beltTurn{})
	if err := head.answer(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	waitForAgentReply(t, graph, session, user.Seq)
	before := pendingTargets(t, graph, store.CommandCancel)
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after := pendingTargets(t, graph, store.CommandCancel)
	if !equalTargets(before, after) || len(after) != 2 {
		t.Fatalf("replay changed the set: before=%v after=%v", before, after)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
}

// seedMixedBoard is the board the live failure happened on: a job with one leaf
// running beside a queued sibling, two wholly-unstarted jobs, one finished job,
// and the resident's own practice work.
func seedMixedBoard(t *testing.T, graph *store.Store) {
	t.Helper()
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("line-scan", "", "Line scan", "scan the lines"),
		spec("line-scan-a", "line-scan", "Line scan pass one", "first pass"),
		spec("line-scan-b", "line-scan", "Line scan pass two", "second pass"))
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("quiet-job", "", "Quarterly summary", "summarize the quarter"),
		spec("quiet-job-a", "quiet-job", "Collect figures", "collect the figures"),
		spec("quiet-job-b", "quiet-job", "Write it up", "write it up"))
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("quiet-job-two", "", "Vendor review", "review the vendors"))
	spliceJobTree(t, graph, store.OriginUser, "",
		spec("done-job", "", "Old export", "export the old data"))
	spliceJobTree(t, graph, store.OriginSelf, store.PracticeGroup,
		spec("practice-job", "", "Practice question", "practice a question"))
	startNode(t, graph, "line-scan-a")
	completeNode(t, graph, "done-job")
}

// queuedSetIDs is the set "the queued ones" names, read off the board by
// filtering it. It is what the loop is pointed at by the reading and what the
// control tool is then given, so a test never invents an id the head could not
// have seen.
func queuedSetIDs(t *testing.T, graph *store.Store) []string {
	t.Helper()
	set, err := New(nil, graph).classSet(classIntent{Class: classQueued}, "",
		[]store.Status{store.Pending})
	if err != nil {
		t.Fatalf("read the queued set: %v", err)
	}
	return unitIDs(set)
}

// deterministicReading is what hints.go puts above the message in the prompt:
// every recognizer's reading of one sentence, marked as evidence. It is the seam
// that replaced the ladder, so a test that used to prove a cue fired proves the
// loop was told what the cue saw.
func deterministicReading(t *testing.T, head *Head, user store.Message) string {
	t.Helper()
	active, err := head.activeUserJobs()
	if err != nil {
		t.Fatalf("read live work: %v", err)
	}
	reading := head.renderHints(user, active)
	if reading == "" {
		t.Fatalf("no recognizer read %q at all", user.Body)
	}
	return reading
}

// beltHead is one head wired to a scripted loop. Every provider call the loop
// makes carries tools, so the turns are what it does and the client is proof it
// was reached at all.
func beltHead(graph *store.Store, turns ...beltTurn) (*Head, *beltClient) {
	client := &beltClient{turns: turns}
	return New(client, graph), client
}

// latestPrompt is the user message of the LAST call the loop made, where
// openingPrompt is the first. A turn that answers an earlier question is only
// honest if that exchange is in front of it, and that is a claim about this
// string rather than about the opening one.
func latestPrompt(client *beltClient) string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.seen) < 2 || len(client.seen[1].Content) == 0 {
		return ""
	}
	return client.seen[1].Content[0].Text
}

func spec(id, parent, title, brief string) store.NodeSpec {
	return store.NodeSpec{ID: id, Parent: parent, Title: title, Brief: brief, Stage: 1}
}

func spliceJobTree(t *testing.T, graph *store.Store, origin store.Origin, group string, nodes ...store.NodeSpec) {
	t.Helper()
	for index := range nodes {
		nodes[index].Group = group
	}
	provenance := store.Provenance{Origin: origin, Intent: nodes[0].Brief}
	if origin == store.OriginUser {
		provenance.SessionID = "surgery"
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: nodes}, provenance); err != nil {
		t.Fatalf("splice %s: %v", nodes[0].ID, err)
	}
}

func startNode(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "tester")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
}

func completeNode(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "tester")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func failNode(t *testing.T, graph *store.Store, id string) {
	t.Helper()
	claim, ok, err := graph.Claim(id, "tester")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	if err := graph.Fail(claim, "no reason"); err != nil {
		t.Fatalf("fail %s: %v", id, err)
	}
}

func postUser(t *testing.T, graph *store.Store, session, body string) store.Message {
	t.Helper()
	user, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleUser, Body: body})
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return user
}

func pendingCommandsOf(t *testing.T, graph *store.Store) []store.Command {
	t.Helper()
	commands, err := graph.PendingCommands(50)
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	return commands
}

func pendingTargets(t *testing.T, graph *store.Store, kind store.CommandKind) []string {
	t.Helper()
	commands, err := graph.PendingCommands(50)
	if err != nil {
		t.Fatalf("pending commands: %v", err)
	}
	targets := make([]string, 0, len(commands))
	for _, command := range commands {
		if command.Kind == kind {
			targets = append(targets, command.Target)
		}
	}
	sort.Strings(targets)
	return targets
}

func unitIDs(set classSet) []string {
	ids := make([]string, 0, len(set.Units))
	for _, node := range set.Units {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	return ids
}

func equalTargets(got, want []string) bool {
	return fmt.Sprint(got) == fmt.Sprint(want)
}

func containsTarget(got []string, want string) bool {
	for _, value := range got {
		if value == want {
			return true
		}
	}
	return false
}
