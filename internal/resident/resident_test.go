package resident

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestTickAppliesSpliceAndPostsCompiledReceipt(t *testing.T) {
	graph := openStore(t)
	instruction := "  Benchmark the parser without changing its output.  "
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-splice",
		Kind:        store.CommandSplice,
		Instruction: instruction,
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	compile := func(_ context.Context, got, graphContext string) (Compiled, error) {
		if got != instruction {
			return Compiled{}, fmt.Errorf("instruction = %q, want verbatim %q", got, instruction)
		}
		if !strings.Contains(graphContext, "root | Permanent Aforge spine | running") {
			return Compiled{}, fmt.Errorf("graph context omitted active root: %q", graphContext)
		}
		return Compiled{
			Goal: "Benchmark the parser and preserve observable output",
			Assumptions: []string{
				"main is the comparison baseline",
				"the existing benchmark harness is sufficient",
			},
			Scale: "project",
		}, nil
	}
	plan := func(ctx context.Context, compiled Compiled) (store.Subtree, error) {
		anchor, ok := PlanAnchorFromContext(ctx)
		wantNodeID := fmt.Sprintf("task-%d", command.Seq)
		if !ok || anchor.NodeID != wantNodeID || anchor.SessionID != command.SessionID ||
			anchor.CommandSeq != command.Seq {
			return store.Subtree{}, fmt.Errorf("plan anchor = %+v ok=%t", anchor, ok)
		}
		if !strings.HasPrefix(compiled.Goal, "Benchmark the parser and preserve observable output") {
			return store.Subtree{}, fmt.Errorf("goal = %q", compiled.Goal)
		}
		// The decisions the compiler already made reach the planner, which is
		// the only place a promise can still become a step someone runs.
		for _, decision := range []string{
			WorkingDecisionsHeader,
			"- main is the comparison baseline",
			"- the existing benchmark harness is sufficient",
		} {
			if !strings.Contains(compiled.Goal, decision) {
				return store.Subtree{}, fmt.Errorf("planned goal omitted %q: %q", decision, compiled.Goal)
			}
		}
		if compiled.Scale != "project" {
			return store.Subtree{}, fmt.Errorf("scale = %q, want project", compiled.Scale)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "benchmark", Brief: "Run the benchmark", Stage: 1},
			{ID: "compare", Parent: "benchmark", Brief: "Compare results", Stage: 2,
				Needs: []store.Need{{NodeID: "benchmark", Kind: store.FeedsInto}}},
		}}, nil
	}

	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandApplied || settled.Result != "spliced 2 nodes" {
		t.Fatalf("settled command = %+v", settled)
	}
	for _, id := range []string{"benchmark", "compare"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%v err=%v", id, ok, err)
		}
		if node.Provenance.Intent != instruction {
			t.Fatalf("node %q intent = %q, want verbatim %q", id, node.Provenance.Intent, instruction)
		}
		if node.Provenance.SessionID != "session-splice" || node.Provenance.Origin != store.OriginUser {
			t.Fatalf("node %q provenance = %+v", id, node.Provenance)
		}
	}

	// The deliverable owner carries the decisions durably: it writes the answer
	// the user reads, and it is the node the delivery gate holds to them.
	owner, _, err := graph.Node("benchmark")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(owner.Brief, WorkingDecisionsHeader) ||
		!strings.Contains(owner.Brief, "- main is the comparison baseline") {
		t.Fatalf("deliverable owner brief omitted the working decisions: %q", owner.Brief)
	}
	if part, _, err := graph.Node("compare"); err != nil || strings.Contains(part.Brief, WorkingDecisionsHeader) {
		t.Fatalf("an inner part was handed the decisions block: %q err=%v", part.Brief, err)
	}

	receipt := commandReceipt(t, graph, "session-splice", command.Seq)
	wantLines := []string{
		"Here's my reading: Benchmark the parser and preserve observable output",
		"Assumed: main is the comparison baseline",
		"Assumed: the existing benchmark harness is sufficient",
		"Correct me anytime — changing course costs nothing.",
	}
	for _, line := range wantLines {
		if !strings.Contains(receipt.Body, line) {
			t.Errorf("receipt %q does not contain %q", receipt.Body, line)
		}
	}
}

