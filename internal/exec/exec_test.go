package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	graphstore "github.com/Agent-Field/aforge-v2/internal/store"
)

func workspace(t *testing.T) *Workspace {
	t.Helper()
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return space
}

func TestRecallToolIsStoreGatedAndBounded(t *testing.T) {
	plain := NewToolbox(workspace(t), 1, nil)
	if definitions := plain.Definitions(); len(definitions) != 5 || definitions[1].Function.Name != "job" {
		t.Fatalf("plain toolbox definitions = %+v, want five universal tools including job", definitions)
	}
	if result := plain.Execute(context.Background(), "recall", `{"terms":"parser"}`); !result.IsError || !strings.Contains(result.Content, "without an attached store") {
		t.Fatalf("plain recall result = %+v, want unavailable", result)
	}

	history, err := graphstore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	for index := 0; index < 12; index++ {
		id := fmt.Sprintf("memory-%02d", index)
		intent := fmt.Sprintf("Repair chromatic parser %02d", index)
		if err := history.Splice(graphstore.RootID, graphstore.Subtree{Nodes: []graphstore.NodeSpec{{
			ID: id, Brief: intent, Stage: 1,
		}}}, graphstore.Provenance{Origin: graphstore.OriginUser, Intent: intent}); err != nil {
			t.Fatal(err)
		}
		claim, won, err := history.Claim(id, "worker")
		if err != nil || !won {
			t.Fatalf("claim %s: won=%v err=%v", id, won, err)
		}
		digest := fmt.Sprintf("memory %02d: %s", index, strings.Repeat("chromatic parser detail ", 220))
		if err := history.Complete(claim, digest); err != nil {
			t.Fatal(err)
		}
		if err := history.Fold(id, digest, []string{fmt.Sprintf("/workspace/parser/%02d.md", index)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := history.RecordFact("memory-11", "repo:/workspace/parser", graphstore.FactLesson,
		"The chromatic parser keeps sentinel values explicit"); err != nil {
		t.Fatal(err)
	}

	tools := NewToolboxWithStore(workspace(t), 2, nil, history)
	definitions := tools.Definitions()
	if len(definitions) != 6 || definitions[5].Function.Name != "recall" ||
		!strings.Contains(definitions[5].Function.Description, "map") ||
		!strings.Contains(definitions[5].Function.Description, "territory") {
		t.Fatalf("store toolbox definitions = %+v", definitions)
	}
	result := tools.Execute(context.Background(), "recall",
		`{"terms":"chromatic parser","scope_cues":["/workspace/parser"],"limit":10}`)
	if result.IsError {
		t.Fatalf("recall failed: %s", result.Content)
	}
	if len(result.Content) > maxRecallResultBytes {
		t.Fatalf("recall result = %d bytes, limit %d", len(result.Content), maxRecallResultBytes)
	}
	var decoded recallToolResponse
	if err := json.Unmarshal([]byte(result.Content), &decoded); err != nil {
		t.Fatalf("recall returned invalid JSON: %v\n%s", err, result.Content)
	}
	if len(decoded.Folds) == 0 || len(decoded.Folds[0].Pointers) == 0 ||
		!strings.HasPrefix(decoded.Folds[0].Pointers[0], "/workspace/parser/") {
		t.Fatalf("recall folds omitted bounded pointers: %+v", decoded.Folds)
	}
	if len(decoded.Notebook) != 1 || !strings.Contains(decoded.Notebook[0].Body, "sentinel") ||
		len(decoded.Notebook[0].Pointers) == 0 || decoded.Notebook[0].Pointers[0] != "/workspace/parser/11.md" {
		t.Fatalf("recall notebook = %+v", decoded.Notebook)
	}
}

func TestShPrependsSkillPathOnlyWithStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin, err := graphstore.SkillsBinDir()
	if err != nil {
		t.Fatal(err)
	}
	command := `{"cmd":"printf '%s' \"$PATH\""}`

	plain := NewToolbox(workspace(t), 1, nil)
	plainResult := plain.Execute(context.Background(), "sh", command)
	if plainResult.IsError {
		t.Fatalf("plain sh: %s", plainResult.Content)
	}
	if strings.Split(plainResult.Content, string(os.PathListSeparator))[0] == bin {
		t.Fatalf("no-store PATH unexpectedly starts with skill bin: %q", plainResult.Content)
	}

	history, err := graphstore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	attached := NewToolboxWithStore(workspace(t), 2, nil, history)
	attachedResult := attached.Execute(context.Background(), "sh", command)
	if attachedResult.IsError {
		t.Fatalf("store-attached sh: %s", attachedResult.Content)
	}
	if got := strings.Split(attachedResult.Content, string(os.PathListSeparator))[0]; got != bin {
		t.Fatalf("store-attached PATH starts with %q, want %q: %q", got, bin, attachedResult.Content)
	}
}

func TestRecallSurfacesActiveSkillKind(t *testing.T) {
	history, err := graphstore.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = history.Close() })
	candidate, err := history.RecordSkillCandidate("", "tool:git",
		"repo-audit checks repository invariants", "/workspace/repo-audit")
	if err != nil {
		t.Fatal(err)
	}
	if err := history.ActivateSkill(candidate.Seq, "/home/test/.aforge/skills/repo-audit"); err != nil {
		t.Fatal(err)
	}

	tools := NewToolboxWithStore(workspace(t), 2, nil, history)
	result := tools.Execute(context.Background(), "recall", `{"terms":"repo audit invariants"}`)
	if result.IsError {
		t.Fatalf("recall failed: %s", result.Content)
	}
	var decoded recallToolResponse
	if err := json.Unmarshal([]byte(result.Content), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Notebook) != 1 || decoded.Notebook[0].Kind != graphstore.FactSkill ||
		decoded.Notebook[0].Body != "repo-audit checks repository invariants" {
		t.Fatalf("recalled skills = %+v", decoded.Notebook)
	}
	if !strings.Contains(result.Content, `"kind":"skill"`) {
		t.Fatalf("recall did not render the skill kind distinctly: %s", result.Content)
	}
}

