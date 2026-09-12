package session

// nohiddenwork_test.go — THE LAWS OF "NOTHING HAPPENS ON THE TURN THAT THE TURN
// DID NOT ASK FOR".
//
// Each test here is one measured defect of 2026-09-11 written down as a rule, so
// that the next person who puts a `git status`, a whole-file write or a second
// timeout budget on a person's path is told by the build rather than by a
// census six weeks later.
//
// WHAT IT PINS IS THE THREE SHAPES THIS FILE'S OWN CHANGE INTRODUCED and not the
// package at large: the worktree reading taken beside the batch rather than
// inside it, the fix shelf's read path writing nothing, and the one door a hand
// on the belt asks a model through. The other deferred writes of the same wave
// are pinned where they live — the meta stamp by the title and meta-transaction
// tests, the working copy by standingtree_test.go, the memos by their own
// packages' tests.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/offpath"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the worktree reading ────────────────────────────────────────────────────

// stubDirtReading puts a recorder in front of the one reading of the worktree
// and hands back its count.
func stubDirtReading(t *testing.T, answer func(int) string) *atomic.Int64 {
	t.Helper()
	var readings atomic.Int64
	restore := worktreeDirtReading
	worktreeDirtReading = func(string) string {
		return answer(int(readings.Add(1)))
	}
	t.Cleanup(func() { worktreeDirtReading = restore })
	return &readings
}

func watchOn(dir string) *loopWatch {
	watch := newLoopWatch()
	watch.dir = dir
	return watch
}

// materialProgressFor is [loopWatch.materialProgress] with the lock its one
// caller already holds, so a test can ask the question directly.
func (w *loopWatch) materialProgressFor(calls []ai.ToolCall, results []toolResult) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.materialProgress(calls, results)
}

func toolBatch(names ...string) ([]ai.ToolCall, []toolResult) {
	calls := make([]ai.ToolCall, 0, len(names))
	results := make([]toolResult, 0, len(names))
	for index, name := range names {
		call := ai.ToolCall{ID: "call-" + name + string(rune('a'+index))}
		call.Function.Name = name
		call.Function.Arguments = `{"path":"notes.md"}`
		calls = append(calls, call)
		results = append(results, toolResult{text: "ok"})
	}
	return calls, results
}

// A BATCH THAT ONLY READ CANNOT HAVE MOVED THE TREE, so nothing is run to find
// out. This was already the rule ([loopWatch.materialProgress]); it is pinned
// here because it is the cheapest half of the fix and the easiest to lose.
func TestAReadOnlyBatchRunsNoWorktreeReading(t *testing.T) {
	readings := stubDirtReading(t, func(int) string { return "dirty" })
	watch := watchOn(t.TempDir())

	for range 3 {
		calls, results := toolBatch("read", "grep", "ls", "manual", "tasks")
		watch.observe(calls, results, false)
	}
	if got := readings.Load(); got != 0 {
		t.Fatalf("a batch of read-only hands ran the worktree reading %d times, want none", got)
	}
}

// A BATCH WITH A SHELL COMMAND IN IT TAKES EXACTLY ONE READING, and it takes it
// BESIDE the turn: the reading here is slower than the join, so the batch must
// come back without it rather than waiting the reading out.
func TestAShellBatchTakesOneReadingAndDoesNotWaitItOut(t *testing.T) {
	slow := make(chan struct{})
	readings := stubDirtReading(t, func(int) string {
		<-slow
		return "moved"
	})
	watch := watchOn(t.TempDir())

	calls, results := toolBatch("bash")
	began := time.Now()
	watch.observe(calls, results, false)
	waited := time.Since(began)
	close(slow)

	if waited > 20*worktreeDirtJoin {
		t.Fatalf("a batch waited %s on a worktree reading bounded at %s", waited, worktreeDirtJoin)
	}
	// The reading is still in flight and is folded by the NEXT batch rather than
	// started again, which is what "exactly one per batch" means when the tree is
	// too big to read inside the join.
	next, nextResults := toolBatch("bash")
	watch.observe(next, nextResults, false)
	if got := readings.Load(); got > 2 {
		t.Fatalf("two batches took %d worktree readings, want at most one each", got)
	}
}

// AND THE ANSWER IS UNCHANGED WHERE THE READING IS FAST, which is every ordinary
// project: the first reading is a baseline, a reading that differs is movement.
func TestAFastWorktreeReadingStillReportsMovement(t *testing.T) {
	answers := []string{"one", "one", "two"}
	stubDirtReading(t, func(nth int) string { return answers[min(nth, len(answers))-1] })
	watch := watchOn(t.TempDir())

	calls, results := toolBatch("bash")
	if watch.materialProgressFor(calls, results) {
		t.Fatal("the first reading of a worktree was reported as movement; it is a baseline")
	}
	if watch.materialProgressFor(calls, results) {
		t.Fatal("an unchanged worktree was reported as movement")
	}
	if !watch.materialProgressFor(calls, results) {
		t.Fatal("a worktree that changed was not reported as movement")
	}
}