func TestTickUsesDefaultCompilerAndPlanner(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-default",
		Kind:        store.CommandSplice,
		Instruction: "Keep this request verbatim",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	id := fmt.Sprintf("task-%d", command.Seq)
	node, ok, err := graph.Node(id)
	if err != nil || !ok {
		t.Fatalf("default node %q: ok=%v err=%v", id, ok, err)
	}
	if node.Parent != store.RootID || node.Brief != command.Instruction || node.Stage != 1 {
		t.Fatalf("default node = %+v", node)
	}
	if node.Provenance.Intent != command.Instruction {
		t.Fatalf("intent = %q, want %q", node.Provenance.Intent, command.Instruction)
	}
	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandApplied || settled.Result != "spliced 1 nodes" {
		t.Fatalf("settled command = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if !strings.Contains(receipt.Body, "Here's my reading: "+command.Instruction) || strings.Contains(receipt.Body, "Assumed:") {
		t.Fatalf("default receipt = %q", receipt.Body)
	}
}

func TestTickCancelsPendingSubtreeAndReportsCounts(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "cancel-root", Brief: "Cancel this run", Stage: 1},
		{ID: "fetch", Parent: "cancel-root", Brief: "Fetch evidence", Stage: 2},
		{ID: "publish", Parent: "cancel-root", Brief: "Publish result", Stage: 3,
			Needs: []store.Need{{NodeID: "fetch", Kind: store.Blocks}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-cancel", Intent: "prepare a report"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "fetch", Cost: 0.85}); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-cancel",
		Kind:        store.CommandCancel,
		Target:      "cancel-root",
		Instruction: "stop this work",
	})
	if err != nil {
		t.Fatalf("request cancel: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	for _, id := range []string{"cancel-root", "fetch", "publish"} {
		node, ok, err := graph.Node(id)
		if err != nil || !ok {
			t.Fatalf("node %q: ok=%v err=%v", id, ok, err)
		}
		if node.Status != store.Cancelled || node.Error != "cancelled by user" {
			t.Errorf("cancelled node %q = status %s error %q", id, node.Status, node.Error)
		}
	}
	settled := commandBySeq(t, graph, command.Seq)
	const result = "cancelled 3; requested cooperative cancellation for 0"
	if settled.Status != store.CommandApplied || settled.Result != result {
		t.Fatalf("settled cancel = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if !strings.Contains(receipt.Body, "cancelled — 3 steps cancelled") ||
		!strings.Contains(receipt.Body, "$0.85 spent stays spent") || receipt.NodeID != "cancel-root" {
		t.Fatalf("cancel receipt = %q", receipt.Body)
	}
}

func TestTickAmendsPendingNodeAndPostsHonestReceipt(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "existing", Brief: "Existing work", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-amend", Intent: "do existing work"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-amend",
		Kind:        store.CommandAmend,
		Target:      "existing",
		Instruction: "change the output format",
	})
	if err != nil {
		t.Fatalf("request amend: %v", err)
	}

	if err := New(graph, nil, nil).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	settled := commandBySeq(t, graph, command.Seq)
	if settled.Status != store.CommandApplied || settled.Result != "amendment attached" {
		t.Fatalf("settled amend = %+v", settled)
	}
	receipt := commandReceipt(t, graph, command.SessionID, command.Seq)
	if !strings.Contains(receipt.Body, "amended — Existing work") || strings.Contains(receipt.Body, "next turn") {
		t.Fatalf("amend receipt = %q", receipt.Body)
	}
	node, found, err := graph.Node("existing")
	if err != nil || !found || !strings.Contains(node.Brief, "Amendment: change the output format") {
		t.Fatalf("amended node = %+v found=%t err=%v", node, found, err)
	}
}

func TestCompletionAnnouncementIsDeduplicatedAcrossRestart(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "land", Brief: "Write the report\nwith an appendix", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-watch", Intent: "write a report"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}

	first := New(graph, nil, nil)
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("initialize watcher: %v", err)
	}
	claim, won, err := graph.Claim("land", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "Report is in cas://report\nsecondary detail"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("announce completion: %v", err)
	}
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("repeat tick: %v", err)
	}

	messages, err := graph.Messages("session-watch", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages after repeated tick = %+v", messages)
	}
	// A deliverable owner (parented on the spine root) posts its whole
	// summary: that summary is the answer the user asked for, and a one-line
	// notice was measured to hide the result entirely.
	if messages[0].NodeID != "land" || messages[0].CommandSeq != 0 ||
		messages[0].Body != "Report is in cas://report\nsecondary detail" {
		t.Fatalf("announcement = %+v", messages[0])
	}

	restarted := New(graph, nil, nil)
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("restart tick: %v", err)
	}
	messages, err = graph.Messages("session-watch", 0, 0)
	if err != nil {
		t.Fatalf("messages after restart: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("restart re-announced history: %+v", messages)
	}
}