// TestClampKeepsBothEnds guards the truncation rule. Keeping only the head is
// the obvious implementation and loses the most valuable line: a command's
// verdict is at the end, so head-only truncation reliably discards the error
// that made the output worth reading.
func TestClampKeepsBothEnds(t *testing.T) {
	body := strings.Repeat("a", maxToolResultBytes) + "FATAL: the thing that matters"
	clamped := clamp(body)

	if len(clamped) > maxToolResultBytes+128 {
		t.Errorf("clamped to %d bytes, want about %d", len(clamped), maxToolResultBytes)
	}
	if !strings.Contains(clamped, "FATAL: the thing that matters") {
		t.Error("truncation dropped the tail, which is where errors live")
	}
	if !strings.HasPrefix(clamped, "aaa") {
		t.Error("truncation dropped the head")
	}
	if !strings.Contains(clamped, "elided") {
		t.Error("truncation did not say that anything was removed")
	}
}

// TestToolFailuresAreResults is the rule that keeps a run alive. A mistyped
// path or a failing command has to come back as something the model can read
// and correct; returning a Go error instead throws away every turn before it.
func TestToolFailuresAreResults(t *testing.T) {
	tools := NewToolbox(workspace(t), 1, nil)
	ctx := context.Background()

	cases := []struct{ name, tool, args, want string }{
		{"unknown tool", "nope", `{}`, "sh, job, write, edit, web"},
		{"bad json", "sh", `{oops`, "valid JSON"},
		{"missing file", "edit", `{"path":"none.md","old":"x","new":"y"}`, "could not read"},
		{"failing command", "sh", `{"cmd":"exit 3"}`, "exit"},
		{"escaping path", "write", `{"path":"../outside.md","text":"x"}`, "escapes the workspace"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			result := tools.Execute(ctx, item.tool, item.args)
			if !result.IsError {
				t.Errorf("expected an error result, got %q", result.Content)
			}
			if !strings.Contains(result.Content, item.want) {
				t.Errorf("result %q does not mention %q", result.Content, item.want)
			}
		})
	}
}

