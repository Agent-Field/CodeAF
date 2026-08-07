package head

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
func TestLiveFailureMessagesResolveTheWholeQueuedSet(t *testing.T) {
	for _, message := range []string{"cancel the queued ones", "cancel the queued tasks"} {
		t.Run(message, func(t *testing.T) {
			graph := openHeadStore(t)
			seedMixedBoard(t, graph)
			session := "live-" + strings.ReplaceAll(message, " ", "-")
			head, user := askHead(t, graph, session, message)
			reply := waitForAgentReply(t, graph, session, user.Seq)
			if !strings.Contains(reply.Body, `"kind":"confirm"`) {
				t.Fatalf("class set skipped its confirm: %q", reply.Body)
			}
			if !strings.Contains(reply.Body, "Cancel 5 queued tasks") {
				t.Fatalf("confirm did not count the set: %q", reply.Body)
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
	head, user := askHead(t, graph, "keep", "cancel the queued ones")
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

func TestUnderGateClassSetActsDirectlyWithCountingReceipt(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	spliceSurgeryJob(t, graph, "beta", "Beta report", "write the beta report")
	session := "small"
	_, user := askHead(t, graph, session, "cancel the queued ones")
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if strings.Contains(reply.Body, `"kind":"confirm"`) {
		t.Fatalf("small set asked anyway: %q", reply.Body)
	}
	if !strings.Contains(reply.Body, "Cancelling 2 queued tasks") ||
		!strings.Contains(reply.Body, "Alpha report") || !strings.Contains(reply.Body, "Beta report") {
		t.Fatalf("receipt does not count and name: %q", reply.Body)
	}
	if !equalTargets(pendingTargets(t, graph, store.CommandCancel), []string{"alpha", "beta"}) {
		t.Fatalf("small set targets = %v", pendingTargets(t, graph, store.CommandCancel))
	}
}

func TestScopedClassSelectorTouchesOnlyThatJob(t *testing.T) {
	graph := openHeadStore(t)
	seedMixedBoard(t, graph)
	session := "scoped"
	_, user := askHead(t, graph, session, "cancel the queued line-scan tasks")
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if strings.Contains(reply.Body, `"kind":"confirm"`) {
		t.Fatalf("scoped set of one asked anyway: %q", reply.Body)
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
	_, user := askHead(t, graph, session, "restart the failed ones")
	waitForAgentReply(t, graph, session, user.Seq)
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

func TestSurgeryMatchDecisiveness(t *testing.T) {
	target := func(score float64) store.SurgeryTarget {
		return store.SurgeryTarget{Node: store.Node{ID: "n"}, Score: score}
	}
	tests := []struct {
		name    string
		matches []store.SurgeryTarget
		want    bool
	}{
		{"nothing found", nil, false},
		{"one weak match", []store.SurgeryTarget{target(ClassFallbackFloor - 0.01)}, false},
		{"one solid match", []store.SurgeryTarget{target(ClassFallbackFloor)}, true},
		{"several matches ask anyway", []store.SurgeryTarget{target(0.1), target(0.1)}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := surgeryMatchIsDecisive(test.matches); got != test.want {
				t.Fatalf("decisive = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRestartRefusesAClassItCannotTouch(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	session := "mismatch"
	_, user := askHead(t, graph, session, "restart the queued ones")
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.Contains(reply.Body, "can't restart queued work") {
		t.Fatalf("status mismatch reply = %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("impossible class still journaled commands: %+v", commands)
	}
}

// The other half of the live failure: a class word beside content that resolves
// to nothing confident must show the board rather than act on a stray match or
// flatly deny that any work exists.
func TestClassWordBesideContentAsksInsteadOfActing(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "line-scan", "Line scan", "scan the lines for defects")
	session := "guard"
	_, user := askHead(t, graph, session, "cancel the queued invoice reconciliation")
	reply := waitForAgentReply(t, graph, session, user.Seq)
	if !strings.HasPrefix(reply.Body, "Which job do you mean?") {
		t.Fatalf("weak class-adjacent match acted silently: %q", reply.Body)
	}
	if commands, _ := graph.PendingCommands(10); len(commands) != 0 {
		t.Fatalf("weak match journaled a command: %+v", commands)
	}
}

func TestConfidentContentReferenceStillActsWithoutAsking(t *testing.T) {
	graph := openHeadStore(t)
	spliceSurgeryJob(t, graph, "line-scan", "Line scan", "scan the lines for defects")
	spliceSurgeryJob(t, graph, "invoices", "Invoice reconciliation", "reconcile the invoices")
	session := "plain"
	_, user := askHead(t, graph, session, "cancel the invoice reconciliation")
	waitForAgentReply(t, graph, session, user.Seq)
	if !equalTargets(pendingTargets(t, graph, store.CommandCancel), []string{"invoices"}) {
		t.Fatalf("ordinary surgery became chattier: %v", pendingTargets(t, graph, store.CommandCancel))
	}
}

func TestClassSurgeryReplaysThroughRebuild(t *testing.T) {
	path := t.TempDir() + "/graph.db"
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	spliceSurgeryJob(t, graph, "alpha", "Alpha report", "write the alpha report")
	spliceSurgeryJob(t, graph, "beta", "Beta report", "write the beta report")
	session := "replay"
	_, user := askHead(t, graph, session, "cancel the queued ones")
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

func askHead(t *testing.T, graph *store.Store, session, body string) (*Head, store.Message) {
	t.Helper()
	user := postUser(t, graph, session, body)
	conversational := New(&fakeClient{}, graph)
	if err := conversational.answer(context.Background(), user); err != nil {
		t.Fatalf("answer %q: %v", body, err)
	}
	return conversational, user
}

func postUser(t *testing.T, graph *store.Store, session, body string) store.Message {
	t.Helper()
	user, err := graph.PostMessage(store.Message{SessionID: session, Role: store.RoleUser, Body: body})
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return user
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