// ── the fix shelf's read path ───────────────────────────────────────────────

// A READ PATH DOES NOT WRITE. Consulting the store for advice about a failed
// call used to persist two counters — four whole-file cycles per fail→fix→
// succeed iteration, in front of the person.
func TestConsultingTheFixStoreWritesNothingOnThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixes.json")
	store := newFixStore(path)

	// THE DEFERRED WRITE IS HELD WHILE THE READ PATH RUNS, and holding it is what
	// makes this a law rather than a race. [offpath.Write.Owe] starts its
	// goroutine at once and that goroutine is as entitled to the processor as this
	// one — so a bare stat after five consultations asks whether the scheduler got
	// there first, which on a loaded box it does. Held, the file can only exist if
	// the READ PATH ITSELF wrote it, which is the whole claim.
	release := make(chan struct{})
	store.countersOnce.Do(func() {
		store.countersWrite = offpath.Deferred(func() {
			<-release
			store.mu.Lock()
			defer store.mu.Unlock()
			store.saveLocked()
		})
	})

	for range 5 {
		store.consult("bash\x00no such file")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("consulting the store wrote its file on the read path")
	}
	close(release)

	// The counters are not lost: they land when the deferred write is settled,
	// which is the exit door and this test's door.
	store.settle()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the counters never landed: %v", err)
	}
	if !strings.Contains(string(raw), `"asked": 5`) {
		t.Fatalf("the settled file does not carry the five consultations:\n%s", raw)
	}
}

// ── one door for a hand asking a model ──────────────────────────────────────

// EVERY TOOL-MADE MODEL CALL GOES THROUGH ONE DOOR. The census of 2026-09-10
// could not attribute 2,830 finished calls in ten days because the hands that
// make them set no tag, and a tag is set either by the caller or not at all.
// This is a structural law rather than a runtime one because the defect is the
// EXISTENCE of a second send site, not its behaviour.
//
// A HAND IS A FILE ON THE BELT — tools_*.go — AND THAT IS THE WHOLE OF WHAT THIS
// WALKS. The package's other senders are not hands and must not be dragged
// through this door: auxiliary.go is the auxiliary ladder, which has a tagging
// door of its own; image.go's vision answer IS the person's own turn under
// another model, streaming into the room under [lane.RoleTalk]; checkpoint.go,
// guardian.go, spellout.go and the task shapers are the engine asking on its own
// account. A door built for "a tool call is waiting on this" would be the wrong
// bound and the wrong phase for every one of them.
func TestNoHandAsksAModelOutsideTheOneDoor(t *testing.T) {
	set := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "tools_") || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "completeWithModel" {
				return true
			}
			t.Errorf("%s:%d calls completeWithModel directly. A hand on the belt asks a model "+
				"through [Agent.askModel] (toolask.go), which is what tags the call, draws its "+
				"phase, bounds its answer and bills it", name, set.Position(call.Pos()).Line)
			return true
		})
	}
}

// AND THE DOOR NAMES THE HAND. A row in the model-call log is named by its tag,
// and `tool:<name>` is the one spelling.
func TestAHandsCallIsTaggedWithItsOwnName(t *testing.T) {
	for name, want := range map[string]string{
		"view_image": "tool:view_image",
		"read":       "tool:read",
		"":           "tool",
	} {
		if got := toolCallTag(name); got != want {
			t.Errorf("a call from %q is tagged %q, want %q", name, got, want)
		}
	}
}

// A HAND SENDS NO OUTPUT CEILING, and this is the law rather than an omission
// ([toolAskSendsNoOutputCeiling]). internal/lane's gate rules a machine out when
// its published MaxOut is below the request's max_tokens (frontier.go's
// `capable`), so a ceiling taken from the belt's result cap — an order of
// magnitude above every measured answer — would strike vision machines off the
// serving set of the very tool it was meant to speed up.
func TestAHandSendsNoOutputCeiling(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "toolask.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "WithMaxTokens" {
			t.Errorf("toolask.go:%d sends an output ceiling with a hand's question. "+
				"The belt's own cap is the only principled one and it does not bind; sending it "+
				"rules every machine with a smaller MaxOut off the serving set (frontier.go's "+
				"capable). The bound that matters is the role's window", set.Position(call.Pos()).Line)
		}
		return true
	})
	// And the reasoning is written down where the next person will look for it.
	if toolAskSendsNoOutputCeiling == "" {
		t.Fatal("the reason a hand sends no ceiling is not stated anywhere")
	}
}

// AND THE WAIT IS THE ROLE'S. There is no duration written down in the tools.
func TestAHandsAskIsAsImpatientAsThePersonWatchingIt(t *testing.T) {
	if lane.RoleTool.Ceiling() > lane.VisiblePatience {
		t.Fatalf("a hand's ask is acted on after %s, which is longer than a person is asked to "+
			"watch an empty row (%s)", lane.RoleTool.Ceiling(), lane.VisiblePatience)
	}
	if facts := lane.RoleTool.Facts(); facts.Visible {
		t.Fatalf("a hand's ask is drawn as visible (%+v); its answer is a tool result and nobody "+
			"reads it arriving", facts)
	}
	if !lane.RoleTool.Facts().Interactive {
		t.Fatal("a hand's ask is not interactive, so a second of it is priced as nobody's — " +
			"the person is watching the tool row it holds open")
	}
}