func TestDeferredResourceCompletionStaysInternalButStillSettles(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "deferred-partial", Brief: "finish all of it", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "deferred-session", Intent: "finish all of it"}); err != nil {
		t.Fatal(err)
	}
	distilled := make([]string, 0, 1)
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, goal, outcome string, failed bool) ([]Learned, error) {
			distilled = append(distilled, outcome)
			return nil, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("deferred-partial", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.DeferOverrun(store.DeferredOverrun{
		NodeID: "deferred-partial", Partial: "useful but unfinished", Prefix: "deferred-partial-x1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "useful but unfinished"); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	messages, err := graph.Messages("deferred-session", 0, 0)
	if err != nil || len(messages) != 0 {
		t.Fatalf("deferred partial announcements = %+v err=%v", messages, err)
	}
	// The split receipt owns the conversation, so nothing is announced — but the
	// partial is the only record of the most expensive work the system does, so
	// it is still distilled and its subtree is still folded.
	if len(distilled) != 1 || !strings.Contains(distilled[0], "useful but unfinished") {
		t.Fatalf("deferred partial distillations = %+v", distilled)
	}
	reconciler.now = func() time.Time { return time.Now().Add(2 * settledFoldGrace) }
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("deferred-partial")
	if err != nil || !ok || !node.Folded {
		t.Fatalf("deferred partial was not folded: node=%+v ok=%t err=%v", node, ok, err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := graph.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return graph
}

func commandBySeq(t *testing.T, graph *store.Store, seq int64) store.Command {
	t.Helper()
	command, ok, err := graph.CommandBySeq(seq)
	if err != nil || !ok {
		t.Fatalf("command %d: ok=%v err=%v", seq, ok, err)
	}
	return command
}

// commandReceipt finds the one filed line a command produced, and enforces
// 13.18's boundary on the way past.
//
// An ANCHORED receipt — applied surgery, an amendment that landed — is the work
// record's sentence: receiptAnchor has always said it "belongs on the job's own
// card", and the head said the thread's one sentence for it in its own voice
// before the command was even journaled. It carries a node and no session, and
// the room reads it by node. An UNANCHORED receipt has no card to live on, so
// the thread is the only place it can be read and it keeps its session.
//
// The read is therefore journal-wide, and sessionID is what the anchored half
// must NOT have.
func commandReceipt(t *testing.T, graph *store.Store, sessionID string, commandSeq int64) store.Message {
	t.Helper()
	messages, err := graph.Messages("", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	for _, message := range messages {
		if message.CommandSeq == commandSeq {
			// A phase row carries the same command seq and is not the receipt:
			// it says where the work has got to, and there are several of them
			// before the one line that says what was decided.
			if message.Progress != nil {
				continue
			}
			if message.Role != store.RoleSystem {
				t.Fatalf("command receipt role = %s", message.Role)
			}
			if message.NodeID != "" && message.SessionID != "" {
				t.Fatalf("an anchored receipt reached the thread: %+v", message)
			}
			if message.NodeID == "" && message.SessionID != sessionID {
				t.Fatalf("an unanchored receipt lost its room: %+v", message)
			}
			return message
		}
	}
	t.Fatalf("no receipt for command %d in %+v", commandSeq, messages)
	return store.Message{}
}

func TestTickReturnsCancelledContextWithoutTouchingCommand(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{Kind: store.CommandSplice, Instruction: "later"})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(graph, nil, nil).Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("tick error = %v, want context.Canceled", err)
	}
	if got := commandBySeq(t, graph, command.Seq).Status; got != store.CommandPending {
		t.Fatalf("command status = %s, want pending", got)
	}
}

func TestCompileReceiptKeepsLegacyBytes(t *testing.T) {
	got := compileReceipt("  Benchmark the parser  ", []string{
		"main is the baseline", "", "keep observable output",
	}, "")
	const want = "Here's my reading: Benchmark the parser\n" +
		"Assumed: main is the baseline\n" +
		"Assumed: keep observable output\n" +
		"Correct me anytime — changing course costs nothing."
	if got != want {
		t.Fatalf("compile receipt changed:\n got %q\nwant %q", got, want)
	}
	// A model the user named is one extra line, in their terms, between the
	// assumptions and the invitation to redirect.
	withModel := compileReceipt("Benchmark the parser", nil, "Running on google/gemini-3-pro.")
	const wantModel = "Here's my reading: Benchmark the parser\n" +
		"Running on google/gemini-3-pro.\n" +
		"Correct me anytime — changing course costs nothing."
	if withModel != wantModel {
		t.Fatalf("model receipt = %q, want %q", withModel, wantModel)
	}
	// A compile that supplied no reading of its own says so on the receipt,
	// after the model line and before the invitation: a goal that is quietly
	// the person's own words back is a substitution they could not see (#335).
	withNote := compileReceipt("Verbatim request:\nBenchmark the parser", nil,
		"Running on google/gemini-3-pro.", "The compiler supplied no reading of its own.")
	const wantNote = "Here's my reading: Verbatim request:\nBenchmark the parser\n" +
		"Running on google/gemini-3-pro.\n" +
		"The compiler supplied no reading of its own.\n" +
		"Correct me anytime — changing course costs nothing."
	if withNote != wantNote {
		t.Fatalf("note receipt = %q, want %q", withNote, wantNote)
	}
}

// Attached documents have to survive compilation twice over: the compiler sees
// them in its graph context, and the goal carries them even when a provider
// ignores that context. Losing them here would leave planning blind to the one
// input the user actually supplied.
func TestAttachedDocumentsReachTheCompilerContextAndTheCompiledGoal(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID:   "session-docs",
		Kind:        store.CommandSplice,
		Instruction: "Summarise the filing",
		Attachments: []string{"/drop/q3 filing.pdf", "/drop/chart.png", "/drop/q3 filing.pdf", "/drop/deck.pptx"},
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}

	seen := ""
	compile := func(_ context.Context, _, graphContext string) (Compiled, error) {
		seen = graphContext
		return Compiled{Goal: "Summarise the filing", Scale: "task"}, nil
	}
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		for _, want := range []string{"q3 filing.pdf", "deck.pptx", "read_document"} {
			if !strings.Contains(compiled.Goal, want) {
				return store.Subtree{}, fmt.Errorf("goal %q omitted %q", compiled.Goal, want)
			}
		}
		if strings.Contains(compiled.Goal, "chart.png") {
			return store.Subtree{}, fmt.Errorf("goal named an image attachment: %q", compiled.Goal)
		}
		if strings.Count(compiled.Goal, "q3 filing.pdf") != 1 {
			return store.Subtree{}, fmt.Errorf("goal repeated a document: %q", compiled.Goal)
		}
		return store.Subtree{Nodes: []store.NodeSpec{{ID: "summarise", Brief: compiled.Goal, Stage: 1}}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
		t.Fatalf("settled command = %+v", settled)
	}
	for _, want := range []string{"Attached documents", "q3 filing.pdf", "deck.pptx", "read_document"} {
		if !strings.Contains(seen, want) {
			t.Fatalf("compile context omitted %q: %q", want, seen)
		}
	}
	if strings.Contains(seen, "chart.png") {
		t.Fatalf("compile context described an image as a document: %q", seen)
	}
}

func TestPlainCommandsCarryNoDocumentPreamble(t *testing.T) {
	if got := attachedDocumentCompileContext([]string{"/drop/chart.png"}); got != "" {
		t.Fatalf("image-only compile context = %q", got)
	}
	if got := anchorAttachedDocuments("Ship the parser", nil); got != "Ship the parser" {
		t.Fatalf("unattached goal changed: %q", got)
	}
}

// The model a user named for a job has to reach the leaves, and the only
// carrier that survives planning, scheduling, and a rebuilt view is the node's
// own provenance. The receipt says so in their terms in the same breath.
func TestRequestedWorkModelRidesProvenanceAndTheCompileReceipt(t *testing.T) {
	graph := openStore(t)
	command, err := graph.RequestCommand(store.Command{
		SessionID: "session-model", Kind: store.CommandSplice,
		Instruction: "benchmark the parser with the gemini model",
	})
	if err != nil {
		t.Fatalf("request command: %v", err)
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{
			Goal: "Benchmark the parser.", Scale: "task",
			WorkModel: "google/gemini-3-pro", ModelNote: "Running on google/gemini-3-pro.",
		}, nil
	}
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: "bench", Brief: compiled.Goal, Stage: 1},
			{ID: "bench-leaf", Parent: "bench", Brief: "run it", Stage: 2},
		}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	for _, id := range []string{"bench", "bench-leaf"} {
		node, found, err := graph.Node(id)
		if err != nil || !found {
			t.Fatalf("read %s: found=%t err=%v", id, found, err)
		}
		if node.Provenance.WorkModel != "google/gemini-3-pro" {
			t.Fatalf("%s provenance = %+v", id, node.Provenance)
		}
	}
	messages, err := graph.Messages("session-model", 0, 0)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	receipt := ""
	for _, message := range messages {
		if message.CommandSeq == command.Seq && message.Role == store.RoleSystem {
			receipt = message.Body
		}
	}
	if !strings.Contains(receipt, "Running on google/gemini-3-pro.") {
		t.Fatalf("compile receipt = %q", receipt)
	}
}

// The other half of the adjacency reading, and the half that has to be real:
// when the head decides an ask that arrived beside a live job is genuinely new
// work, the splice names that job and the new work waits behind it. A label
// would not have prevented the failure — two jobs editing one repository at
// once is the outcome nothing downstream can repair — so this asserts the wait.
func TestSpliceNamingALiveJobWaitsBehindItRatherThanRacingIt(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "middleware", Brief: "add the gin logger middleware and push it", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "s1", Intent: "add request logging middleware",
	}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("middleware", "tester")
	if err != nil || !ok {
		t.Fatalf("claim middleware: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}

	command, err := graph.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandSplice, Target: "middleware",
		Instruction: "make sure you review the changes and check for bugs",
	})
	if err != nil {
		t.Fatal(err)
	}
	newID := fmt.Sprintf("task-%d", command.Seq)
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "Review the diff for regressions", Scale: "task"}, nil
	}
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		if len(compiled.BuildsOn) != 1 || compiled.BuildsOn[0] != "middleware" {
			return store.Subtree{}, fmt.Errorf("builds_on = %v, want the live job it arrived beside", compiled.BuildsOn)
		}
		return store.Subtree{Nodes: []store.NodeSpec{{ID: newID, Brief: compiled.Goal, Stage: 1}}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
		t.Fatalf("settled command = %+v", settled)
	}

	ready, err := graph.Ready(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range ready {
		if node.ID == newID {
			t.Fatalf("the new job is claimable while the job it continues is still running: %+v", ready)
		}
	}
	if _, claimable, err := graph.Claim(newID, "tester"); err != nil || claimable {
		t.Fatalf("the new job was claimable: claimable=%t err=%v", claimable, err)
	}
}