// TestShDoesNotHangOnBackgroundChildren covers the hang that once wedged a
// whole run: a command leaves a background child sharing its stdout, bash
// exits, and CombinedOutput blocks until the child does — past every deadline,
// silently. The tool must return shortly after the command itself finishes.
func TestShDoesNotHangOnBackgroundChildren(t *testing.T) {
	tools := NewToolbox(workspace(t), 1, nil)
	started := time.Now()
	// The child outlives the ceiling by a wide margin on any host: returning
	// inside the bound can only mean sh did not wait for it. The gap is what
	// keeps this honest under load — not a tight ceiling.
	result := tools.Execute(context.Background(), "sh", `{"cmd":"sleep 120 & echo started"}`)
	if elapsed := time.Since(started); elapsed > 40*time.Second {
		t.Fatalf("sh blocked %s on a background child holding the pipe", elapsed.Round(time.Millisecond))
	}
	if result.IsError {
		t.Fatalf("a finished command with a detached child was reported as failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "started") {
		t.Errorf("the command's own output was lost: %q", result.Content)
	}
}

// TestEditRefusesAmbiguousMatch keeps a silent wrong edit from happening.
// Replacing the first of several matches looks like success and is the hardest
// kind of mistake to notice later.
func TestEditRefusesAmbiguousMatch(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "doc.md")
	if err := os.WriteFile(path, []byte("alpha\nalpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tools := NewToolbox(space, 1, nil)

	result := tools.Execute(context.Background(), "edit", `{"path":"doc.md","old":"alpha","new":"beta"}`)
	if !result.IsError || !strings.Contains(result.Content, "appears 2 times") {
		t.Fatalf("ambiguous edit was not refused: %+v", result)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "alpha\nalpha\n" {
		t.Errorf("file was modified despite the refusal: %q", body)
	}
}

// TestWriteRecordsArtifact checks the other half of the result contract: files
// are tracked so dependents can be pointed at them instead of being handed the
// whole text.
func TestWriteRecordsArtifact(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, 7, nil)

	result := tools.Execute(context.Background(), "write", `{"path":"report.md","text":"body"}`)
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}
	if got := space.Artifacts(7); len(got) != 1 || got[0] != "report.md" {
		t.Errorf("artifacts = %v, want [report.md]", got)
	}
	if got := space.Artifacts(8); len(got) != 0 {
		t.Errorf("artifact leaked to another node: %v", got)
	}
}

// TestSizeResolvesThroughSymlinkedRoot is the run summary's honesty check.
// Recorded artifact paths are workspace-relative and the root may be reached
// through a symlink — /tmp is one on macOS — so statting the recorded string
// from the process working directory finds nothing and reports every file as
// 0 bytes, which reads as a run that produced nothing at all.
func TestSizeResolvesThroughSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	space, err := NewWorkspace(link)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	tools := NewToolbox(space, 3, nil)
	if result := tools.Execute(context.Background(), "write", `{"path":"report.md","text":"body"}`); result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}

	recorded := space.Artifacts(3)
	if len(recorded) != 1 {
		t.Fatalf("artifacts = %v, want one entry", recorded)
	}
	size, ok := space.Size(recorded[0])
	if !ok {
		t.Fatalf("Size(%q) did not find the file the run just wrote", recorded[0])
	}
	if size != int64(len("body")) {
		t.Errorf("Size(%q) = %d, want %d", recorded[0], size, len("body"))
	}

	// Both spellings of the root name the same file, and an agent that learned
	// the resolved one from pwd must not make the summary lie.
	for _, spelling := range []string{filepath.Join(link, "report.md"), filepath.Join(target, "report.md")} {
		if size, ok := space.Size(spelling); !ok || size != int64(len("body")) {
			t.Errorf("Size(%q) = %d, %v; want %d, true", spelling, size, ok, len("body"))
		}
	}
	if _, ok := space.Size("never-written.md"); ok {
		t.Error("Size reported a file that does not exist")
	}
}

// TestSchedulerDispatchAndBlocking covers the two behaviours that decide
// whether a run is worth anything: a node with no inputs must not wait for
// anything, and one failure must cost only its own descendants.
func TestSchedulerDispatchAndBlocking(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	independent := graph.Add(plan.Node{Stage: 1, Title: "Independent"})
	doomed := graph.Add(plan.Node{Stage: 1, Title: "Doomed"})
	downstream := graph.Add(plan.Node{Stage: 1, Title: "Downstream"})
	if err := graph.AddNeed(downstream, doomed); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedExecutor{fail: map[int]bool{doomed: true}}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4).WithGovernor(calmGovernor())
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if state := graph.Node(independent).State; state != plan.StateDone {
		t.Errorf("independent node = %s, want done — an unrelated failure stopped it", state)
	}
	if state := graph.Node(doomed).State; state != plan.StateFailed {
		t.Errorf("failing node = %s, want failed", state)
	}
	if state := graph.Node(downstream).State; state != plan.StateBlocked {
		t.Errorf("downstream node = %s, want blocked", state)
	}
	if !fake.ranBefore(independent, doomed) && !fake.ran[independent] {
		t.Error("independent node never ran")
	}
}

