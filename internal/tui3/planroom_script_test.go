package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// scriptStepPage is a run task whose one step is a script that made two calls.
func scriptStepPage() session.PlanTaskPage {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	return session.PlanTaskPage{Row: root, Steps: []session.PlanStep{{
		Kind: "step", Step: 1, Tool: "script",
		Command: `{"source":"const r = await tools.bash({command: \"echo one\"})"}`,
		Calls: []session.PlanCall{
			{Tool: "bash", Command: "echo one", Observation: "one"},
			{Tool: "read_document", Command: `{"path":"brief.pdf"}`},
		},
	}}}
}

// A SCRIPT STEP DRAWS AS ITSELF, AND ITS CALLS FOLLOW IT MARKED NESTED. The
// record keeps the calls on the script's step (internal/run's StepCall); the
// room draws each as the tool it was, right after the script that made it.
func TestPlanRoomEntriesPutAScriptsCallsUnderItsStep(t *testing.T) {
	page := scriptStepPage()
	a, _ := planAppWith(t, []session.PlanTaskRow{page.Row}, nil)
	entries, _ := a.planRoomEntries(page)
	var tools []entry
	for _, e := range entries {
		if e.kind == entryTool {
			tools = append(tools, e)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("want the script and its two calls, got %d tool rows: %+v", len(tools), tools)
	}
	want := []struct {
		tool   string
		nested bool
		args   string
	}{{"script", false, "tools.bash"}, {"bash", true, "echo one"}, {"read_document", true, "brief.pdf"}}
	for i, w := range want {
		got := tools[i]
		if got.tool != w.tool || got.nested != w.nested || !strings.Contains(got.detail.Args, w.args) {
			t.Errorf("row %d = tool %q nested %v args %q; want %q nested %v with %q", i, got.tool, got.nested, got.detail.Args, w.tool, w.nested, w.args)
		}
	}
}

// A SCRIPT IN FLIGHT IS DRAWN AS A SCRIPT, from the tool the live step names.
func TestPlanRoomDrawsALiveScriptAsAScript(t *testing.T) {
	root := session.PlanTaskRow{ID: "t-root", Title: "Root", Status: "running"}
	root.Live.Step, root.Live.Tool, root.Live.Command = 2, "script", `{"source":"return 1"}`
	page := session.PlanTaskPage{Row: root, Live: root.Live}
	a, _ := planAppWith(t, []session.PlanTaskRow{root}, nil)
	entries, _ := a.planRoomEntries(page)
	for _, e := range entries {
		if e.kind == entryTool {
			if e.tool != "script" || e.status != toolRunning {
				t.Errorf("the live step is drawn as %q (%v), want a running script", e.tool, e.status)
			}
			return
		}
	}
	t.Fatal("no live row")
}

// A NESTED ROW IS THE SAME ROW, SHIFTED RIGHT AND NO WIDER. It sits under the
// script that made it and still fits the frame it is drawn in.
func TestNestedToolRowIsIndentedAndFitsItsWidth(t *testing.T) {
	page := scriptStepPage()
	a, _ := planAppWith(t, []session.PlanTaskRow{page.Row}, nil)
	entries, _ := a.planRoomEntries(page)
	at := -1
	for i, e := range entries {
		if e.kind == entryTool && e.nested {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("no nested row")
	}
	const width = 100
	nested := a.toolRows(deck{entries: entries}, at, true, width)[0].text
	flat := append([]entry(nil), entries...)
	flat[at].nested = false
	plainRow := a.toolRows(deck{entries: flat}, at, true, width)[0].text
	lead := func(s string) int { return len(s) - len(strings.TrimLeft(plain(s), " ")) }
	if got, base := lead(plain(nested)), lead(plain(plainRow)); got != base+nestedIndentCols {
		t.Errorf("a nested row leads with %d spaces, want %d (%d + %d)\n%q\n%q", got, base+nestedIndentCols, base, nestedIndentCols, plain(nested), plain(plainRow))
	}
	if w := len([]rune(plain(nested))); w > width {
		t.Errorf("a nested row is %d cells wide in a %d-cell frame: %q", w, width, plain(nested))
	}
}