// A target that is over is not work to wait for. Continuity is a claim about
// something still moving; waiting on a finished job would only cost the splice
// the moment it was made.
func TestSpliceTargetThatIsNoLongerLiveCostsOnlyTheContinuity(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "settled", Brief: "the job that already finished", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "finish it"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("settled", "tester")
	if err != nil || !ok {
		t.Fatalf("claim settled: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatal(err)
	}
	command, err := graph.RequestCommand(store.Command{
		SessionID: "s1", Kind: store.CommandSplice, Target: "settled",
		Instruction: "write the summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	compile := func(context.Context, string, string) (Compiled, error) {
		return Compiled{Goal: "Write the summary", Scale: "task"}, nil
	}
	plan := func(_ context.Context, compiled Compiled) (store.Subtree, error) {
		if len(compiled.BuildsOn) != 0 {
			return store.Subtree{}, fmt.Errorf("builds_on = %v, want none", compiled.BuildsOn)
		}
		return store.Subtree{Nodes: []store.NodeSpec{
			{ID: fmt.Sprintf("task-%d", command.Seq), Brief: compiled.Goal, Stage: 1}}}, nil
	}
	if err := New(graph, compile, plan).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if settled := commandBySeq(t, graph, command.Seq); settled.Status != store.CommandApplied {
		t.Fatalf("settled command = %+v", settled)
	}
}

// A completion journaled while no reconciler was ticking is the routine case,
// not the exceptional one: every `aforge wake` builds a fresh reconciler, and
// the settle lane used to prime past everything that landed since the last one.
func TestSettlementResumesAcrossProcessBoundary(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "overnight", Brief: "Survey the field", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-gap", Intent: "survey the field"}); err != nil {
		t.Fatalf("splice fixture: %v", err)
	}
	first := New(graph, nil, nil)
	if err := first.Tick(context.Background()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	// The terminal closes here. Everything below happens with nothing ticking.
	claim, won, err := graph.Claim("overnight", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Complete(claim, "The field is surveyed."); err != nil {
		t.Fatalf("complete: %v", err)
	}

	distilled := 0
	restarted := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _, _ string, _ bool) ([]Learned, error) {
			distilled++
			return nil, nil
		})
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("restarted tick: %v", err)
	}

	messages, err := graph.Messages("session-gap", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Body != "The field is surveyed." {
		t.Fatalf("the gap was skipped: %+v", messages)
	}
	if distilled != 1 {
		t.Fatalf("distillations across the gap = %d", distilled)
	}
	// Folding is derived from graph state rather than from the tick that
	// announced the landing, so a third process still files the job once the
	// grace window has closed.
	filing := New(graph, nil, nil)
	filing.now = func() time.Time { return time.Now().Add(2 * settledFoldGrace) }
	if err := filing.Tick(context.Background()); err != nil {
		t.Fatalf("filing tick: %v", err)
	}
	node, ok, err := graph.Node("overnight")
	if err != nil || !ok || !node.Folded {
		t.Fatalf("job across the gap was not folded: node=%+v ok=%t err=%v", node, ok, err)
	}

	// The lane must not become a perpetual writer. Its own watermark event is
	// the last thing left to consume, and consuming that may not write another
	// one — otherwise the journal grows on every idle tick forever and no tick
	// can ever be quiet again.
	for range 2 {
		if err := restarted.Tick(context.Background()); err != nil {
			t.Fatalf("drain tick: %v", err)
		}
	}
	before, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := restarted.Tick(context.Background()); err != nil {
			t.Fatalf("idle tick: %v", err)
		}
	}
	after, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("idle ticks kept journaling: %d -> %d", before, after)
	}
}