// ── the sense's phase ───────────────────────────────────────────────────────

// A PERSON WATCHING A `read` OF A PICTURE IS TOLD WHAT IS HAPPENING, in the
// words of the sense rather than of the file's extension.
func TestTheSenseSaysWhichSenseItIsUsing(t *testing.T) {
	for _, row := range []struct {
		kind  senseKind
		shown string
		want  string
	}{
		{senseImage, "shots/error.png", "looking at error.png"},
		{senseAudio, "memo.m4a", "listening to memo.m4a"},
		{senseVideo, "clips/run.mp4", "watching run.mp4"},
		{senseNone, "notes.md", ""},
		{sensePDF, "paper.pdf", ""},
	} {
		if got := senseDoing(row.kind, row.shown); got != row.want {
			t.Errorf("senseDoing(%v, %q) = %q, want %q", row.kind, row.shown, got, row.want)
		}
	}
}

// ── AND EVERY REQUEST SAYS WHAT IT IS FOR ───────────────────────────────────
//
// THE STRUCTURAL HALF OF clientdoor.go's [callPurpose]. The type already makes
// an anonymous call fail to compile; what the type cannot see is a caller that
// satisfies it by passing the empty string, which is the same anonymous request
// wearing the argument. Nine of the eleven callers of this door reached the
// wire with no tag before the purpose was an argument — the guardian, vision,
// the shaper, the spell-out, the intake, the planner, the designer, the handoff
// draft and a saved program's step — and 2,309 of 2,839 untagged finishes in
// ten days were this package's.
//
// SO THE LAW IS: A PURPOSE IS A WORD, NOT A BLANK. One caller is exempt and it
// is named rather than inferred — [modelRoutingCompleter], which is the view of
// a live agent that LEAVES this package, whose callers each name their own call
// and whose tag this door must not overwrite.
//
// AND THE TAG IS SPELLED IN ONE PLACE. [provider.WithCallTag] may be called by
// the door and by nothing else in this package: three callers used to stamp
// their own, which is three spellings of one fact and the reason the other nine
// could forget it existed.
//
// IT READS THE TREE ITSELF, so it runs on the laws gate of every pull request
// (scripts/laws.sh finds it by this import).
func TestEveryRequestThroughTheDoorSaysWhatItIsFor(t *testing.T) {
	set := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	// theDoors are the two spellings of the one door, and theWrapper is the one
	// caller that may pass an empty purpose.
	theDoors := map[string]bool{"completeWithModel": true, "completeWithNamedModel": true}
	const theWrapper = "modelRoutingCompleter"
	purposes := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// within is the receiver type of the function being walked, so the one
		// exemption is a property of where the call is rather than a file name.
		within := ""
		ast.Inspect(file, func(node ast.Node) bool {
			if decl, ok := node.(*ast.FuncDecl); ok {
				within = receiverTypeName(decl)
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "WithCallTag" && name != "clientdoor.go" {
				t.Errorf("%s:%d calls provider.WithCallTag. The tag is spelled by the one door "+
					"and nowhere else — pass a callPurpose to completeWithModel instead "+
					"(clientdoor.go)", name, set.Position(call.Pos()).Line)
				return true
			}
			if !theDoors[selector.Sel.Name] || len(call.Args) < 2 {
				return true
			}
			purposes++
			if blankPurpose(call.Args[1]) && within != theWrapper {
				t.Errorf("%s:%d reaches the wire with no purpose. Every request this package "+
					"makes says what it is FOR, because a tag that can be forgotten is a tag "+
					"that will be — and the call log cannot say what a build spent its night on "+
					"(clientdoor.go's [callPurpose])", name, set.Position(call.Pos()).Line)
			}
			return true
		})
	}
	// AND THE LAW IS READING THE TREE IT THINKS IT IS. A walk that matched
	// nothing would pass for ever, which is how a structural law rots.
	if purposes < 8 {
		t.Fatalf("only %d calls through the one door were found; the law is reading the wrong tree", purposes)
	}
}

// blankPurpose reports an argument that names no purpose: the empty literal, or
// a conversion of one.
func blankPurpose(arg ast.Expr) bool {
	switch node := arg.(type) {
	case *ast.BasicLit:
		return node.Kind == token.STRING && (node.Value == `""` || node.Value == "``")
	case *ast.CallExpr:
		// callPurpose("") is the same blank wearing its type.
		return len(node.Args) == 1 && blankPurpose(node.Args[0])
	}
	return false
}

// receiverTypeName is the bare type a method hangs off, and "" for a function.
func receiverTypeName(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return ""
	}
	expr := decl.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}
