package builder

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// goldenScreen is the bare screen object a model sends instead of a files map.
func goldenScreen(path string) string {
	golden, _ := spec.GoldenCRM()
	return string(golden[path])
}

// writeReplies answers WRITE calls with shaped(path) for screen paths, counting
// the calls per path.
type writeReplies struct {
	mu    sync.Mutex
	calls map[string]int
}

func (w *writeReplies) reply(prompt string, shaped func(path string, call int) string) string {
	paths := onlyPaths(prompt)
	w.mu.Lock()
	n := w.calls[paths[0]]
	w.calls[paths[0]]++
	w.mu.Unlock()
	if strings.HasPrefix(paths[0], "screens/") {
		if out := shaped(paths[0], n); out != "" {
			return out
		}
	}
	return goldenFiles(paths)
}

func TestWrongShapedWriteRepliesNormalizeOrBecomeFixIssues(t *testing.T) {
	ambiguous := `{"title":"Somewhere","body":{"part":"Text","props":{"text":"x"}}, "extra": 1}`
	tests := []struct {
		name   string
		shaped func(path string, call int) string
		fixes  int
	}{
		{"bare screen object", func(p string, _ int) string { return goldenScreen(p) }, 0},
		{"files list", func(p string, _ int) string {
			raw, _ := json.Marshal(map[string]any{"files": []map[string]any{{"path": p, "content": json.RawMessage(goldenScreen(p))}}})
			return string(raw)
		}, 0},
		{"folder nesting", func(p string, _ int) string {
			raw, _ := json.Marshal(map[string]any{"screens": map[string]any{strings.TrimPrefix(p, "screens/"): json.RawMessage(goldenScreen(p))}})
			return string(raw)
		}, 0},
		{"one screen ambiguous twice", func(p string, _ int) string {
			if p == "screens/deal.json" {
				return `{"layout":` + ambiguous + `,"other":{}}`
			}
			return ""
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, bridge.CheckReport{Pass: true})
			w := &writeReplies{calls: map[string]int{}}
			r.model.write = func(prompt string) string { return w.reply(prompt, tt.shaped) }
			r.model.fixes = []string{goldenFiles([]string{"screens/deal.json"})}
			r.create(t, true)
			r.waitState(t, bridge.StatePreview)
			if r.model.calls["fix"] != tt.fixes {
				t.Fatalf("FIX calls %d, want %d", r.model.calls["fix"], tt.fixes)
			}
			_, files := r.store.current()
			if string(files["screens/deal.json"]) == "" {
				t.Fatal("screens/deal.json was never written")
			}
			if tt.fixes == 0 {
				return
			}
			fix := lastPrompt(r, "fix")
			for _, want := range []string{ruleReplyShape, `exactly the paths ["screens/deal.json"]`, `got keys ["layout" "other"]`} {
				if !strings.Contains(fix, want) {
					t.Errorf("FIX prompt lacks %q", want)
				}
			}
			assertOwnerLogClean(t, r)
		})
	}
}

func TestFixReplyInTheWrongShapeSpendsARoundNotTheBuild(t *testing.T) {
	fail := bridge.CheckReport{Issues: []spec.Issue{{File: "screens/deal.json", Rule: "kit/label", Message: "label"}}}
	r := newRig(t, fail, fail, bridge.CheckReport{Pass: true})
	r.model.fixes = []string{"I fixed it, trust me", "still prose", goldenFiles([]string{"screens/deal.json"})}
	r.create(t, true)
	r.waitState(t, bridge.StatePreview)
	if r.model.calls["fix"] != 3 {
		t.Fatalf("FIX calls %d, want the shape retry and a second round", r.model.calls["fix"])
	}
	if !strings.Contains(lastPrompt(r, "fix"), ruleReplyShape) {
		t.Error("the second FIX round does not hear about the reply shape")
	}
	assertOwnerLogClean(t, r)
}

func TestFixItRepairsAShapeFailure(t *testing.T) {
	r := newRig(t)
	w := &writeReplies{calls: map[string]int{}}
	broken := true
	var mu sync.Mutex
	r.model.write = func(prompt string) string {
		mu.Lock()
		defer mu.Unlock()
		if broken && strings.Contains(prompt, "This call writes ONLY: app.json") {
			return `{"body":{},"nav":{},"screen":"x","title":"X"}`
		}
		return w.reply(prompt, func(string, int) string { return "" })
	}
	r.create(t, true)
	r.waitState(t, bridge.StateFailed)
	failed, ok := r.hub.find(bridge.StepWrite, bridge.BuildFailed)
	if !ok || failed.Log != logWrongShape {
		t.Fatalf("failed event = %+v", failed)
	}
	assertOwnerLogClean(t, r)
	settle(t, r)

	mu.Lock()
	broken = false
	mu.Unlock()
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)
	for path, n := range w.calls {
		if n != 1 {
			t.Errorf("%s written %d times", path, n)
		}
	}
}

func TestFixItAfterFailedCheckRepairsWithoutRewriting(t *testing.T) {
	fail := bridge.CheckReport{Issues: []spec.Issue{{File: "screens/deal.json", Rule: "kit/label", Message: "label"}}}
	r := newRig(t, fail)
	r.model.fixes = []string{goldenFiles([]string{"screens/deal.json"})}
	r.create(t, true)
	r.waitState(t, bridge.StateFailed)
	settle(t, r)
	writes := r.model.calls["write"]

	r.checks.mu.Lock()
	r.checks.reports = []bridge.CheckReport{fail, {Pass: true}}
	r.checks.seen = nil
	r.checks.mu.Unlock()
	if err := r.svc.Write(context.Background(), r.scope()); err != nil {
		t.Fatal(err)
	}
	r.waitState(t, bridge.StatePreview)
	if r.model.calls["write"] != writes {
		t.Errorf("Fix it rewrote the app: %d WRITE calls after %d", r.model.calls["write"], writes)
	}
	if r.model.calls["fix"] != 3 {
		t.Errorf("FIX calls %d, want two from the build and one from Fix it", r.model.calls["fix"])
	}
}

func settle(t *testing.T, r *rig) {
	t.Helper()
	b, _ := r.svc.buildFor(context.Background(), "crm")
	waitFor(t, "the build to settle", func() bool { b.mu.Lock(); defer b.mu.Unlock(); return !b.working })
}

func lastPrompt(r *rig, kind string) string {
	r.model.mu.Lock()
	defer r.model.mu.Unlock()
	var last string
	for _, p := range r.model.prompts {
		if callKind(p) == kind {
			last = p
		}
	}
	return last
}

// assertOwnerLogClean fails when a build log line carries the parser's text.
func assertOwnerLogClean(t *testing.T, r *rig) {
	t.Helper()
	sawRepair := false
	for _, ev := range r.hub.events() {
		for _, raw := range []string{"prompt:", "got keys", "JSON object"} {
			if strings.Contains(ev.Log, raw) {
				t.Errorf("owner log carries %q: %q", raw, ev.Log)
			}
		}
		sawRepair = sawRepair || ev.Log == logWrongShapeRetry
	}
	if !sawRepair {
		t.Errorf("no %q line in the build log", logWrongShapeRetry)
	}
}