// Consolidation retires and rewrites standing beliefs. A memory-only interval
// guard bought one more paid pass per restart.
func TestConsolidationIntervalSurvivesProcessBoundary(t *testing.T) {
	graph := openStore(t)
	recordScopeFacts(t, graph, "repo:restart", consolidationThreshold+1)
	passes := 0
	consolidate := func(_ context.Context, _ string, _ []store.Fact, _ *ScopePair) (Consolidation, error) {
		passes++
		return Consolidation{}, nil
	}
	first := New(graph, nil, nil).WithConsolidator(consolidate)
	first.consolidateNotebook(context.Background())
	if passes != 1 {
		t.Fatalf("first consolidation pass count = %d", passes)
	}
	restarted := New(graph, nil, nil).WithConsolidator(consolidate)
	restarted.consolidateNotebook(context.Background())
	if passes != 1 {
		t.Fatalf("restart bought another consolidation: %d", passes)
	}

	// The window is a window, not a lock: once it elapses the pass runs again.
	later := New(graph, nil, nil).WithConsolidator(consolidate)
	later.now = func() time.Time { return time.Now().Add(consolidationInterval + time.Minute) }
	later.consolidateNotebook(context.Background())
	if passes != 2 {
		t.Fatalf("elapsed interval did not consolidate: %d", passes)
	}
}