// TestSchedulerRoutesOnlyDeclaredInputs is the runtime half of the design's
// central claim: the edge list is a context router, so a node must receive its
// declared inputs and nothing else.
func TestSchedulerRoutesOnlyDeclaredInputs(t *testing.T) {
	graph := &plan.Graph{Goal: "g", Stages: []plan.Stage{{Title: "One"}}, NextID: 1}
	wanted := graph.Add(plan.Node{Stage: 1, Title: "Wanted"})
	graph.Add(plan.Node{Stage: 1, Title: "Unrelated"})
	consumer := graph.Add(plan.Node{Stage: 1, Title: "Consumer"})
	if err := graph.AddNeed(consumer, wanted); err != nil {
		t.Fatal(err)
	}

	fake := &scriptedExecutor{}
	scheduler := NewScheduler(NewRegistry(fake), workspace(t), 4).WithGovernor(calmGovernor())
	if err := scheduler.Run(context.Background(), graph); err != nil {
		t.Fatalf("Run: %v", err)
	}

	inputs := fake.inputs[consumer]
	if len(inputs) != 1 || inputs[0].Title != "Wanted" {
		t.Errorf("consumer received %v, want only the declared input", titlesOf(inputs))
	}
}

// scriptedExecutor stands in for the real loop. It is mutex-guarded because the
// scheduler calls executors concurrently — the race detector caught this double
// without one, which is the same mistake a real executor could make.
type scriptedExecutor struct {
	mutex  sync.Mutex
	fail   map[int]bool
	ran    map[int]bool
	order  []int
	inputs map[int][]Input
}

func (s *scriptedExecutor) Skill() string { return "linear" }

func (s *scriptedExecutor) Run(ctx context.Context, task Task) (*Outcome, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.ran == nil {
		s.ran = map[int]bool{}
		s.inputs = map[int][]Input{}
	}
	s.ran[task.NodeID] = true
	s.order = append(s.order, task.NodeID)
	s.inputs[task.NodeID] = task.Inputs
	if s.fail[task.NodeID] {
		return &Outcome{Stop: StopError}, context.Canceled
	}
	return &Outcome{Text: "result of " + task.Title, Turns: 1, Stop: StopDone, Elapsed: time.Millisecond}, nil
}

func (s *scriptedExecutor) ranBefore(first, second int) bool {
	for _, id := range s.order {
		if id == first {
			return true
		}
		if id == second {
			return false
		}
	}
	return false
}

func titlesOf(inputs []Input) []string {
	titles := make([]string, len(inputs))
	for index, input := range inputs {
		titles[index] = input.Title
	}
	return titles
}

// TestEveryByteBudgetCutsOnACharacterBoundary is one rule in three places. A
// byte budget is a budget, not a boundary: cutting at the offset itself lands
// mid-character about half the time in any text that is not English, and the
// replacement character it leaves behind is carried into the model's context —
// on a routed input, on every turn of the consuming leaf, forever.
func TestEveryByteBudgetCutsOnACharacterBoundary(t *testing.T) {
	// Deliberately misaligned: a single ASCII byte in front of three-byte
	// characters puts every budget offset in this package one or two bytes
	// inside a character, and the four-byte characters plus a trailing byte do
	// the same for the windows that are measured from the end.
	head := "x" + strings.Repeat("日", 8192)
	tail := strings.Repeat("🎯", 4096) + "!"

	t.Run("routed input", func(t *testing.T) {
		bounded := boundInput(head, []string{"report.md"})
		if !utf8.ValidString(bounded) {
			t.Fatal("a routed upstream result was cut mid-character")
		}
		if !strings.Contains(bounded, "the complete version is in report.md") {
			t.Fatalf("bounded input lost its pointer: %q", bounded[len(bounded)-120:])
		}
	})

	t.Run("tool result", func(t *testing.T) {
		clamped := clamp(head + tail)
		if !utf8.ValidString(clamped) {
			t.Fatal("a tool result was cut mid-character at one of its two ends")
		}
		if !strings.Contains(clamped, "elided") {
			t.Fatal("clamp stopped saying that anything was removed")
		}
		if !strings.HasSuffix(clamped, "!") {
			t.Fatalf("the tail — where the verdict lives — did not survive: %q", clamped[len(clamped)-16:])
		}
	})

	t.Run("job line", func(t *testing.T) {
		// Two more bytes of shift: this budget's offsets happen to land on
		// character boundaries in the fixture above, and a truncation test
		// that never cuts mid-character proves nothing.
		line := compactJobLine("xy" + head + tail + "!")
		if !utf8.ValidString(line) {
			t.Fatal("a background job's last line was cut mid-character")
		}
		if !strings.Contains(line, "...") {
			t.Fatalf("job line lost its elision marker: %q", line)
		}
	})

	t.Run("recall clip", func(t *testing.T) {
		clipped := recallClip(head, 65)
		if !utf8.ValidString(clipped) || !strings.HasSuffix(clipped, "...") {
			t.Fatalf("recall clip = %q", clipped)
		}
	})
}