// A node spliced by a revision carries no session of its own; its failure used
// to be swallowed whole rather than interrupting the conversation that owns it.
func TestSessionlessChildFailureReachesTheJobSession(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "Ship the thing", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-revision", Intent: "ship the thing"}); err != nil {
		t.Fatalf("splice job: %v", err)
	}
	if err := graph.Splice("job", store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job-n9", Brief: "Check the deprecated API", Stage: 1},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "revision: the API changed"}); err != nil {
		t.Fatalf("splice revision node: %v", err)
	}
	reconciler := New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	claim, won, err := graph.Claim("job-n9", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%v err=%v", won, err)
	}
	if err := graph.Fail(claim, "the endpoint is gone"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("announce tick: %v", err)
	}
	messages, err := graph.Messages("session-revision", 0, 0)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 1 || !strings.Contains(messages[0].Body, "the endpoint is gone") {
		t.Fatalf("revision failure did not interrupt: %+v", messages)
	}
}

// A transient fault inside one pass must not end the loop and strand the lease.
func TestServeSurvivesTransientTickFailures(t *testing.T) {
	graph := openStore(t)
	reconciler := New(graph, nil, nil)
	failures := 0
	reconciler.standingWatchKeyPersist = func() (bool, string, error) {
		failures++
		return false, "", errors.New("provider unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err := reconciler.Serve(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("serve ended on a transient fault: %v", err)
	}
}

// The strongest correction signal in the system is what the user says while the
// work is still running. It reached the notebook as a boolean at best.
func TestDistillerSeesTheMidRunRedirect(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "table-job", Brief: "Summarise the numbers", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "redirect", Intent: "summarise the numbers"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	var outcomes []string
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, _, outcome string, _ bool) ([]Learned, error) {
			outcomes = append(outcomes, outcome)
			return nil, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(store.Command{
		SessionID: "redirect", Kind: store.CommandRedirect, Target: "table-job",
		Instruction: "no — a table, not prose",
	}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("table-job", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "Here is the table."); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || !strings.Contains(outcomes[0], "no — a table, not prose") {
		t.Fatalf("distiller never saw the redirect: %+v", outcomes)
	}
	if !strings.Contains(outcomes[0], "Record the standard, not the episode.") {
		t.Fatalf("redirect block lost its charge: %q", outcomes[0])
	}
}

// task-14 is not the owner of task-142's edges. A bare prefix match wrote
// another job's history into this one's distillation as durable evidence.
func TestContinuitySourcesRespectTheJobNamespace(t *testing.T) {
	graph := openStore(t)
	for _, id := range []string{"task-9", "task-14", "task-142"} {
		if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
			ID: id, Brief: "work " + id, Stage: 1,
		}}}, store.Provenance{Origin: store.OriginUser, SessionID: "namespace", Intent: "asked for " + id}); err != nil {
			t.Fatal(err)
		}
	}
	claim, won, err := graph.Claim("task-9", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "delivered task-9"); err != nil {
		t.Fatal(err)
	}
	if err := graph.AddEdge("task-9", "task-142", store.FeedsInto); err != nil {
		t.Fatal(err)
	}
	neighbour, found, err := graph.Node("task-14")
	if err != nil || !found {
		t.Fatalf("node task-14 found=%t err=%v", found, err)
	}
	if sources := New(graph, nil, nil).continuitySources(neighbour); sources != "" {
		t.Fatalf("task-14 claimed task-142's continuity: %q", sources)
	}
	continuation, found, err := graph.Node("task-142")
	if err != nil || !found {
		t.Fatalf("node task-142 found=%t err=%v", found, err)
	}
	if sources := New(graph, nil, nil).continuitySources(continuation); !strings.Contains(sources, "asked for task-9") {
		t.Fatalf("real continuity was lost: %q", sources)
	}
}
